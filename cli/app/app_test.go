package app

import (
	"testing"

	"core/server/launch"
	"core/server/sessionstore"
	"core/shared/config"
	sharedpb "core/shared/protoapi/gen/kent/api/shared"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/toolspec"
)

func TestEffectiveSettingsKeepsBaseThinkingLevelEvenWhenSessionIsLocked(t *testing.T) {
	base := config.Settings{Model: "gpt-6-sol", ThinkingLevel: "high"}
	locked := &session.LockedContract{Model: "gpt-6-sol"}

	effective := launch.EffectiveSettings(base, locked)
	if effective.ThinkingLevel != "high" {
		t.Fatalf("thinking level = %q, want %q", effective.ThinkingLevel, "high")
	}
}

func TestActiveToolIDs_UsesLockedEnabledToolsVerbatim(t *testing.T) {
	locked := &session.LockedContract{EnabledTools: []string{string(toolspec.ToolExecCommand)}}
	ids, err := launch.ActiveToolIDsForPlan(config.Settings{Model: "gpt-6-sol"}, config.SourceReport{}, locked)
	if err != nil {
		t.Fatalf("activeToolIDs: %v", err)
	}
	if !containsToolID(ids, toolspec.ToolExecCommand) || len(ids) != 1 {
		t.Fatalf("expected locked enabled tools to be used verbatim, got %+v", ids)
	}
}

func containsToolID(ids []toolspec.ID, want toolspec.ID) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func TestSessionLaunchOptionsForUsesTypedInitialIntent(t *testing.T) {
	selected := runtimeids.NewSessionID()
	parent := runtimeids.NewSessionID()

	tests := []struct {
		name          string
		options       Options
		wantKind      serverapi.SessionLaunchIntentKind
		wantSessionID runtimeids.SessionID
		wantParentID  runtimeids.SessionID
	}{
		{
			name:          "open selected session",
			options:       Options{SessionID: selected.String()},
			wantKind:      serverapi.SessionLaunchIntentOpenExisting,
			wantSessionID: selected,
		},
		{
			name:     "create for non-default agent role",
			options:  Options{AgentRole: sessionLifecycleStringPtr("reviewer")},
			wantKind: serverapi.SessionLaunchIntentCreateNew,
		},
		{
			name: "create with workspace context parent",
			options: Options{
				WorkspaceContextSessionID: parent.String(),
			},
			wantKind:     serverapi.SessionLaunchIntentCreateNew,
			wantParentID: parent,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			intent, _, err := sessionLaunchOptionsFor(test.options)
			if err != nil {
				t.Fatalf("sessionLaunchOptionsFor: %v", err)
			}
			if intent == nil || (intent.GetCreateNew() != nil) != (test.wantKind == serverapi.SessionLaunchIntentCreateNew) {
				t.Fatalf("intent = %+v, want %q", intent, test.wantKind)
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

func TestSessionLaunchOptionsForPreservesShortSessionSelector(t *testing.T) {
	intent, _, err := sessionLaunchOptionsFor(Options{SessionID: "42"})
	if err != nil {
		t.Fatal(err)
	}
	selector, ok := intent.GetOpenExistingSessionId().GetValue().(*sharedpb.SessionSelector_ShortId)
	if !ok || selector.ShortId != 42 {
		t.Fatalf("session selector = %v, want short ID 42", intent.GetOpenExistingSessionId())
	}
}

func TestSessionLaunchOptionsForPreservesAgentRoleOverrides(t *testing.T) {
	intent, overrides, err := sessionLaunchOptionsFor(Options{
		AgentRole: sessionLifecycleStringPtr(config.DefaultSubagentRole),
	})
	if err != nil {
		t.Fatalf("sessionLaunchOptionsFor: %v", err)
	}
	if intent != nil {
		t.Fatal("default subagent role must not force a new session")
	}
	if overrides.AgentRole == nil || *overrides.AgentRole != config.DefaultSubagentRole {
		t.Fatalf("agent role override = %v, want %q", overrides.AgentRole, config.DefaultSubagentRole)
	}
}

func TestSessionLaunchOptionsForRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name    string
		options Options
	}{
		{name: "blank agent role", options: Options{AgentRole: sessionLifecycleStringPtr(" \t ")}},
		{name: "blank session ID", options: Options{SessionID: "   "}},
		{name: "blank workspace context session ID", options: Options{WorkspaceContextSessionID: "   "}},
		{
			name: "conflicting session identities",
			options: Options{
				SessionID:                 "11111111-1111-4111-8111-111111111111",
				WorkspaceContextSessionID: "22222222-2222-4222-8222-222222222222",
			},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := sessionLaunchOptionsFor(test.options); err == nil {
				t.Fatalf("sessionLaunchOptionsFor(%+v) succeeded; want validation error", test.options)
			}
		})
	}
}
