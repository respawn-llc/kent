package launch

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"

	"core/server/session"
	"core/server/session/sessiontest"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestConnectionRotationSharesSetsWithoutAllocatingPreviews(t *testing.T) {
	app := loadLaunchConfig(t, t.TempDir(),
		`connection = ["account-a", "account-z"]`,
		`[connections.account-z]`,
		`protocol = "chatgpt-codex"`,
		`[connections.account-a]`,
		`protocol = "chatgpt-codex"`,
	)
	rotation := new(ConnectionRotation)
	for index := range 4 {
		preview, err := rotation.Prepare(app.Settings, false)
		if err != nil || preview != "account-a" {
			t.Fatalf("preview = %v, %v", preview, err)
		}
		selected, err := rotation.Prepare(app.Settings, true)
		want := []config.ConnectionID{"account-a", "account-z"}[index%2]
		if err != nil || selected != want {
			t.Fatalf("allocation %d = %v, %v; want %v", index, selected, err, want)
		}
		selection := config.ConnectionSelection{"account-z", "account-a"}
		app.Settings.Connection = &selection
	}
}

func TestConnectionRotationConcurrentSelection(t *testing.T) {
	app := loadLaunchConfig(t, t.TempDir(),
		`connection = ["account-z", "account-a"]`,
		`[connections.account-z]`,
		`protocol = "chatgpt-codex"`,
		`[connections.account-a]`,
		`protocol = "chatgpt-codex"`,
	)
	rotation := new(ConnectionRotation)
	results := make(chan config.ConnectionID, 100)
	var group sync.WaitGroup
	for range 100 {
		group.Go(func() {
			id, err := rotation.Prepare(app.Settings, true)
			if err != nil {
				t.Error(err)
				return
			}
			results <- id
		})
	}
	group.Wait()
	close(results)
	counts := map[config.ConnectionID]int{}
	for id := range results {
		counts[id]++
	}
	if counts["account-z"] != 50 || counts["account-a"] != 50 {
		t.Fatalf("concurrent assignments = %v", counts)
	}
}

func TestAllocatedMemberCapabilitiesPreserveAuthoredModelAndPrompt(t *testing.T) {
	app := loadLaunchConfig(t, t.TempDir(),
		`connection = ["a", "b"]`,
		`model = "operator-model"`,
		`[connections.a]`,
		`protocol = "chatgpt-codex"`,
		`[connections.b]`,
		`protocol = "responses"`,
		`endpoint = "http://127.0.0.1:1/v1"`,
		`[connections.b.provider_capabilities]`,
		`provider_id = "custom"`,
		`supports_responses_api = true`,
	)
	prompt := &config.SystemPromptFile{Path: "/operator-prompt.md", Scope: config.SystemPromptFileScopeHomeConfig}
	app.Settings.SystemPromptFile = prompt
	rotation := new(ConnectionRotation)
	for _, want := range []struct {
		id   config.ConnectionID
		fast bool
	}{{"a", true}, {"b", false}} {
		prepared, err := PrepareRunPromptOverridesWithContext(app, serverapi.RunPromptOverrides{}, RunPromptPreparationContext{
			Mode: ModeInteractive, Rotation: rotation, AllocateConnection: true,
		})
		if err != nil {
			t.Fatal(err)
		}
		target := prepared.PromptFacingTarget()
		if target == nil || target.Settings.Model != "operator-model" || !reflect.DeepEqual(target.Settings.SystemPromptFile, prompt) {
			t.Fatalf("authored model/prompt changed: %+v", target)
		}
		if !reflect.DeepEqual(target.Settings.Connection, config.SingleConnection(want.id)) ||
			prepared.ProviderCapabilities == nil || prepared.ProviderCapabilities.SupportsFastMode != want.fast {
			t.Fatalf("actual-member preparation = %+v, capabilities=%+v", target, prepared.ProviderCapabilities)
		}
	}
}

func TestFreshLaunchUsesAllocatedConnectionWithoutSecondSelection(t *testing.T) {
	app := loadLaunchConfig(t, t.TempDir(),
		`connection = ["a", "b"]`,
		`[connections.a]`,
		`protocol = "chatgpt-codex"`,
		`[connections.b]`,
		`protocol = "chatgpt-codex"`,
	)
	rotation := new(ConnectionRotation)
	planner := newTestPlanner(app, t.TempDir(), sessiontest.NewPersistence().Options()...)
	planner.Rotation = rotation
	for _, want := range []config.ConnectionID{"a", "b", "a"} {
		plan, err := planner.PlanSession(t.Context(), SessionRequest{
			Mode: ModeInteractive, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
		})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(plan.ActiveSettings.Connection, config.SingleConnection(want)) {
			t.Fatalf("fresh selected connection = %v; want %s", plan.ActiveSettings.Connection, want)
		}
		store := testStoreForPlannerPlan(t, planner, plan)
		settings := plan.ActiveSettings
		if _, err := BindSessionConnection(store, &settings, plan.Source.Sources); err != nil {
			t.Fatal(err)
		}
		if store.Meta().ConnectionID == nil || *store.Meta().ConnectionID != want {
			t.Fatalf("bound connection = %v; want %s", store.Meta().ConnectionID, want)
		}
	}
}

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
				`model = "gpt-6-sol"`,
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
				Model: "gpt-6-sol", HasEnabledTools: true, EnabledTools: []string{"exec_command"}, WebSearchMode: "native",
			}); err != nil {
				t.Fatal(err)
			}
			before := store.Meta()
			projection, err := ResolveReadOnlySessionContextSettings(app, before, false)
			if (err != nil) != test.wantError {
				t.Fatalf("read-only bound Session: %v", err)
			}
			if err == nil && !reflect.DeepEqual(projection.Settings.Connection, config.SingleConnection("work")) {
				t.Fatalf("read-only connection = %v", projection.Settings.Connection)
			}
			snapshot, err := ResolvePromptFacingSnapshotPlan(app, store, false)
			if (err != nil) != test.wantError {
				t.Fatalf("prompt-facing bound Session: %v", err)
			}
			if err == nil && !reflect.DeepEqual(snapshot.ActiveSettings.Connection, config.SingleConnection("work")) {
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
			if err == nil && !reflect.DeepEqual(plan.ActiveSettings.Connection, config.SingleConnection("work")) {
				t.Fatalf("resume connection = %v", plan.ActiveSettings.Connection)
			}
			if saved := store.Meta().ConnectionID; saved == nil || *saved != test.saved {
				t.Fatalf("planning changed stored binding: %v", saved)
			}
		})
	}
}
