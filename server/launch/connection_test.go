package launch

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"core/internal/testharness/testsetup"
	"core/server/sessionstore"
	"core/server/sessionstore/sessiontest"
	"core/shared/config"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestConnectionUsageCooldownSkipsExhaustedMember(t *testing.T) {
	settings := config.Settings{
		Connection:      &config.ConnectionSelection{"a", "b"},
		ConnectionOrder: []config.ConnectionID{"a", "b"},
		Connections: map[config.ConnectionID]config.ProviderConnection{
			"a": {Protocol: config.ConnectionChatGPT},
			"b": {Protocol: config.ConnectionChatGPT},
		},
	}
	rotation := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Minute)
	first, err := rotation.Prepare(t.Context(), settings, true)
	if err != nil || first != "a" {
		t.Fatalf("first allocation = %v, %v", first, err)
	}
	rotation.RecordExhaustion(first)
	for range 2 {
		next, err := rotation.Prepare(t.Context(), settings, true)
		if err != nil || next != "b" {
			t.Fatalf("cooldown allocation = %v, %v", next, err)
		}
	}
}

func TestConnectionUsageFallbackDisabledAndExpiry(t *testing.T) {
	settings := config.Settings{
		Connection:  config.SingleConnection("a"),
		Connections: map[config.ConnectionID]config.ProviderConnection{"a": {Protocol: config.ConnectionChatGPT}},
	}
	disabled := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	disabled.RecordExhaustion("a")
	if _, err := disabled.Prepare(t.Context(), settings, true); err != nil {
		t.Fatal(err)
	}
	rotation := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 10*time.Millisecond)
	rotation.RecordExhaustion("a")
	_, err := rotation.Prepare(t.Context(), settings, true)
	var unavailable *ConnectionsUnavailableError
	if !errors.As(err, &unavailable) || len(unavailable.Members) != 1 || unavailable.Members[0].AvailableAt == nil {
		t.Fatalf("unavailable = %v", err)
	}
	time.Sleep(time.Until(*unavailable.Members[0].AvailableAt))
	if selected, err := rotation.Prepare(t.Context(), settings, true); err != nil || selected != "a" {
		t.Fatalf("after expiry = %s, %v", selected, err)
	}
}

func TestConnectionUsageSelectionExcludesVisitedMembers(t *testing.T) {
	settings := config.Settings{
		ConnectionOrder: []config.ConnectionID{"a", "b"},
		Connection:      &config.ConnectionSelection{"a", "b"},
		Connections: map[config.ConnectionID]config.ProviderConnection{
			"a": {Protocol: config.ConnectionChatGPT}, "b": {Protocol: config.ConnectionChatGPT},
		},
	}
	rotation := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Minute)
	scope := config.ConnectionScope{Selection: settings.Connection}
	selected, err := rotation.SelectReplacement(t.Context(), settings, scope, map[config.ConnectionID]struct{}{"a": {}})
	if err != nil || selected != "b" {
		t.Fatalf("replacement = %s, %v", selected, err)
	}
	_, err = rotation.SelectReplacement(t.Context(), settings, scope, map[config.ConnectionID]struct{}{"a": {}, "b": {}})
	var unavailable *ConnectionsUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("visited error = %v", err)
	}
}

func TestConnectionAllocationPrefersConsumptionBeforeLRU(t *testing.T) {
	store := testsetup.OpenStore(t, t.TempDir())
	rotation := NewConnectionRotation(store, 0)
	settings := config.Settings{
		Connection:      config.SingleConnection("a"),
		ConnectionOrder: []config.ConnectionID{"a", "b"},
		Connections: map[config.ConnectionID]config.ProviderConnection{
			"a": {Protocol: config.ConnectionChatGPT}, "b": {Protocol: config.ConnectionChatGPT},
		},
	}
	if _, err := rotation.Prepare(t.Context(), settings, true); err != nil {
		t.Fatal(err)
	}
	if err := store.RecordConnectionConsumption(t.Context(), "b", time.Now(), 42); err != nil {
		t.Fatal(err)
	}
	members := config.ConnectionSelection{"a", "b"}
	settings.Connection = &members
	for _, allocate := range []bool{false, true} {
		selected, err := rotation.Prepare(t.Context(), settings, allocate)
		if err != nil || selected != "a" {
			t.Fatalf("allocate %v: selected %q, error %v; want lower-cost a", allocate, selected, err)
		}
	}
	selectedFallback, err := rotation.SelectReplacement(t.Context(), settings, config.ConnectionScope{Selection: &members}, nil)
	if err != nil || selectedFallback != "a" {
		t.Fatalf("fallback selected %q, error %v; want lower-consumption a", selectedFallback, err)
	}
	if err := store.RecordConnectionConsumption(t.Context(), "a", time.Now(), 42); err != nil {
		t.Fatal(err)
	}
	selected, err := rotation.Prepare(t.Context(), settings, true)
	if err != nil || selected != "b" {
		t.Fatalf("equal cost selected %q, error %v; want LRU b", selected, err)
	}
}

