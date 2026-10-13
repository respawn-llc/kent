package runner

import (
	"testing"

	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestSessionLifecycleOptionsUseExplicitTypedInitialIntent(t *testing.T) {
	selected := runtimeids.NewSessionID()
	parent := runtimeids.NewSessionID()

	tests := []struct {
		name          string
		request       Request
		wantKind      serverapi.SessionLaunchIntentKind
		wantSessionID runtimeids.SessionID
		wantParentID  runtimeids.SessionID
	}{
		{
			name:          "open selected session",
			request:       Request{SessionID: selected.String()},
			wantKind:      serverapi.SessionLaunchIntentOpenExisting,
			wantSessionID: selected,
		},
		{
			name: "create for non-default agent role",
			request: Request{
				AgentRole: runnerStringPtr("reviewer"),
			},
			wantKind: serverapi.SessionLaunchIntentCreateNew,
		},
		{
			name: "create with workspace context parent",
			request: Request{
				WorkspaceContextSessionID: parent.String(),
			},
			wantKind:     serverapi.SessionLaunchIntentCreateNew,
			wantParentID: parent,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			intent, _, err := SessionLifecycleOptionsFor(test.request)
			if err != nil {
				t.Fatalf("SessionLifecycleOptionsFor: %v", err)
			}
			if (intent.GetCreateNew() != nil) != (test.wantKind == serverapi.SessionLaunchIntentCreateNew) {
				t.Fatalf("intent = %v, want %q", intent, test.wantKind)
			}
			if test.wantSessionID.IsZero() {
				if intent.GetOpenExistingSessionId() != nil {
					t.Fatal("create intent unexpectedly has an existing session identity")
				}
			} else if got := intent.GetOpenExistingSessionId().GetSessionId(); got != test.wantSessionID.String() {
				t.Fatalf("session identity = %q, want %q", got, test.wantSessionID.String())
			}
			origin := intent.GetCreateNew()
			if test.wantParentID.IsZero() {
				if origin.GetParentAgentSessionId() != nil {
					t.Fatal("intent unexpectedly has a parent-agent origin")
				}
			} else if got := origin.GetParentAgentSessionId().GetSessionId(); got != test.wantParentID.String() {
				t.Fatalf("parent-agent identity = %q, want %q", got, test.wantParentID.String())
			}
		})
	}
}

func TestSessionLifecycleOptionsRejectInvalidIdentityWithoutEmptyStringInference(t *testing.T) {
	for _, request := range []Request{
		{SessionID: "   "},
		{WorkspaceContextSessionID: "   "},
	} {
		if _, _, err := SessionLifecycleOptionsFor(request); err == nil {
			t.Fatalf("SessionLifecycleOptionsFor(%+v) succeeded; want invalid identity error", request)
		}
	}
}

func mustRunnerSessionID(t *testing.T, raw string) runtimeids.SessionID {
	t.Helper()
	id, err := runtimeids.ParseSessionID(raw)
	if err != nil {
		t.Fatalf("ParseSessionID(%q): %v", raw, err)
	}
	return id
}
