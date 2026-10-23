package launch

import (
	"testing"

	"core/internal/testharness/testsetup"
	"core/server/sessionstore"
)

func TestResolveSessionCallerTreatsUnownedSessionAsOrdinary(t *testing.T) {
	persistenceRoot := t.TempDir()
	metadataStore := testsetup.OpenStore(t, persistenceRoot)
	ordinary := createMetadataBackedSession(
		t,
		metadataStore,
		persistenceRoot,
		"/tmp/ordinary-workspace",
		session.ContinuationContext{},
	)
	ordinaryCaller, err := ResolveSessionCaller(t.Context(), metadataStore, ordinary.Meta().SessionID)
	if err != nil {
		t.Fatalf("ResolveSessionCaller ordinary: %v", err)
	}
	if ordinaryCaller.Workflow {
		t.Fatal("ordinary session resolved as workflow")
	}
}