func TestConnectionRotationSharesSetsWithoutAllocatingPreviews(t *testing.T) {
	app := loadLaunchConfig(t, t.TempDir(),
		`connection = ["account-a", "account-z"]`,
		`[connections.account-z]`,
		`protocol = "chatgpt-codex"`,
		`[connections.account-a]`,
		`protocol = "chatgpt-codex"`,
	)
	rotation := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	for index := range 4 {
		want := []config.ConnectionID{"account-a", "account-z"}[index%2]
		for range 2 {
			preview, err := rotation.Prepare(t.Context(), app.Settings, false)
			if err != nil || preview != want {
				t.Fatalf("preview = %v, %v; want %v", preview, err, want)
			}
		}
		selected, err := rotation.Prepare(t.Context(), app.Settings, true)
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
	rotation := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	results := make(chan config.ConnectionID, 100)
	var group sync.WaitGroup
	for range 100 {
		group.Go(func() {
			preview, err := rotation.Prepare(t.Context(), app.Settings, false)
			if err != nil || (preview != "account-z" && preview != "account-a") {
				t.Errorf("concurrent preview = %s, %v", preview, err)
				return
			}
			id, err := rotation.Prepare(t.Context(), app.Settings, true)
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

func TestConnectionAllocationUsesGlobalLRUAcrossAllowedSets(t *testing.T) {
	settings := config.Settings{
		ConnectionOrder: []config.ConnectionID{"z", "a", "b"},
		Connections: map[config.ConnectionID]config.ProviderConnection{
			"z": {Protocol: config.ConnectionChatGPT},
			"a": {Protocol: config.ConnectionChatGPT},
			"b": {Protocol: config.ConnectionChatGPT},
		},
	}
	allocator := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	for index, step := range []struct {
		allowed config.ConnectionSelection
		want    config.ConnectionID
	}{
		{config.ConnectionSelection{"a", "z"}, "z"},
		{config.ConnectionSelection{"z"}, "z"},
		{config.ConnectionSelection{"b", "a"}, "a"},
		{config.ConnectionSelection{"z", "a"}, "z"},
		{config.ConnectionSelection{"a", "b", "z"}, "b"},
		{config.ConnectionSelection{"a", "z"}, "a"},
	} {
		settings.Connection = &step.allowed
		got, err := allocator.Prepare(t.Context(), settings, true)
		if err != nil || got != step.want {
			t.Fatalf("allocation %d for %v = %q, %v; want %q", index, step.allowed, got, err, step.want)
		}
	}
	selection := config.ConnectionSelection{"a", "b", "z"}
	settings.Connection = &selection
	restarted := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	got, err := restarted.Prepare(t.Context(), settings, true)
	if err != nil || got != "z" {
		t.Fatalf("first allocation after restart = %q, %v; want declaration-order z", got, err)
	}
}

func TestBindingOneAllowedConnectionParticipatesInGlobalLRU(t *testing.T) {
	app := loadLaunchConfig(t, t.TempDir(),
		`connection = "a"`,
		`[connections.a]`,
		`protocol = "chatgpt-codex"`,
		`[connections.b]`,
		`protocol = "chatgpt-codex"`,
	)
	allocator := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	persistence := sessiontest.NewPersistence()
	store := createTestSessionInContainer(t, t.TempDir(), "sessions", app.WorkspaceRoot, persistence.Options()...)
	scope := config.ConnectionScope{Selection: app.Settings.Connection}
	if _, err := allocator.BindSessionConnection(t.Context(), store, &app.Settings, app.Source.Sources, scope); err != nil {
		t.Fatal(err)
	}
	selection := config.ConnectionSelection{"a", "b"}
	app.Settings.Connection = &selection
	scope.Selection = &selection
	got, err := allocator.Prepare(t.Context(), app.Settings, true)
	if err != nil || got != "b" {
		t.Fatalf("allocation after binding a = %q, %v; want b", got, err)
	}
	if _, err := allocator.BindSessionConnection(t.Context(), store, &app.Settings, app.Source.Sources, scope); err != nil {
		t.Fatal(err)
	}
	app.Settings.Connection = &selection
	got, err = allocator.Prepare(t.Context(), app.Settings, true)
	if err != nil || got != "a" {
		t.Fatalf("allocation after resuming a = %q, %v; want a", got, err)
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
	rotation := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
	for _, want := range []struct {
		id   config.ConnectionID
		fast bool
	}{{"a", true}, {"b", false}} {
		prepared, err := PrepareRunPromptOverridesWithContext(t.Context(), app, serverapi.RunPromptOverrides{}, RunPromptPreparationContext{
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
	rotation := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), 0)
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
		if _, err := BindSessionConnection(store, &settings, plan.Source.Sources, plan.ConnectionScope); err != nil {
			t.Fatal(err)
		}
		if store.Meta().ConnectionID == nil || *store.Meta().ConnectionID != want {
			t.Fatalf("bound connection = %v; want %s", store.Meta().ConnectionID, want)
		}
	}
	app.Settings.Connection = config.SingleConnection("a")
	delete(app.Settings.Connections, "a")
	planner.Config = app
	plan, err := planner.PlanSession(t.Context(), SessionRequest{
		Mode: ModeInteractive, Intent: serverapi.CreateNewSessionLaunchIntent(serverapi.IndependentSessionCreateOrigin()),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.ActiveSettings.Connection, config.SingleConnection("b")) {
		t.Fatalf("fresh Session with a removed single-ID default selected %v; want b", plan.ActiveSettings.Connection)
	}
	if !reflect.DeepEqual(planner.Config.Settings.Connection, config.SingleConnection("a")) {
		t.Fatalf("removed default selector changed to %v", planner.Config.Settings.Connection)
	}
	if !reflect.DeepEqual(plan.ConnectionScope.Selection, config.SingleConnection("a")) {
		t.Fatalf("launch selector changed to %v", plan.ConnectionScope.Selection)
	}
	store := testStoreForPlannerPlan(t, planner, plan)
	if store.Meta().ConnectionID == nil || *store.Meta().ConnectionID != "b" {
		t.Fatalf("fresh Session binding = %v; want b", store.Meta().ConnectionID)
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
		{name: "removed binding falls back from a missing top-level selector", currentDefault: "missing", saved: "removed"},
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
			if err == nil && test.roleConnection != nil {
				_, err := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Minute).SelectReplacement(t.Context(), snapshot.ActiveSettings, snapshot.ConnectionScope, nil)
				if err == nil {
					t.Fatal("undefined role selection borrowed the saved/global connection")
				}
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

func TestPreparedAgentReplacementStaysInsideConfiguredSelection(t *testing.T) {
	app := loadLaunchConfig(t, t.TempDir(),
		`connection = ["outside", "a", "b"]`,
		`[connections.outside]`, `protocol = "chatgpt-codex"`,
		`[connections.a]`, `protocol = "chatgpt-codex"`,
		`[connections.b]`, `protocol = "chatgpt-codex"`,
		`[subagents.worker]`, `connection = ["a", "b"]`,
		`[subagents.default]`, `connection = ["a", "b"]`,
	)
	for _, role := range []string{"worker", "default"} {
		t.Run(role, func(t *testing.T) {
			rotation := NewConnectionRotation(testsetup.OpenStore(t, t.TempDir()), time.Minute)
			prepared, err := PrepareRunPromptOverridesWithContext(t.Context(), app, serverapi.RunPromptOverrides{AgentRole: &role}, RunPromptPreparationContext{
				Mode: ModeHeadless, Rotation: rotation, AllocateConnection: true,
			})
			if err != nil {
				t.Fatal(err)
			}
			target := prepared.PromptFacingTarget()
			selected, err := rotation.SelectReplacement(t.Context(), target.Settings, target.ConnectionScope, map[config.ConnectionID]struct{}{"a": {}})
			if err != nil || selected != "b" {
				t.Fatalf("role fallback = %s, %v", selected, err)
			}
		})
	}
}
