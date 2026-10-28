package sessionruntime

import (
	"context"
	"core/server/agentruntime"
	"core/server/sessionstore"
	"core/server/tools"
	"core/server/workflow"
	"core/server/workflowruntime"
	"core/shared/runtimeids"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

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

func (a *Authority) StartAgentExecution(ctx context.Context, request AgentExecutionRequest) (ExecutionHandle, error) {
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
	gate, release, err := a.admitSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer release()
	return a.startAgentExecutionUnderAdmission(ctx, gate, request, executionAccepted)
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
			a.resource.acceptRuntimeWrite()
			a.release()
			if onAccepted != nil {
				onAccepted()
			}
		}
		return committed, err
	}
}

func (a *Authority) admitSession(ctx context.Context, sessionID runtimeids.SessionID) (*sessionAdmissionGate, func(), error) {
	gate, releaseReference := a.gateFor(sessionID)
	if err := gate.lock.LockContext(ctx); err != nil {
		releaseReference()
		return nil, nil, err
	}
	if len(gate.blocks) != 0 {
		gate.lock.Unlock()
		releaseReference()
		return nil, nil, sessionStartsBlockedError(sessionID)
	}
	return gate, func() {
		gate.lock.Unlock()
		releaseReference()
	}, nil
}

// Turn joining and execution removal share the turn lock, inside the Session gate.
func (a *Authority) admitCurrentTurn(ctx context.Context, sessionID runtimeids.SessionID) (*turnAdmission, error) {
	gate, releaseGate, err := a.admitSession(ctx, sessionID)
	if err != nil {
		return nil, err
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
		releaseGate()
	})}, nil
}

func (a *Authority) admitAgentTurn(ctx context.Context, sessionID runtimeids.SessionID) (*turnAdmission, *execution, error) {
	for {
		admission, err := a.admitCurrentTurn(ctx, sessionID)
		if err != nil {
			return nil, nil, err
		}
		var current *execution
		if admission.resource != nil {
			admission.resource.mu.Lock()
			current = admission.resource.current
			admission.resource.mu.Unlock()
		}
		if current == nil || context.Cause(current.ctx) == nil {
			return admission, current, nil
		}
		admission.release()
		if err := current.awaitDone(ctx); err != nil {
			return nil, nil, err
		}
	}
}

type agentTurnResult struct {
	accepted bool
	err      error
	done     <-chan struct{}
}

func (r agentTurnResult) unavailable() *runtime.LiveRunAdmissionUnavailable {
	if r.accepted {
		return nil
	}
	var unavailable *runtime.LiveRunAdmissionUnavailable
	if errors.As(r.err, &unavailable) {
		return unavailable
	}
	return nil
}

func (a *turnAdmission) submit(
	ctx context.Context,
	current *execution,
	accept runtime.CommandAcceptance,
	run func(context.Context, *runtime.Engine, runtime.CommandAcceptance) error,
) agentTurnResult {
	a.resource.mu.Lock()
	notification := a.resource.changed
	a.resource.mu.Unlock()
	var accepted atomic.Bool
	err := a.resource.withEngineUnderAdmission(ctx, func(ctx context.Context, engine *runtime.Engine) error {
		return run(ctx, engine, a.acceptance(accept, func() { accepted.Store(true) }))
	})
	a.release()
	var unavailable *runtime.LiveRunAdmissionUnavailable
	if errors.Is(err, runtime.ErrNoActiveLiveRun) && !errors.As(err, &unavailable) {
		err = &runtime.LiveRunAdmissionUnavailable{Ready: notification}
	}
	return agentTurnResult{accepted: accepted.Load(), err: err, done: current.done}
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
		expected, err := a.prepareSessionExecution(ctx, request.Descriptor.SessionID())
		if errors.Is(err, ErrExecutionNoLongerLive) {
			continue
		}
		if err != nil {
			return nil, false, err
		}
		admission, current, err := a.admitAgentTurn(ctx, request.Descriptor.SessionID())
		if err != nil {
			return nil, false, err
		}
		resource := admission.resource
		if expected != nil && (current == nil || current.scope.ID() != expected.Scope().ID()) {
			admission.release()
			continue
		}
		if current == nil {
			if preparedResource != nil {
				admission.release()
				return nil, true, nil
			}
			handle, err := a.startAgentExecutionUnderAdmission(ctx, admission.gate, request, executionAccepted)
			admission.release()
			return handle, false, err
		}
		if preparedResource == nil || *preparedResource != resource.ref {
			admission.release()
			return nil, true, nil
		}
		accepted := false
		result := admission.submit(ctx, current, func(commit func() (bool, error)) (bool, error) { return commit() }, func(runCtx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) error {
			var joinErr error
			accepted, joinErr = join(runCtx, engine, accept)
			if !accepted && joinErr == nil {
				return runtime.ErrNoActiveLiveRun
			}
			return joinErr
		})
		if unavailable := result.unavailable(); unavailable != nil {
			if err := unavailable.Wait(ctx, result.done); err != nil {
				return nil, false, err
			}
			continue
		}
		if accepted {
			return executionHandle{execution: current}, false, result.err
		}
		return nil, false, result.err
	}
}

