package runtime

import (
	"errors"
	"testing"

	"core/server/llm"
	"core/server/session"
)

func TestFastModeAvailabilityUsesCurrentConnectionCapabilityWithOldSessionContract(t *testing.T) {
	t.Parallel()
	store := mustCreateTestSession(t)
	if err := store.MarkModelDispatchLocked(session.LockedContract{
		Model: "custom-model",
		ProviderContract: session.LockedProviderCapabilities{
			ProviderID: "anthropic",
		},
	}); err != nil {
		t.Fatalf("persist provider contract: %v", err)
	}

	currentCapabilities := llm.ProviderCapabilities{
		ProviderID: "openai-compatible", SupportsResponsesAPI: true, SupportsFastMode: true,
	}
	client := &fakeClient{
		capsErr: errors.New("transient provider capability failure"),
	}
	engine, err := New(
		store,
		mustMaterializeTestEventLog(t, store),
		client,
		newTestToolRegistry(t),
		Config{Model: "custom-model", ProviderCapabilitiesOverride: &currentCapabilities},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { _ = engine.Close() })
	if !engine.FastModeAvailable() {
		t.Fatal("current provider capability was ignored in favor of the old Session provider facts")
	}
}
