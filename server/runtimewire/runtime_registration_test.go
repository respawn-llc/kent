package runtimewire

import (
	"context"
	"testing"

	"core/server/registry"
	"core/server/runtime"
)

type recordingRuntimeRegistry struct {
	registered   []string
	unregistered []string
	onUnregister func(string)
}

func (r *recordingRuntimeRegistry) Register(sessionID string, _ *runtime.Engine) {
	r.registered = append(r.registered, sessionID)
}

func (r *recordingRuntimeRegistry) Unregister(sessionID string, _ *runtime.Engine) {
	r.unregistered = append(r.unregistered, sessionID)
	if r.onUnregister != nil {
		r.onUnregister(sessionID)
	}
}

func TestRuntimeRegistrationCloseWithDrainRunsDrainBeforeUnregister(t *testing.T) {
	var order []string
	registry := &recordingRuntimeRegistry{onUnregister: func(string) {
		order = append(order, "unregister")
	}}
	engine := &runtime.Engine{}
	registration := RegisterSessionRuntime("session-1", engine, registry, nil)
	err := registration.CloseWithDrain(context.Background(), func(context.Context) error {
		order = append(order, "drain")
		return nil
	})
	if err != nil {
		t.Fatalf("CloseWithDrain: %v", err)
	}
	if len(registry.unregistered) != 1 || registry.unregistered[0] != "session-1" {
		t.Fatalf("unregistered = %#v, want session-1 once", registry.unregistered)
	}
	order = append(order, "closed")
	if len(order) != 3 || order[0] != "drain" || order[1] != "unregister" || order[2] != "closed" {
		t.Fatalf("order = %#v, want drain before unregister before close", order)
	}
	registration.Close()
	if len(registry.unregistered) != 1 {
		t.Fatalf("unregistered after idempotent close = %#v, want once", registry.unregistered)
	}
}

func TestRuntimeRegistrationCloseWithDrainRejectsNewGuardsBeforeDrain(t *testing.T) {
	registry := registry.NewRuntimeRegistry()
	engine := &runtime.Engine{}
	registration := RegisterSessionRuntime("session-1", engine, registry, nil)

	err := registration.CloseWithDrain(context.Background(), func(ctx context.Context) error {
		if !registry.IsSessionRuntimeActive("session-1") {
			t.Fatal("runtime should remain registered while drain runs")
		}
		guard, guardErr := registry.BeginRuntimeGuard(ctx, "session-1")
		if guard != nil {
			guard.Release()
		}
		if guardErr == nil {
			t.Fatal("expected close barrier to reject new guarded collaborative access before drain")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("CloseWithDrain: %v", err)
	}
	if registry.IsSessionRuntimeActive("session-1") {
		t.Fatal("runtime should be unregistered after close")
	}
}