func (a *Authority) RunCurrentTurn(
	ctx context.Context,
	descriptor session.SessionDescriptor,
	accept runtime.CommandAcceptance,
	run func(context.Context, *runtime.Engine, runtime.CommandAcceptance) error,
) error {
	return a.runCurrentTurn(ctx, descriptor, accept, nil, run)
}

func (a *Authority) runCurrentTurn(
	ctx context.Context,
	descriptor session.SessionDescriptor,
	accept runtime.CommandAcceptance,
	expected *runtimeids.ExecutionScopeID,
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
	for {
		admission, current, err := a.admitAgentTurn(ctx, descriptor.SessionID())
		if err != nil {
			return err
		}
		result := a.runAdmittedAgentTurn(ctx, descriptor, admission, current, accept, expected, run)
		admission.release()
		if unavailable := result.unavailable(); unavailable != nil {
			if err := unavailable.Wait(ctx, result.done); err != nil {
				return err
			}
			continue
		}
		if !result.accepted && errors.Is(result.err, context.Canceled) && context.Cause(ctx) == nil {
			continue
		}
		return result.err
	}
}

func (a *Authority) runAdmittedAgentTurn(
	ctx context.Context,
	descriptor session.SessionDescriptor,
	admission *turnAdmission,
	current *execution,
	accept runtime.CommandAcceptance,
	expected *runtimeids.ExecutionScopeID,
	run func(context.Context, *runtime.Engine, runtime.CommandAcceptance) error,
) agentTurnResult {
	defer admission.release()
	gate, resource := admission.gate, admission.resource
	if resource == nil {
		return agentTurnResult{err: runtimeUnavailableErr(descriptor.SessionID().String())}
	}
	releaseAdmission := admission.release

	accepted := make(chan struct{})
	var acceptedOnce sync.Once
	admit := admission.acceptance(accept, func() {
		acceptedOnce.Do(func() { close(accepted) })
	})

	if expected != nil && (current == nil || current.scope.ID() != *expected) {
		return agentTurnResult{err: ErrExecutionNoLongerLive}
	}
	if current != nil {
		return admission.submit(ctx, current, accept, run)
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
		return agentTurnResult{err: err}
	}

	select {
	case <-accepted:
	case <-handle.Done():
		releaseAdmission()
	case <-ctx.Done():
		releaseAdmission()
	}
	if !<-operationContinues {
		_, err = handle.Wait(context.Background())
	}
	acceptedFresh := false
	select {
	case <-accepted:
		acceptedFresh = true
	default:
	}
	return agentTurnResult{accepted: acceptedFresh, err: err, done: handle.Done()}
}

func (a *Authority) runIdleAgentTurn(
	ctx context.Context,
	descriptor session.SessionDescriptor,
	accept runtime.CommandAcceptance,
	run func(context.Context, *runtime.Engine, runtime.CommandAcceptance) error,
) error {
	if a == nil {
		return errors.New("session runtime authority is required")
	}
	if err := descriptor.Validate(); err != nil {
		return err
	}
	if accept == nil || run == nil {
		return errors.New("agent turn acceptance and operation are required")
	}
	admission, err := a.admitCurrentTurn(ctx, descriptor.SessionID())
	if err != nil {
		return err
	}
	defer admission.release()
	if admission.resource != nil {
		admission.resource.mu.Lock()
		active := admission.resource.current != nil
		admission.resource.mu.Unlock()
		if active {
			return ErrSessionRunActive
		}
	}
	return a.runAdmittedAgentTurn(ctx, descriptor, admission, nil, accept, nil, run).err
}
