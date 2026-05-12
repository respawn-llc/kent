package registry

import (
	"context"
	"fmt"
	"io"

	"builder/server/primaryrun"
	"builder/server/runtime"
	"builder/server/runtimeview"
	askquestion "builder/server/tools/askquestion"
	"builder/shared/serverapi"
)

const (
	sessionActivityBufferSize = 256
	promptActivityBufferSize  = 64
)

type RuntimeRegistry struct {
	directory *runtimeDirectory
	leases    *primaryRunLeaseStore
}

func NewRuntimeRegistry() *RuntimeRegistry {
	return &RuntimeRegistry{
		directory: newRuntimeDirectory(),
		leases:    newPrimaryRunLeaseStore(),
	}
}

func (r *RuntimeRegistry) Register(sessionID string, engine *runtime.Engine) {
	if r == nil || engine == nil {
		return
	}
	previous := r.directory.Register(sessionID, engine)
	closeRuntimeEntry(previous, io.EOF)
}

func (r *RuntimeRegistry) Unregister(sessionID string, engine *runtime.Engine) {
	if r == nil {
		return
	}
	id, entry := r.directory.Unregister(sessionID, engine)
	if id == "" {
		return
	}
	r.leases.Clear(id)
	closeRuntimeEntry(entry, io.EOF)
}

func (r *RuntimeRegistry) ResolveRuntime(_ context.Context, sessionID string) (*runtime.Engine, error) {
	if r == nil {
		return nil, nil
	}
	return r.directory.Resolve(sessionID), nil
}

func (r *RuntimeRegistry) IsSessionRuntimeActive(sessionID string) bool {
	if r == nil {
		return false
	}
	return r.directory.Active(sessionID)
}

func (r *RuntimeRegistry) PublishRuntimeEvent(sessionID string, evt runtime.Event) {
	if r == nil {
		return
	}
	entry := r.directory.Entry(sessionID)
	if entry == nil || entry.sessionActivity == nil {
		return
	}
	entry.sessionActivity.Publish(runtimeview.EventFromRuntime(evt))
}

func (r *RuntimeRegistry) SubscribeSessionActivity(_ context.Context, sessionID string) (serverapi.SessionActivitySubscription, error) {
	return r.SubscribeSessionActivityFrom(context.Background(), serverapi.SessionActivitySubscribeRequest{SessionID: sessionID})
}

func (r *RuntimeRegistry) SubscribeSessionActivityFrom(_ context.Context, req serverapi.SessionActivitySubscribeRequest) (serverapi.SessionActivitySubscription, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime registry is required")
	}
	id := normalizeRegistrySessionID(req.SessionID)
	entry := r.directory.Entry(id)
	if entry == nil || entry.sessionActivity == nil {
		return nil, fmt.Errorf("session activity stream for %q is unavailable: %w", id, serverapi.ErrSessionActivityUnavailable)
	}
	return entry.sessionActivity.Subscribe(req.AfterSequence)
}

func (r *RuntimeRegistry) SubscribePromptActivity(_ context.Context, sessionID string) (serverapi.PromptActivitySubscription, error) {
	return r.SubscribePromptActivityFrom(context.Background(), serverapi.PromptActivitySubscribeRequest{SessionID: sessionID})
}

func (r *RuntimeRegistry) SubscribePromptActivityFrom(_ context.Context, req serverapi.PromptActivitySubscribeRequest) (serverapi.PromptActivitySubscription, error) {
	if r == nil {
		return nil, fmt.Errorf("runtime registry is required")
	}
	id := normalizeRegistrySessionID(req.SessionID)
	entry := r.directory.Entry(id)
	if entry == nil || entry.promptActivity == nil {
		return nil, fmt.Errorf("prompt activity stream for %q is unavailable: %w", id, serverapi.ErrStreamUnavailable)
	}
	if req.AfterSequence == 0 {
		return entry.SubscribePromptActivityInitial(id, nil)
	}
	sub, err := entry.promptActivity.Subscribe(nil, req.AfterSequence)
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, fmt.Errorf("prompt activity stream for %q is unavailable: %w", id, serverapi.ErrStreamUnavailable)
	}
	return sub, nil
}

func (r *RuntimeRegistry) BeginPendingPrompt(sessionID string, req askquestion.Request) {
	if r == nil {
		return
	}
	id := normalizeRegistrySessionID(sessionID)
	entry := r.directory.Entry(id)
	if entry == nil {
		return
	}
	snapshot, ok := entry.pendingPrompts.Begin(req)
	if !ok {
		return
	}
	entry.PublishPendingPrompt(id, snapshot, pendingPromptEventPending)
}

func (r *RuntimeRegistry) CompletePendingPrompt(sessionID string, requestID string) {
	if r == nil {
		return
	}
	id := normalizeRegistrySessionID(sessionID)
	entry := r.directory.Entry(id)
	if entry == nil {
		return
	}
	snapshot, ok := entry.pendingPrompts.Complete(requestID)
	if ok {
		entry.PublishPendingPrompt(id, snapshot, pendingPromptEventResolved)
	}
}

func (r *RuntimeRegistry) ListPendingPrompts(sessionID string) []PendingPromptSnapshot {
	if r == nil {
		return nil
	}
	entry := r.directory.Entry(sessionID)
	if entry == nil {
		return nil
	}
	return entry.pendingPrompts.List()
}

func (r *RuntimeRegistry) AwaitPromptResponse(ctx context.Context, sessionID string, req askquestion.Request) (askquestion.Response, error) {
	if r == nil {
		return askquestion.Response{}, fmt.Errorf("runtime registry is required")
	}
	id := normalizeRegistrySessionID(sessionID)
	entry := r.directory.Entry(id)
	if entry == nil {
		return askquestion.Response{}, fmt.Errorf("runtime %q is unavailable", id)
	}
	return entry.pendingPrompts.Await(ctx, req, func(snapshot PendingPromptSnapshot, eventType pendingPromptEventType) {
		entry.PublishPendingPrompt(id, snapshot, eventType)
	})
}

func (r *RuntimeRegistry) SubmitPromptResponse(sessionID string, resp askquestion.Response, err error) error {
	if r == nil {
		return fmt.Errorf("runtime registry is required")
	}
	id := normalizeRegistrySessionID(sessionID)
	entry := r.directory.Entry(id)
	if entry == nil {
		return fmt.Errorf("runtime %q is unavailable", id)
	}
	return entry.pendingPrompts.Submit(resp, err, func(snapshot PendingPromptSnapshot, eventType pendingPromptEventType) {
		entry.PublishPendingPrompt(id, snapshot, eventType)
	})
}

func (r *RuntimeRegistry) AcquirePrimaryRun(sessionID string) (primaryrun.Lease, error) {
	if r == nil {
		return nil, primaryrun.ErrActivePrimaryRun
	}
	return r.leases.Acquire(sessionID)
}
