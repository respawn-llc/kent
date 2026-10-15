package sessionruntime

import (
	"errors"
	"slices"
	"sync"

	"core/server/runtime"
	"core/shared/runtimeids"
)

type resourceEventObserver struct {
	mu      sync.Mutex
	deliver func(runtime.Event)
	scopeID runtimeids.ExecutionScopeID
}

func (o *resourceEventObserver) publish(event runtime.Event) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.deliver != nil {
		o.deliver(event)
	}
}

// ObserveRuntimeEvents attaches future-only delivery to the current execution
// of one resource generation, while its caller holds turn admission.
// Removing this observation has no effect on server-owned execution.
func (a *Authority) ObserveRuntimeEvents(ref runtimeids.SessionResourceRef, deliver func(runtime.Event)) (func(), error) {
	if err := ref.Validate(); err != nil {
		return nil, err
	}
	if deliver == nil {
		return nil, errors.New("runtime event delivery is required")
	}
	a.mu.Lock()
	resource := a.resources[ref.SessionID()]
	a.mu.Unlock()
	if resource == nil || resource.ref != ref {
		return nil, runtimeUnavailableErr(ref.SessionID().String())
	}
	observer := &resourceEventObserver{deliver: deliver}
	resource.mu.Lock()
	if resource.rejectsNewUseLocked() || resource.current == nil {
		resource.mu.Unlock()
		return nil, runtimeUnavailableErr(ref.SessionID().String())
	}
	observer.scopeID = resource.current.scope.ID()
	resource.mu.Unlock()
	resource.eventMu.Lock()
	resource.eventObservers = append(resource.eventObservers, observer)
	resource.eventMu.Unlock()
	return sync.OnceFunc(func() {
		observer.mu.Lock()
		observer.deliver = nil
		observer.mu.Unlock()
		resource.eventMu.Lock()
		resource.eventObservers = slices.DeleteFunc(resource.eventObservers, func(item *resourceEventObserver) bool { return item == observer })
		resource.eventMu.Unlock()
	}), nil
}

func (r *agentResource) publishObservedEvent(event runtime.Event) {
	// Event producers may hold the resource's execution lock (notably Stop).
	// Delivery membership is independent from execution-state synchronization.
	r.eventMu.Lock()
	observers := slices.Clone(r.eventObservers)
	r.eventMu.Unlock()
	for _, observer := range observers {
		observer.publish(event)
	}
}

func (r *agentResource) removeExecutionEventObservers(scopeID runtimeids.ExecutionScopeID) {
	r.eventMu.Lock()
	defer r.eventMu.Unlock()
	r.eventObservers = slices.DeleteFunc(r.eventObservers, func(observer *resourceEventObserver) bool {
		return observer.scopeID == scopeID
	})
}
