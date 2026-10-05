package sessionruntime

import (
	"context"
	"errors"
	"testing"

	"core/server/runtime"
	"core/server/session"
	"core/server/session/sessiontest"
	"core/shared/runtimeids"
)

func TestConnectionReplacementPersistenceFailureDoesNotPublishOrDispatch(t *testing.T) {
	fixture := newSessionRuntimeFixture(t)
	if err := fixture.store.SetConnectionID("removed"); err != nil {
		t.Fatal(err)
	}
	gate := sessiontest.NewPersistenceGate(sessiontest.NewPersistence())
	failure := errors.New("replacement persistence rejected")
	gate.FailWhen(func(snapshot session.PersistedStoreSnapshot) bool {
		return snapshot.Meta.ConnectionID != nil && *snapshot.Meta.ConnectionID == *fixture.config.Settings.Connection
	}, failure)
	var notices int
	authority := NewAuthority(AuthorityOptions{
		PersistenceRoot:     fixture.config.PersistenceRoot,
		WorkspaceMembership: fixture.metadata,
		StoreOptions:        append(fixture.metadata.AuthoritativeSessionStoreOptions(), session.WithPersistenceObserver(gate)),
		EventFeed: func(_ runtimeids.SessionResourceRef, event runtime.Event) {
			if event.Kind == runtime.EventConnectionReplaced {
				notices++
			}
		},
	})
	t.Cleanup(func() { _ = authority.Close(context.Background()) })
	client := &sessionRuntimeTestLLMClient{}
	plan := authorityTestRuntimePlan(t, fixture, client)
	_, err := authority.OpenRuntime(t.Context(), RuntimeOpenRequest{
		SessionID: lifecycleSessionID(t, fixture), OwnerID: "replacement-failure", Runtime: &plan,
	})
	if !errors.Is(err, failure) {
		t.Fatalf("activation error = %v", err)
	}
	record, err := fixture.metadata.ResolvePersistedSession(t.Context(), fixture.store.Meta().SessionID)
	if err != nil {
		t.Fatal(err)
	}
	if record.Meta.ConnectionID == nil || *record.Meta.ConnectionID != "removed" || notices != 0 {
		t.Fatalf("failed replacement binding=%v notices=%d", record.Meta.ConnectionID, notices)
	}
	if len(client.requestSnapshot()) != 0 {
		t.Fatal("failed replacement dispatched a request")
	}
}
