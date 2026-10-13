package sessionruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"

	"core/server/runlog"
	"core/server/runtime"
	"core/server/runtimewire"
	"core/server/session"
	"core/server/tools"
	shelltool "core/server/tools/shell"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

type AgentResourceState uint8

var ErrSessionRunActive = errors.New("session has an active run")
var ErrSessionStartsBlocked = errors.New("session starts are blocked")
var ErrSessionWorkflowActivationActive = errors.New("session has a retained workflow activation")

const (
	AgentResourceBuilding AgentResourceState = iota + 1
	AgentResourceReady
	AgentResourceDraining
	AgentResourceClosed
)

type AgentResourceDescriptor struct {
	Ref   runtimeids.SessionResourceRef
	State AgentResourceState
}

type AgentResourceEventFeed func(runtimeids.SessionResourceRef, runtime.Event)

type AgentResourceObserver func() (io.Closer, error)

type AgentResourceLifecycle interface {
	ResourceReady(context.Context, AgentResourceDescriptor, *runtime.Engine, AgentResourceObserver) error
	ResourceDraining(context.Context, AgentResourceDescriptor) error
}

type AgentResourceStepLifecycle interface {
	StepBegan(context.Context, AgentResourceDescriptor, runtime.StepLifecycleSnapshot) error
	StepEnded(context.Context, AgentResourceDescriptor, runtime.StepLifecycleSnapshot) error
}

type AgentResourceSelection interface {
	agentResourceSelection()
}

type CurrentAgentResource struct{}

func (CurrentAgentResource) agentResourceSelection() {}

type OpenAgentResource struct{}

func (OpenAgentResource) agentResourceSelection() {}

type ReplaceAgentResource struct{}

func (ReplaceAgentResource) agentResourceSelection() {}

type RuntimeOpenRequest struct {
	SessionID runtimeids.SessionID
	Runtime   *AgentRuntimePlan
}

type resourceObservation struct {
	resource *agentResource
	once     sync.Once
	err      error
}

func (r *resourceObservation) Close() error {
	r.once.Do(func() {
		resource := r.resource
		resource.mu.Lock()
		defer resource.mu.Unlock()
		if resource.observers == 0 {
			r.err = resource.authority.invariant("close observation", errors.New("Session Runtime observation underflow"))
			return
		}
		resource.observers--
		resource.updateIdleLocked(false)
		resource.signalLocked()
	})
	return r.err
}

type AgentRuntimeBridge struct {
	authority *Authority
	resource  runtimeids.SessionResourceRef
}

func (b AgentRuntimeBridge) WithEngine(ctx context.Context, callback func(context.Context, *runtime.Engine) error) error {
	if b.authority == nil {
		return errors.New("agent runtime bridge is uninitialized")
	}
	return b.authority.WithRuntime(ctx, b.resource, callback)
}

type AgentRunner func(context.Context, ExecutionScope, AgentRuntimeBridge) error

type ExecutionAskHandler func(context.Context, ExecutionScope, tools.AskQuestionRequest) (tools.AskQuestionResolution, error)

type AgentExecutionRequest struct {
	Descriptor session.SessionDescriptor
	Runtime    *AgentRuntimePlan
	Workflow   *WorkflowAgentExecution
	Resource   AgentResourceSelection
	Ask        ExecutionAskHandler
	Runner     AgentRunner
}

type WorkflowAgentExecution struct {
	Reference WorkflowExecutionRef
	// Config is absent during pre-assignment compaction, which is Workflow
	// scoped but must retain the outgoing Session's request configuration.
	Config   *workflowruntime.CurrentNodeExecutionConfig
	OnRetire func()
}

type agentResource struct {
	authority *Authority
	ref       runtimeids.SessionResourceRef
	ctx       context.Context
	cancel    context.CancelFunc

	// Turn admission and completion share this lock. Resource replacement
	// uses the Session gate and may wait for execution completion.
	turnMu            sync.Mutex
	mu                sync.Mutex
	changed           chan struct{}
	state             AgentResourceState
	store             *session.Store
	engine            *runtime.Engine
	logger            *runlog.RunLogger
	localTools        *runtimewire.LocalToolRegistryBinding
	askBroker         *tools.AskQuestionBroker
	close             func() error
	backgroundLimit   int
	backgroundMode    shelltool.BackgroundOutputMode
	current           *execution
	eventMu           sync.Mutex
	eventObservers    []*resourceEventObserver
	pins              int
	callbacks         int
	steps             int
	lifecycleReady    bool
	lifecycleDraining bool
	idleDeadline      *time.Time
	observers         int
}

func (r *agentResource) descriptorLocked() AgentResourceDescriptor {
	return AgentResourceDescriptor{
		Ref:   r.ref,
		State: r.state,
	}
}

func (r *agentResource) descriptor() AgentResourceDescriptor {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.descriptorLocked()
}

func (r *agentResource) signalLocked() {
	close(r.changed)
	r.changed = make(chan struct{})
}

func (r *agentResource) currentExecution() (ExecutionHandle, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.current == nil {
		return nil, false
	}
	return executionHandle{execution: r.current}, true
}

func (r *agentResource) pinLocked() error {
	if r.rejectsNewUseLocked() {
		return errors.Join(
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("agent resource %s generation %d is unavailable", r.ref.SessionID(), r.ref.Generation()),
		)
	}
	r.pins++
	r.signalLocked()
	return nil
}

func (r *agentResource) releasePin() error {
	r.mu.Lock()
	if r.pins <= 0 {
		panic(fmt.Sprintf("agent resource %s generation %d pin underflow", r.ref.SessionID(), r.ref.Generation()))
	}
	r.pins--
	r.signalLocked()
	r.mu.Unlock()
	return nil
}

