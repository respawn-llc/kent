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
			a.resource.acceptRuntimeWrite()
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
			handle, err := a.startAgentExecutionUnderAdmission(ctx, admission.gate, request, executionAccepted)
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
		var unavailable *runtime.LiveRunAdmissionUnavailable
		if errors.As(runErr, &unavailable) {
			if err := unavailable.Wait(ctx, current.done); err != nil {
				return err
			}
			return a.RunCurrentTurn(ctx, descriptor, accept, run)
		}
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
	if !acceptedFresh && context.Cause(ctx) == nil {
		var unavailable *runtime.LiveRunAdmissionUnavailable
		if errors.As(err, &unavailable) {
			if err := unavailable.Wait(ctx, nil); err != nil {
				return err
			}
			return a.RunCurrentTurn(ctx, descriptor, accept, run)
		}
		if errors.Is(err, context.Canceled) {
			return a.RunCurrentTurn(ctx, descriptor, accept, run)
		}
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
