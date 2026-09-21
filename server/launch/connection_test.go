package launch

import (
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	"core/server/session"
	"core/server/session/sessiontest"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestSavedConnectionPrecedesCurrentRoleProviderPreparation(t *testing.T) {
	for _, test := range []struct {
		name           string
		currentDefault string
		roleConnection *string
		saved          config.ConnectionID
		wantError      bool
	}{
		{name: "undefined role connection", currentDefault: "work", roleConnection: launchTestStringPtr("missing"), saved: "work"},
		{name: "undefined inherited default", currentDefault: "missing", saved: "work"},
		{name: "removed binding with undefined replacement", currentDefault: "missing", saved: "removed", wantError: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			workspace := t.TempDir()
			lines := []string{
				fmt.Sprintf("connection = %q", test.currentDefault),
				`[connections.work]`,
				`protocol = "responses"`,
				`endpoint = "http://127.0.0.1:1/v1"`,
				`[subagents.worker]`,
				`model = "gpt-5"`,
			}
			if test.roleConnection != nil {
				lines = append(lines, fmt.Sprintf("connection = %q", *test.roleConnection))
			}
			app := loadLaunchConfig(t, workspace, lines...)
			persistence := sessiontest.NewPersistence()
			store := createTestSessionInContainer(t, t.TempDir(), "sessions", workspace, persistence.Options()...)
			if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: sessiontest.AgentRole("worker")}); err != nil {
				t.Fatal(err)
			}
			if err := store.SetConnectionID(test.saved); err != nil {
				t.Fatal(err)
			}
			if err := store.MarkModelDispatchLocked(session.LockedContract{
				Model: "gpt-5", HasEnabledTools: true, EnabledTools: []string{"exec_command"}, WebSearchMode: "native",
			}); err != nil {
				t.Fatal(err)
			}
			before := store.Meta()
			projection, err := ResolveReadOnlySessionContextSettings(app, before, false)
			if (err != nil) != test.wantError {
				t.Fatalf("read-only bound Session: %v", err)
			}
			if err == nil && (projection.Settings.Connection == nil || *projection.Settings.Connection != "work") {
				t.Fatalf("read-only connection = %v", projection.Settings.Connection)
			}
			snapshot, err := ResolvePromptFacingSnapshotPlan(app, store, false)
			if (err != nil) != test.wantError {
				t.Fatalf("prompt-facing bound Session: %v", err)
			}
			if err == nil && (snapshot.ActiveSettings.Connection == nil || *snapshot.ActiveSettings.Connection != "work") {
				t.Fatalf("prompt-facing connection = %v", snapshot.ActiveSettings.Connection)
			}
			if !reflect.DeepEqual(before, store.Meta()) {
				t.Fatal("read projection mutated Session metadata")
			}
			id, err := runtimeids.ParseSessionID(before.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			planner := newPersistenceBackedTestPlanner(app, filepath.Dir(store.Dir()), persistence)
			plan, err := planner.PlanSession(t.Context(), SessionRequest{
				Mode: ModeHeadless, Intent: serverapi.OpenExistingSessionLaunchIntent(id),
			})
			if (err != nil) != test.wantError {
				t.Fatalf("resume bound Session: %v", err)
			}
			if err == nil && (plan.ActiveSettings.Connection == nil || *plan.ActiveSettings.Connection != config.ConnectionID("work")) {
				t.Fatalf("resume connection = %v", plan.ActiveSettings.Connection)
			}
			if saved := store.Meta().ConnectionID; saved == nil || *saved != test.saved {
				t.Fatalf("planning changed stored binding: %v", saved)
			}
		})
	}
}