func (r *agentResource) withEngine(ctx context.Context, ref runtimeids.SessionResourceRef, callback func(context.Context, *runtime.Engine) error) (err error) {
	if callback == nil {
		return errors.New("agent resource callback is required")
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	r.mu.Lock()
	if r.ref != ref || r.rejectsNewUseLocked() || r.engine == nil {
		r.mu.Unlock()
		return errors.Join(
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("agent resource %s generation %d is unavailable", ref.SessionID(), ref.Generation()),
		)
	}
	engine := r.engine
	r.callbacks++
	r.signalLocked()
	r.mu.Unlock()
	defer func() {
		if releaseErr := r.releaseCallback(); releaseErr != nil {
			err = errors.Join(err, releaseErr)
		}
	}()
	return callback(ctx, engine)
}

// withStoreUnderAdmission runs while the caller owns the Session admission
// gate. Resource retirement must be attempted only after that gate is
// released, because retirement acquires the same gate.
func (r *agentResource) withStoreUnderAdmission(ctx context.Context, callback func(context.Context, *session.Store) error) error {
	r.mu.Lock()
	if r.rejectsNewUseLocked() || r.store == nil {
		r.mu.Unlock()
		return fmt.Errorf("agent resource %s generation %d is unavailable", r.ref.SessionID(), r.ref.Generation())
	}
	store := r.store
	r.callbacks++
	r.signalLocked()
	r.mu.Unlock()
	defer func() {
		r.releaseCallbackCount()
	}()
	return callback(ctx, store)
}

func (r *agentResource) withEngineUnderAdmission(
	ctx context.Context,
	callback func(context.Context, *runtime.Engine) error,
) error {
	if callback == nil {
		return errors.New("agent resource callback is required")
	}
	engine, err := r.beginEngineCallbackUnderAdmission()
	if err != nil {
		return err
	}
	defer r.releaseCallbackCount()
	return callback(ctx, engine)
}

func (r *agentResource) beginEngineCallbackUnderAdmission() (*runtime.Engine, error) {
	r.mu.Lock()
	if r.rejectsNewUseLocked() || r.engine == nil {
		ref := r.ref
		r.mu.Unlock()
		return nil, errors.Join(
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("agent resource %s generation %d is unavailable", ref.SessionID(), ref.Generation()),
		)
	}
	engine := r.engine
	r.callbacks++
	r.signalLocked()
	r.mu.Unlock()
	return engine, nil
}

func (r *agentResource) releaseCallbackCount() {
	r.mu.Lock()
	if r.callbacks <= 0 {
		panic(fmt.Sprintf("agent resource %s generation %d callback underflow", r.ref.SessionID(), r.ref.Generation()))
	}
	r.callbacks--
	r.signalLocked()
	r.mu.Unlock()
}

func (r *agentResource) releaseCallback() error {
	r.releaseCallbackCount()
	return nil
}

func (r *agentResource) publishReady(ctx context.Context) error {
	if r.authority.options.resourceLifecycle == nil {
		return nil
	}
	r.mu.Lock()
	if r.state != AgentResourceReady || r.engine == nil {
		r.mu.Unlock()
		return fmt.Errorf("agent resource %s generation %d is not ready", r.ref.SessionID(), r.ref.Generation())
	}
	if r.lifecycleReady {
		r.mu.Unlock()
		return nil
	}
	r.lifecycleReady = true
	descriptor := r.descriptorLocked()
	engine := r.engine
	r.mu.Unlock()
	if err := r.authority.options.resourceLifecycle.ResourceReady(ctx, descriptor, engine, func() (io.Closer, error) {
		return r.observe()
	}); err != nil {
		return err
	}
	return engine.PublishConnectionReplacement()
}

func (r *agentResource) closeResource(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	switch r.state {
	case AgentResourceClosed:
		r.mu.Unlock()
		return nil
	case AgentResourceBuilding:
		r.mu.Unlock()
		return fmt.Errorf("agent resource %s generation %d is still building", r.ref.SessionID(), r.ref.Generation())
	}
	r.state = AgentResourceDraining
	r.cancel()
	r.signalLocked()
	notifyDraining := r.lifecycleReady && !r.lifecycleDraining && r.authority.options.resourceLifecycle != nil
	if notifyDraining {
		r.lifecycleDraining = true
	}
	descriptor := r.descriptorLocked()
	engine := r.engine
	r.mu.Unlock()
	var interruptErr error
	if engine != nil {
		interruptErr = engine.Interrupt()
	}
	var lifecycleErr error
	if notifyDraining {
		lifecycleErr = r.authority.options.resourceLifecycle.ResourceDraining(ctx, descriptor)
	}
	r.mu.Lock()
	for r.pins != 0 || r.callbacks != 0 || r.steps != 0 || r.current != nil {
		changed := r.changed
		r.mu.Unlock()
		select {
		case <-changed:
		case <-ctx.Done():
			return context.Cause(ctx)
		}
		r.mu.Lock()
	}
	// Another close caller may have finished while this caller released the lock.
	if r.state == AgentResourceClosed {
		r.mu.Unlock()
		return errors.Join(lifecycleErr, interruptErr)
	}
	closeEngine := r.close
	r.state = AgentResourceClosed
	r.signalLocked()
	r.mu.Unlock()
	if closeEngine == nil {
		return lifecycleErr
	}
	return errors.Join(lifecycleErr, interruptErr, closeEngine())
}

func (r *agentResource) StepBegan(ctx context.Context, snapshot runtime.StepLifecycleSnapshot) error {
	r.mu.Lock()
	if r.rejectsNewStepLocked() {
		r.mu.Unlock()
		return errors.Join(
			runtime.ErrEngineClosed,
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("agent resource %s generation %d is retiring", r.ref.SessionID(), r.ref.Generation()),
		)
	}
	if r.steps != 0 {
		r.mu.Unlock()
		panic(fmt.Sprintf("agent resource %s generation %d admitted overlapping engine steps", r.ref.SessionID(), r.ref.Generation()))
	}
	r.steps++
	r.idleDeadline = nil
	if r.current != nil {
		r.current.admission = executionAccepted
	}
	r.signalLocked()
	descriptor := r.descriptorLocked()
	r.mu.Unlock()
	if r.authority.options.stepLifecycle == nil {
		return nil
	}
	return r.authority.options.stepLifecycle.StepBegan(ctx, descriptor, snapshot)
}

func (r *agentResource) StepEnded(ctx context.Context, snapshot runtime.StepLifecycleSnapshot) error {
	r.mu.Lock()
	if r.steps == 0 {
		rejected := r.rejectsNewStepLocked()
		r.mu.Unlock()
		if rejected {
			return nil
		}
		panic(fmt.Sprintf("agent resource %s generation %d engine step underflow", r.ref.SessionID(), r.ref.Generation()))
	}
	descriptor := r.descriptorLocked()
	r.mu.Unlock()
	var publishErr error
	if r.authority.options.stepLifecycle != nil {
		publishErr = r.authority.options.stepLifecycle.StepEnded(ctx, descriptor, snapshot)
	}
	pending, pendingErr := r.engine.PendingWorkSnapshot()
	r.mu.Lock()
	if r.steps != 1 {
		r.mu.Unlock()
		panic(fmt.Sprintf("agent resource %s generation %d engine step count changed during completion", r.ref.SessionID(), r.ref.Generation()))
	}
	r.steps--
	r.updateIdleLocked(len(pending.Items) != 0)
	r.signalLocked()
	r.mu.Unlock()
	return errors.Join(publishErr, pendingErr)
}

func (r *agentResource) rejectsNewUseLocked() bool {
	return r.state != AgentResourceReady
}

func (r *agentResource) rejectsNewStepLocked() bool {
	return r.state != AgentResourceReady
}

func (a *Authority) OpenRuntime(ctx context.Context, request RuntimeOpenRequest) (runtimeids.SessionResourceRef, error) {
	return a.openRuntime(ctx, request, nil)
}

func (a *Authority) openRuntime(
	ctx context.Context,
	request RuntimeOpenRequest,
	resolvePlan func(context.Context, *session.Store) (*AgentRuntimePlan, AgentResourceSelection, error),
) (runtimeids.SessionResourceRef, error) {
	if a == nil {
		return runtimeids.SessionResourceRef{}, errors.New("session runtime authority is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := context.Cause(ctx); err != nil {
		return runtimeids.SessionResourceRef{}, err
	}
	if request.SessionID.IsZero() {
		return runtimeids.SessionResourceRef{}, errors.New("session id is required")
	}
	gate, releaseGate := a.gateFor(request.SessionID)
	defer releaseGate()
	gate.lock.Lock()
	defer gate.lock.Unlock()
	if len(gate.blocks) != 0 {
		return runtimeids.SessionResourceRef{}, sessionStartsBlockedError(request.SessionID)
	}
	descriptor, err := session.NewOpenSessionDescriptor(request.SessionID)
	if err != nil {
		return runtimeids.SessionResourceRef{}, err
	}
	var admittedStore *runtimeStoreAdmission
	var resourceSelection AgentResourceSelection = OpenAgentResource{}
	if resolvePlan != nil {
		admittedStore, err = a.materializeRuntimeStore(descriptor)
		if err != nil {
			return runtimeids.SessionResourceRef{}, err
		}
		request.Runtime, resourceSelection, err = resolvePlan(ctx, admittedStore.store)
		if err != nil {
			return runtimeids.SessionResourceRef{}, err
		}
		if request.Runtime == nil {
			return runtimeids.SessionResourceRef{}, errors.New("resolved runtime plan is required")
		}
	}
	var resource *agentResource
	switch resourceSelection.(type) {
	case OpenAgentResource:
		resource, err = a.openResource(ctx, descriptor, request.Runtime, admittedStore)
	case ReplaceAgentResource:
		resource, err = a.replaceResource(ctx, descriptor, request.Runtime, admittedStore)
	default:
		return runtimeids.SessionResourceRef{}, fmt.Errorf("unsupported runtime open resource selection %T", resourceSelection)
	}
	if err != nil {
		return runtimeids.SessionResourceRef{}, err
	}
	resource.acceptRuntimeWrite()
	return resource.ref, nil
}

func (a *Authority) retireRuntimeAbortResource(ctx context.Context, resource *agentResource) error {
	if resource == nil {
		return nil
	}
	sessionID := resource.ref.SessionID()
	gate, releaseGate := a.gateFor(sessionID)
	defer releaseGate()
	gate.lock.Lock()
	defer gate.lock.Unlock()

	resource.mu.Lock()
	a.mu.Lock()
	admitted := a.resources[sessionID] == resource
	a.mu.Unlock()
	if !admitted || resource.state == AgentResourceClosed {
		resource.mu.Unlock()
		return nil
	}
	if resource.state != AgentResourceReady ||
		resource.steps != 0 {
		descriptor := resource.descriptorLocked()
		resource.mu.Unlock()
		return fmt.Errorf(
			"retire runtime-aborted resource %s generation %d from state %d with live ownership",
			descriptor.Ref.SessionID(),
			descriptor.Ref.Generation(),
			descriptor.State,
		)
	}
	_, err := a.closeAdmittedResourceLocked(ctx, resource)
	return err
}

// closeAdmittedResourceLocked owns the exact transition from a ready resource
// admitted for closure to its removal from the live authority. The caller must
// hold the session admission gate and resource.mu.
func (a *Authority) closeAdmittedResourceLocked(ctx context.Context, resource *agentResource) (bool, error) {
	sessionID := resource.ref.SessionID()
	engine := resource.engine
	resource.state = AgentResourceDraining
	resource.signalLocked()
	resource.mu.Unlock()

	if engine != nil {
		engine.FailQueuedUserMessages(runtime.QueuedUserMessageFailureClosing)
	}
	closeErr := resource.closeResource(ctx)
	resource.mu.Lock()
	closed := resource.state == AgentResourceClosed
	resource.mu.Unlock()
	if closed {
		a.mu.Lock()
		if a.resources[sessionID] == resource {
			delete(a.resources, sessionID)
		}
		a.mu.Unlock()
	}
	return closed, closeErr
}

func (a *Authority) StartAgentExecution(ctx context.Context, request AgentExecutionRequest) (ExecutionHandle, error) {
	return a.startAgentExecution(ctx, request, executionAccepted)
}

func (a *Authority) startAgentExecution(ctx context.Context, request AgentExecutionRequest, admission executionAdmission) (ExecutionHandle, error) {
	if a == nil {
		return nil, errors.New("session runtime authority is required")
	}
	if err := context.Cause(ctx); err != nil {
		return nil, err
	}
	if err := request.Descriptor.Validate(); err != nil {
		return nil, err
	}
	sessionID := request.Descriptor.SessionID()
	if request.Resource == nil {
		return nil, errors.New("agent resource selection is required")
	}
	if request.Runner == nil {
		return nil, errors.New("agent runner is required")
	}
	gate, releaseGate := a.gateFor(sessionID)
	defer releaseGate()
	if err := gate.lock.LockContext(ctx); err != nil {
		return nil, err
	}
	defer gate.lock.Unlock()
	return a.startAgentExecutionUnderAdmission(ctx, gate, request, admission)
}

func (a *Authority) startAgentExecutionUnderAdmission(
	ctx context.Context,
	gate *sessionAdmissionGate,
	request AgentExecutionRequest,
	admission executionAdmission,
) (ExecutionHandle, error) {
	sessionID := request.Descriptor.SessionID()
	if len(gate.blocks) != 0 {
		return nil, sessionStartsBlockedError(sessionID)
	}
	resource, err := a.selectResource(ctx, request.Descriptor, request.Runtime, request.Resource)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, ErrAuthorityClosed
	}
	var workflowRef *WorkflowExecutionRef
	var workflowKey workflow.CurrentNodeReferenceKey
	if request.Workflow != nil {
		if err := request.Workflow.Reference.Validate(); err != nil {
			a.mu.Unlock()
			return nil, err
		}
		if err := validateWorkflowAgentExecution(request.Workflow); err != nil {
			a.mu.Unlock()
			return nil, err
		}
		ref := request.Workflow.Reference
		workflowRef = &ref
		workflowKey, err = workflowExecutionKeyFor(ref)
		if err != nil {
			a.mu.Unlock()
			return nil, err
		}
		if a.workflowExecutionByCurrentNodeLocked(ref, workflowKey) != nil {
			a.mu.Unlock()
			return nil, fmt.Errorf("workflow current node %v is already live", ref.CurrentNode)
		}
	}
	resource.mu.Lock()
	if resource.current != nil {
		resource.mu.Unlock()
		a.mu.Unlock()
		return nil, errors.Join(ErrSessionRunActive, fmt.Errorf("session %s already has an agent execution", sessionID))
	}
	if workflowRef == nil && resource.engine.CurrentNodeExecutionConfigured() {
		resource.mu.Unlock()
		a.mu.Unlock()
		return nil, errors.Join(
			ErrSessionWorkflowActivationActive,
			fmt.Errorf("session %s cannot start an ordinary execution while its workflow activation remains active", sessionID),
		)
	}
	if err := resource.pinLocked(); err != nil {
		resource.mu.Unlock()
		a.mu.Unlock()
		return nil, err
	}
	scopeID := runtimeids.NewExecutionScopeID()
	scope := newAgentExecutionScope(scopeID, resource.ref, workflowRef)
	var workflowBinding *runtime.CurrentNodeExecutionBinding
	if request.Workflow != nil && request.Workflow.Config != nil {
		config := *request.Workflow.Config
		config.ScopeID = scopeID
		workflowBinding, err = resource.engine.BindCurrentNodeExecution(&config)
		if err != nil {
			resource.pins--
			resource.signalLocked()
			resource.mu.Unlock()
			a.mu.Unlock()
			return nil, fmt.Errorf("bind workflow current node execution: %w", err)
		}
	}
	correlation, err := runtimeids.NewExecutionCorrelation(scope.ID(), resource.ref.Generation())
	if err != nil {
		bindingErr := workflowBinding.Close()
		resource.pins--
		resource.signalLocked()
		resource.mu.Unlock()
		a.mu.Unlock()
		return nil, errors.Join(a.invariant(
			"create Agent execution correlation",
			fmt.Errorf("scope=%s resource=%v: %w", scope.ID(), resource.ref, err),
		), bindingErr)
	}
	if resource.localTools != nil {
		if err := resource.localTools.BindExecutionCorrelation(&correlation); err != nil {
			bindingErr := workflowBinding.Close()
			resource.pins--
			resource.signalLocked()
			resource.mu.Unlock()
			a.mu.Unlock()
			return nil, errors.Join(fmt.Errorf("bind agent execution correlation: %w", err), bindingErr)
		}
	}
	runCtx, cancel := context.WithCancel(resource.ctx)
	execution := &execution{
		authority: a,
		resource:  resource,
		scope:     scope,
		ctx:       runCtx,
		cancel:    cancel,
		done:      make(chan struct{}),
		prompts:   newExecutionPromptStore(a, scope, a.promptFeed),
		workflow:  workflowBinding,
		phase:     executionPhaseRunning,
		admission: admission,
	}
	if request.Workflow != nil {
		execution.onRetire = request.Workflow.OnRetire
	}
	if resource.askBroker != nil {
		askHandler := request.Ask
		if askHandler == nil {
			askHandler = func(ctx context.Context, scope ExecutionScope, req tools.AskQuestionRequest) (tools.AskQuestionResolution, error) {
				return a.AwaitPromptResolution(ctx, scope.ID(), req)
			}
		}
		resource.askBroker.SetLifecycleAskHandler(func(ctx context.Context, req tools.AskQuestionRequest) (tools.AskQuestionResolution, error) {
			return askHandler(ctx, execution.scope, req)
		})
	}
	resource.current = execution
	if admission == executionAccepted {
		resource.idleDeadline = nil
	}
	resource.signalLocked()
	resource.mu.Unlock()
	a.byScope[scope.ID()] = execution
	if workflowRef != nil {
		a.addWorkflowExecutionLocked(*workflowRef, workflowKey, execution)
	}
	a.mu.Unlock()
	go func() {
		runErr := request.Runner(execution.ctx, execution.scope, AgentRuntimeBridge{
			authority: a,
			resource:  resource.ref,
		})
		execution.finish(ExecutionResult{}, runErr, nil)
	}()
	return executionHandle{execution: execution}, nil
}

func validateWorkflowAgentExecution(request *WorkflowAgentExecution) error {
	if request == nil {
		return nil
	}
	if request.Config == nil {
		return nil
	}
	if !request.Config.Instructions.CurrentNode.Equal(request.Reference.CurrentNode) {
		return errors.New("workflow runtime config does not match execution current node")
	}
	return nil
}

type turnAdmission struct {
	gate     *sessionAdmissionGate
	resource *agentResource
	release  func()
}

func (a *turnAdmission) acceptance(accept runtime.CommandAcceptance, onAccepted func()) runtime.CommandAcceptance {
	return func(commit func() (bool, error)) (bool, error) {
		committed, err := accept(commit)
		if committed {
			a.release()
			if onAccepted != nil {
				onAccepted()
			}
		}
		return committed, err
	}
}

// Turn joining and execution removal share the turn lock, inside the Session gate.
func (a *Authority) admitCurrentTurn(ctx context.Context, sessionID runtimeids.SessionID) (*turnAdmission, error) {
	gate, releaseReference := a.gateFor(sessionID)
	if err := gate.lock.LockContext(ctx); err != nil {
		releaseReference()
		return nil, err
	}
	if len(gate.blocks) != 0 {
		gate.lock.Unlock()
		releaseReference()
		return nil, sessionStartsBlockedError(sessionID)
	}
	a.mu.Lock()
	resource := a.resources[sessionID]
	a.mu.Unlock()
	if resource != nil {
		resource.turnMu.Lock()
	}
	return &turnAdmission{gate: gate, resource: resource, release: sync.OnceFunc(func() {
		if resource != nil {
			resource.turnMu.Unlock()
		}
		gate.lock.Unlock()
		releaseReference()
	})}, nil
}

// ContinueAgentExecution chooses active steering or idle launch at the same
// boundary as RunCurrentTurn. changed asks the caller to refresh preparation
// outside the joining locks; no input has been accepted in that case.
type ContinuationCandidate interface {
	continuationCandidate()
}

type ActiveContinuation struct {
	Descriptor session.SessionDescriptor
	Resource   runtimeids.SessionResourceRef
	Join       func(context.Context, *runtime.Engine, runtime.CommandAcceptance) (bool, error)
}

func (ActiveContinuation) continuationCandidate() {}

type IdleContinuation struct{ Request AgentExecutionRequest }

func (IdleContinuation) continuationCandidate() {}

func (a *Authority) ContinueAgentExecution(ctx context.Context, candidate ContinuationCandidate) (handle ExecutionHandle, changed bool, err error) {
	if a == nil {
		return nil, false, errors.New("session runtime authority is required")
	}
	var request AgentExecutionRequest
	var preparedResource *runtimeids.SessionResourceRef
	var join func(context.Context, *runtime.Engine, runtime.CommandAcceptance) (bool, error)
	switch candidate := candidate.(type) {
	case ActiveContinuation:
		request.Descriptor, preparedResource, join = candidate.Descriptor, &candidate.Resource, candidate.Join
	case IdleContinuation:
		request = candidate.Request
	default:
		return nil, false, errors.New("continuation candidate is required")
	}
	if err := request.Descriptor.Validate(); err != nil {
		return nil, false, err
	}
	if preparedResource != nil {
		if err := preparedResource.Validate(); err != nil {
			return nil, false, err
		}
		if join == nil {
			return nil, false, errors.New("continuation join callback is required")
		}
	} else if request.Resource == nil || request.Runner == nil {
		return nil, false, errors.New("idle continuation execution request is required")
	}
	for {
		admission, err := a.admitCurrentTurn(ctx, request.Descriptor.SessionID())
		if err != nil {
			return nil, false, err
		}
		resource := admission.resource
		var current *execution
		if resource != nil {
			resource.mu.Lock()
			current = resource.current
			resource.mu.Unlock()
		}
		if current == nil {
			if preparedResource != nil {
				admission.release()
				return nil, true, nil
			}
			handle, err := a.startAgentExecutionUnderAdmission(ctx, admission.gate, request)
			admission.release()
			return handle, false, err
		}
		if preparedResource == nil || *preparedResource != resource.ref {
			admission.release()
			return nil, true, nil
		}
		if context.Cause(current.ctx) != nil {
			admission.release()
			if err := current.awaitDone(ctx); err != nil {
				return nil, false, err
			}
			continue
		}
		accepted := false
		err = resource.withEngineUnderAdmission(ctx, func(runCtx context.Context, engine *runtime.Engine) error {
			var joinErr error
			accepted, joinErr = join(runCtx, engine, admission.acceptance(
				func(commit func() (bool, error)) (bool, error) { return commit() }, nil))
			return joinErr
		})
		if accepted {
			admission.release()
			return executionHandle{execution: current}, false, err
		}
		if err != nil && !errors.Is(err, runtime.ErrNoActiveLiveRun) {
			admission.release()
			return nil, false, err
		}
		resource.mu.Lock()
		notification := resource.changed
		resource.mu.Unlock()
		var unavailable *runtime.LiveRunAdmissionUnavailable
		var ready <-chan struct{}
		if errors.As(err, &unavailable) {
			ready = unavailable.Ready
		}
		admission.release()
		select {
		case <-ready:
		case <-notification:
		case <-current.done:
		case <-ctx.Done():
			return nil, false, context.Cause(ctx)
		}
	}
}

func (a *Authority) RunCurrentTurn(
	ctx context.Context,
	descriptor session.SessionDescriptor,
	accept runtime.CommandAcceptance,
	run func(context.Context, *runtime.Engine, runtime.CommandAcceptance) error,
) error {
	if a == nil {
		return errors.New("session runtime authority is required")
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	if err := descriptor.Validate(); err != nil {
		return err
	}
	if accept == nil {
		return errors.New("turn acceptance is required")
	}
	if run == nil {
		return errors.New("turn callback is required")
	}
	sessionID := descriptor.SessionID()
	admission, err := a.admitCurrentTurn(ctx, sessionID)
	if err != nil {
		return err
	}
	defer admission.release()
	gate, resource := admission.gate, admission.resource
	if resource == nil {
		return runtimeUnavailableErr(sessionID.String())
	}
	releaseAdmission := admission.release

	accepted := make(chan struct{})
	var acceptedOnce sync.Once
	admit := admission.acceptance(accept, func() {
		resource.acceptRuntimeWrite()
		acceptedOnce.Do(func() { close(accepted) })
	})

	resource.mu.Lock()
	current := resource.current
	resource.mu.Unlock()
	if current != nil {
		_, workflowExecution := current.scope.Workflow()
		interruptedHumanExecution := current.scope.Kind() == ExecutionScopeAgent &&
			!workflowExecution &&
			context.Cause(current.ctx) != nil
		if interruptedHumanExecution {
			releaseAdmission()
			if err := current.awaitDone(ctx); err != nil {
				return err
			}
			return a.RunCurrentTurn(ctx, descriptor, accept, run)
		}
		runErr := resource.withEngineUnderAdmission(ctx, func(runCtx context.Context, engine *runtime.Engine) error {
			return run(runCtx, engine, admit)
		})
		releaseAdmission()
		return runErr
	}

	operationContinues := make(chan bool, 1)
	handle, err := a.startAgentExecutionUnderAdmission(ctx, gate, AgentExecutionRequest{
		Descriptor: descriptor,
		Resource:   CurrentAgentResource{},
		Runner: func(executionCtx context.Context, _ ExecutionScope, bridge AgentRuntimeBridge) error {
			callbackRan := false
			runErr := bridge.WithEngine(executionCtx, func(_ context.Context, engine *runtime.Engine) error {
				callbackRan = true
				runCtx, stop := MergeContexts(executionCtx, ctx)
				err := run(runCtx, engine, admit)
				stop()
				releaseAdmission()
				if err != nil {
					operationContinues <- false
					return err
				}
				queuedWorkScheduled := engine.HasScheduledQueuedUserWork()
				goalLoopActive := engine.GoalLoopRunning()
				operationContinues <- queuedWorkScheduled || goalLoopActive
				return nil
			})
			if !callbackRan {
				releaseAdmission()
				operationContinues <- false
			}
			return runErr
		},
	}, executionProvisional)
	if err != nil {
		return err
	}
	exactHandle, ok := handle.(executionHandle)
	if !ok || exactHandle.execution == nil {
		return a.invariant(
			"run current Agent turn",
			fmt.Errorf("execution handle type=%T", handle),
		)
	}

	select {
	case <-accepted:
	case <-exactHandle.execution.done:
		releaseAdmission()
	case <-ctx.Done():
		releaseAdmission()
	}
	if <-operationContinues {
		return nil
	}
	_, err = handle.Wait(context.Background())
	acceptedFresh := false
	select {
	case <-accepted:
		acceptedFresh = true
	default:
	}
	if !acceptedFresh && context.Cause(ctx) == nil && errors.Is(err, context.Canceled) {
		return a.RunCurrentTurn(ctx, descriptor, accept, run)
	}
	return err
}

func (a *Authority) RunCurrentAgentExecution(
	ctx context.Context,
	descriptor session.SessionDescriptor,
	run func(context.Context, *runtime.Engine) error,
) error {
	if a == nil {
		return errors.New("session runtime authority is required")
	}
	if run == nil {
		return errors.New("agent runtime callback is required")
	}
	operationContinues := make(chan bool, 1)
	handle, err := a.startAgentExecution(ctx, AgentExecutionRequest{
		Descriptor: descriptor,
		Resource:   CurrentAgentResource{},
		Runner: func(executionCtx context.Context, _ ExecutionScope, bridge AgentRuntimeBridge) error {
			callbackRan := false
			runErr := bridge.WithEngine(executionCtx, func(_ context.Context, engine *runtime.Engine) error {
				callbackRan = true
				runCtx, stop := MergeContexts(executionCtx, ctx)
				err := run(runCtx, engine)
				stop()
				operationContinues <- err == nil && (engine.HasScheduledQueuedUserWork() || engine.GoalLoopRunning())
				return err
			})
			if !callbackRan {
				operationContinues <- false
			}
			return runErr
		},
	}, executionProvisional)
	if err != nil {
		return err
	}
	if <-operationContinues {
		return nil
	}
	_, err = handle.Wait(context.Background())
	return err
}

func (a *Authority) WithRuntime(ctx context.Context, ref runtimeids.SessionResourceRef, callback func(context.Context, *runtime.Engine) error) error {
	if a == nil {
		return errors.New("session runtime authority is required")
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	if err := ref.Validate(); err != nil {
		return err
	}
	a.mu.Lock()
	resource := a.resources[ref.SessionID()]
	a.mu.Unlock()
	if resource == nil {
		return errors.Join(
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("agent resource %s generation %d is unavailable", ref.SessionID(), ref.Generation()),
		)
	}
	return resource.withEngine(ctx, ref, callback)
}

func (a *Authority) WithCurrentRuntime(ctx context.Context, sessionID runtimeids.SessionID, callback func(context.Context, *runtime.Engine) error) error {
	if a == nil {
		return errors.New("session runtime authority is required")
	}
	if sessionID.IsZero() {
		return errors.New("session id is required")
	}
	a.mu.Lock()
	resource := a.resources[sessionID]
	a.mu.Unlock()
	if resource == nil {
		return errors.Join(
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("session %s has no active runtime available", sessionID),
		)
	}
	return resource.withEngine(ctx, resource.ref, callback)
}

func (a *Authority) WithLiveExecutionRuntime(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	callback func(context.Context, *runtime.Engine) error,
) error {
	if a == nil {
		return errors.New("session runtime authority is required")
	}
	execution, ok := a.SessionExecution(sessionID)
	if !ok {
		err := a.WithCurrentRuntime(ctx, sessionID, func(context.Context, *runtime.Engine) error {
			return nil
		})
		if err != nil {
			return err
		}
		return serverapi.ErrRuntimeNoActiveRun
	}
	resource, ok := execution.Scope().Resource()
	if !ok {
		return errors.New("agent execution scope has no runtime resource")
	}
	return a.WithRuntime(ctx, resource, callback)
}

// WithRetainedWorkflowRuntime admits a callback only while the Session keeps
// Workflow activation without requiring an Exact Execution Scope to be live.
func (a *Authority) WithRetainedWorkflowRuntime(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	callback func(context.Context, *runtime.Engine) error,
) error {
	if callback == nil {
		return errors.New("retained workflow runtime callback is required")
	}
	return a.WithCurrentRuntime(ctx, sessionID, func(ctx context.Context, engine *runtime.Engine) error {
		if !engine.CurrentNodeExecutionConfigured() {
			return errors.Join(
				serverapi.ErrRuntimeNoActiveRun,
				fmt.Errorf("session %s has no retained workflow activation", sessionID),
			)
		}
		return callback(ctx, engine)
	})
}

func (a *Authority) withInterruptibleAgentTurn(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	withoutExecution func() error,
	callback func(context.Context, *runtime.Engine, *execution) error,
) error {
	if a == nil {
		return errors.New("session runtime authority is required")
	}
	if sessionID.IsZero() {
		return errors.New("session id is required")
	}
	if callback == nil {
		return errors.New("interruptible Agent Turn mutation is required")
	}
	runWithoutExecution := func(missing error) error {
		if err := context.Cause(ctx); err != nil {
			return err
		}
		if withoutExecution != nil {
			return withoutExecution()
		}
		return missing
	}
	a.mu.Lock()
	resource := a.resources[sessionID]
	if resource == nil {
		defer a.mu.Unlock()
		return runWithoutExecution(errors.Join(serverapi.ErrRuntimeUnavailable, fmt.Errorf("session %s has no active runtime available", sessionID)))
	}
	resource.mu.Lock()
	execution := resource.current
	if execution == nil {
		defer a.mu.Unlock()
		defer resource.mu.Unlock()
		return runWithoutExecution(ErrExecutionNoLongerLive)
	}
	resource.mu.Unlock()
	a.mu.Unlock()
	execution.prompts.mu.RLock()
	defer execution.prompts.mu.RUnlock()
	resource.mu.Lock()
	defer resource.mu.Unlock()
	if resource.current != execution {
		return ErrExecutionNoLongerLive
	}
	if resource.rejectsNewUseLocked() || resource.engine == nil {
		return errors.Join(serverapi.ErrRuntimeUnavailable, fmt.Errorf("session %s has no active runtime available", sessionID))
	}
	if err := context.Cause(ctx); err != nil {
		return err
	}
	return callback(ctx, resource.engine, execution)
}

// WithInterruptibleAgentTurn prevents Question admission across one exact current-execution mutation.
func (a *Authority) WithInterruptibleAgentTurn(
	ctx context.Context,
	sessionID runtimeids.SessionID,
	withoutExecution func() error,
	callback func(context.Context, *runtime.Engine) error,
) error {
	if callback == nil {
		return errors.New("interruptible Agent Turn mutation is required")
	}
	return a.withInterruptibleAgentTurn(
		ctx,
		sessionID,
		withoutExecution,
		func(ctx context.Context, engine *runtime.Engine, _ *execution) error {
			return callback(ctx, engine)
		},
	)
}

func (a *Authority) InterruptSession(
	ctx context.Context,
	sessionID runtimeids.SessionID,
) (bool, error) {
	if a == nil {
		return false, errors.New("session runtime authority is required")
	}
	if sessionID.IsZero() {
		return false, errors.New("session id is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	a.mu.Lock()
	resource := a.resources[sessionID]
	if resource == nil {
		a.mu.Unlock()
		return false, context.Cause(ctx)
	}
	resource.mu.Lock()
	execution := resource.current
	resource.mu.Unlock()
	a.mu.Unlock()
	if execution == nil {
		var interrupted bool
		err := resource.withEngine(ctx, resource.ref, func(_ context.Context, engine *runtime.Engine) error {
			gate, releaseGate := a.gateFor(sessionID)
			defer releaseGate()
			if err := gate.lock.LockContext(ctx); err != nil {
				return err
			}
			defer gate.lock.Unlock()
			resource.mu.Lock()
			successor := resource.current != nil
			resource.mu.Unlock()
			if successor {
				return nil
			}
			var err error
			interrupted, err = engine.TryInterruptActiveRun()
			return err
		})
		return interrupted, err
	}

	execution.exactMu.Lock()
	defer execution.exactMu.Unlock()
	a.mu.Lock()
	live := a.byScope[execution.scope.ID()] == execution
	a.mu.Unlock()
	if !live {
		return false, nil
	}
	alreadyCanceled := context.Cause(execution.ctx) != nil
	if err := context.Cause(ctx); err != nil {
		return false, err
	}
	execution.prompts.mu.Lock()
	closure := execution.prompts.closeLocked(context.Canceled)
	execution.prompts.mu.Unlock()
	publicationErr := execution.prompts.publishClosure(closure)
	execution.prompts.releaseClosure(closure)
	publicationErr = errors.Join(
		publicationErr,
		execution.prompts.closeApprovals(closure, true),
	)

	resource.mu.Lock()
	if resource.current != execution {
		resource.mu.Unlock()
		return false, publicationErr
	}
	if resource.rejectsNewUseLocked() || resource.engine == nil {
		resource.mu.Unlock()
		return false, errors.Join(
			publicationErr,
			serverapi.ErrRuntimeUnavailable,
			fmt.Errorf("session %s has no active runtime available", sessionID),
		)
	}
	engine := resource.engine
	execution.cancel()
	interrupted, interruptErr := engine.TryInterruptActiveRun()
	resource.mu.Unlock()
	if interruptErr == nil && !interrupted && !alreadyCanceled {
		interruptErr = engine.PersistInterruption()
	}
	return interrupted || !alreadyCanceled, errors.Join(publicationErr, interruptErr)
}

func (r *agentResource) observe() (io.Closer, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.state != AgentResourceReady {
		return nil, serverapi.ErrRuntimeUnavailable
	}
	r.observers++
	r.idleDeadline = nil
	r.signalLocked()
	return &resourceObservation{resource: r}, nil
}

func (a *Authority) selectResource(ctx context.Context, descriptor session.SessionDescriptor, plan *AgentRuntimePlan, selection AgentResourceSelection) (*agentResource, error) {
	sessionID := descriptor.SessionID()
	switch selection.(type) {
	case CurrentAgentResource:
		a.mu.Lock()
		resource := a.resources[sessionID]
		a.mu.Unlock()
		if resource == nil {
			return nil, errors.Join(
				serverapi.ErrRuntimeUnavailable,
				fmt.Errorf("session %s has no registered runtime", sessionID),
			)
		}
		return resource, nil
	case OpenAgentResource:
		return a.openResource(ctx, descriptor, plan, nil)
	case ReplaceAgentResource:
		return a.replaceResource(ctx, descriptor, plan, nil)
	default:
		return nil, fmt.Errorf("unsupported agent resource selection %T", selection)
	}
}

func (a *Authority) openResource(
	ctx context.Context,
	descriptor session.SessionDescriptor,
	plan *AgentRuntimePlan,
	admittedStore *runtimeStoreAdmission,
) (*agentResource, error) {
	sessionID := descriptor.SessionID()
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil, ErrAuthorityClosed
	}
	resource := a.resources[sessionID]
	a.mu.Unlock()
	created := false
	if resource != nil && plan != nil {
		if err := resource.withStoreUnderAdmission(ctx, func(_ context.Context, store *session.Store) error {
			return store.AdoptToolSelection(plan.options.ExplicitToolSelection)
		}); err != nil {
			return nil, err
		}
	}
	if resource == nil {
		if plan == nil {
			return nil, ErrAgentRuntimePlanRequired
		}
		var err error
		resource, err = a.buildAgentResource(ctx, descriptor, plan, admittedStore)
		if err != nil {
			return nil, err
		}
		a.mu.Lock()
		if a.closed {
			a.mu.Unlock()
			return nil, errors.Join(ErrAuthorityClosed, resource.closeResource(ctx))
		}
		if existing := a.resources[sessionID]; existing != nil {
			a.mu.Unlock()
			if err := resource.closeResource(ctx); err != nil {
				return nil, fmt.Errorf("close redundant session %s runtime: %w", sessionID, err)
			}
			resource = existing
		} else {
			a.resources[sessionID] = resource
			created = true
			a.mu.Unlock()
		}
	}
	if created {
		if err := resource.publishReady(ctx); err != nil {
			a.mu.Lock()
			if a.resources[sessionID] == resource {
				delete(a.resources, sessionID)
			}
			a.mu.Unlock()
			return nil, errors.Join(err, resource.closeResource(ctx))
		}
		resource.mu.Lock()
		resource.refreshIdleLocked()
		resource.updateIdleLocked(false)
		resource.mu.Unlock()
		a.launchLifecycleTask(resource.runIdleRetirement)
	}
	resource.mu.Lock()
	if resource.state != AgentResourceReady {
		resource.mu.Unlock()
		return nil, fmt.Errorf("session %s runtime is not ready", sessionID)
	}
	resource.signalLocked()
	resource.mu.Unlock()
	if a.options.background != nil {
		a.options.background.RetryTerminalEvents(sessionID.String())
	}
	return resource, nil
}

func (a *Authority) replaceResource(
	ctx context.Context,
	descriptor session.SessionDescriptor,
	plan *AgentRuntimePlan,
	admittedStore *runtimeStoreAdmission,
) (*agentResource, error) {
	sessionID := descriptor.SessionID()
	a.mu.Lock()
	existing := a.resources[sessionID]
	a.mu.Unlock()
	if existing != nil {
		if err := a.retireResourceForReplacement(ctx, existing); err != nil {
			return nil, err
		}
	}
	return a.openResource(ctx, descriptor, plan, admittedStore)
}

func (a *Authority) retireResourceForReplacement(ctx context.Context, resource *agentResource) error {
	for {
		resource.mu.Lock()
		switch resource.state {
		case AgentResourceClosed:
			resource.mu.Unlock()
			a.mu.Lock()
			if a.resources[resource.ref.SessionID()] == resource {
				delete(a.resources, resource.ref.SessionID())
			}
			a.mu.Unlock()
			return nil
		case AgentResourceDraining:
			changed := resource.changed
			resource.mu.Unlock()
			select {
			case <-changed:
				continue
			case <-ctx.Done():
				return context.Cause(ctx)
			}
		case AgentResourceReady:
		default:
			state := resource.state
			resource.mu.Unlock()
			return fmt.Errorf(
				"session %s runtime generation %d cannot be replaced from state %d",
				resource.ref.SessionID(),
				resource.ref.Generation(),
				state,
			)
		}

		if resource.current != nil || resource.callbacks != 0 || resource.steps != 0 {
			changed := resource.changed
			resource.mu.Unlock()
			select {
			case <-changed:
				continue
			case <-ctx.Done():
				return context.Cause(ctx)
			}
		}
		_, closeErr := a.closeAdmittedResourceLocked(ctx, resource)
		return closeErr
	}
}
