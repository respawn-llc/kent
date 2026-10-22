package sessionruntime

import (
	"context"
	"core/server/runtime"
	"core/server/session"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"errors"
	"fmt"
)

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
