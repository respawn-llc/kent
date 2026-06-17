package launch

import (
	"context"
	"path/filepath"
	"testing"

	"core/server/auth"
	"core/server/session"
	"core/shared/config"
	"core/shared/serverapi"
)

func TestApplyRunPromptOverridesFastRoleContextBudgetForFirstPartyAuth(t *testing.T) {
	tests := []struct {
		name      string
		authState auth.State
	}{
		{
			name:      "api key openai",
			authState: auth.State{Method: auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "test-key"}}},
		},
		{
			name:      "oauth chatgpt codex",
			authState: auth.State{Method: auth.Method{Type: auth.MethodOAuth}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			loaded := loadLaunchConfig(t, workspace)
			plan := newLoadedConfigPlan(t, workspace, loaded)

			updated := applyRunPromptOverridesNoWarnings(t, plan, serverapi.RunPromptOverrides{AgentRole: config.BuiltInSubagentRoleFast}, tt.authState)
			assertFastMiniBudget(t, updated.ActiveSettings)
		})
	}
}

func TestApplyRunPromptOverridesFastRoleReconcilesSameModelSelection(t *testing.T) {
	tests := []struct {
		name      string
		authState auth.State
	}{
		{
			name:      "api key openai",
			authState: auth.State{Method: auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "test-key"}}},
		},
		{
			name:      "oauth chatgpt codex",
			authState: auth.State{Method: auth.Method{Type: auth.MethodOAuth}},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			loaded := loadLaunchConfig(t, workspace, "model = \"gpt-5.4-mini\"")
			plan := newLoadedConfigPlan(t, workspace, loaded)

			updated := applyRunPromptOverridesNoWarnings(t, plan, serverapi.RunPromptOverrides{AgentRole: config.BuiltInSubagentRoleFast}, tt.authState)
			assertFastMiniBudget(t, updated.ActiveSettings)
		})
	}
}

func TestApplyRunPromptOverridesFastRolePreservesExplicitSameModelBudget(t *testing.T) {
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace,
		"model = \"gpt-5.4-mini\"",
		"model_context_window = 300000",
	)
	plan := newLoadedConfigPlan(t, workspace, loaded)

	updated := applyRunPromptOverridesNoWarnings(t, plan, serverapi.RunPromptOverrides{AgentRole: config.BuiltInSubagentRoleFast}, auth.State{Method: auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "test-key"}}})
	if updated.ActiveSettings.ModelContextWindow != 300_000 {
		t.Fatalf("context window = %d, want 300000", updated.ActiveSettings.ModelContextWindow)
	}
	if updated.ActiveSettings.ContextCompactionThresholdTokens != 285_000 {
		t.Fatalf("compaction threshold = %d, want 285000", updated.ActiveSettings.ContextCompactionThresholdTokens)
	}
	if updated.ActiveSettings.Reviewer.ModelContextWindow != 300_000 {
		t.Fatalf("reviewer context window = %d, want 300000", updated.ActiveSettings.Reviewer.ModelContextWindow)
	}
}

func TestApplyRunPromptOverridesFastRoleContextWindowOnlyOverrideDerivesThreshold(t *testing.T) {
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace,
		"[subagents.fast]",
		"model_context_window = 100000",
	)
	plan := newLoadedConfigPlan(t, workspace, loaded)

	updated := applyRunPromptOverridesNoWarnings(t, plan, serverapi.RunPromptOverrides{AgentRole: config.BuiltInSubagentRoleFast}, auth.State{Method: auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "test-key"}}})
	if updated.ActiveSettings.ModelContextWindow != 100_000 {
		t.Fatalf("context window = %d, want 100000", updated.ActiveSettings.ModelContextWindow)
	}
	if updated.ActiveSettings.ContextCompactionThresholdTokens != 95_000 {
		t.Fatalf("compaction threshold = %d, want 95000", updated.ActiveSettings.ContextCompactionThresholdTokens)
	}
	if updated.ActiveSettings.Reviewer.ModelContextWindow != 100_000 {
		t.Fatalf("reviewer context window = %d, want 100000", updated.ActiveSettings.Reviewer.ModelContextWindow)
	}
}

func TestApplyRunPromptOverridesCustomRoleBudgetOnlyOverrideReconcilesWithoutModelSelection(t *testing.T) {
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace,
		"[subagents.worker]",
		"model_context_window = 100000",
	)
	plan := newLoadedConfigPlan(t, workspace, loaded)

	updated := applyRunPromptOverridesNoWarnings(t, plan, serverapi.RunPromptOverrides{AgentRole: "worker"}, auth.EmptyState())
	if updated.ActiveSettings.Model != loaded.Settings.Model {
		t.Fatalf("model = %q, want inherited %q", updated.ActiveSettings.Model, loaded.Settings.Model)
	}
	assertBudget(t, updated.ActiveSettings, 100_000, 95_000, 35_000)
	if updated.ActiveSettings.Reviewer.ModelContextWindow != 100_000 {
		t.Fatalf("reviewer context window = %d, want 100000", updated.ActiveSettings.Reviewer.ModelContextWindow)
	}
}

func TestApplyRunPromptOverridesCustomRoleBudgetOnlyOverrideRejectsInvalidValues(t *testing.T) {
	tests := []struct {
		name        string
		configLines []string
	}{
		{
			name: "threshold",
			configLines: []string{
				"[subagents.worker]",
				"context_compaction_threshold_tokens = 300000",
			},
		},
		{
			name: "pre submit lead",
			configLines: []string{
				"[subagents.worker]",
				"pre_submit_compaction_lead_tokens = 240000",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			loaded := loadLaunchConfig(t, workspace, tt.configLines...)
			plan := newLoadedConfigPlan(t, workspace, loaded)

			if _, _, err := ApplyRunPromptOverrides(plan, serverapi.RunPromptOverrides{AgentRole: "worker"}, auth.EmptyState()); err == nil {
				t.Fatal("expected invalid role budget error")
			}
		})
	}
}

func TestApplyRunPromptOverridesFastRolePreservesTopLevelBudgetSources(t *testing.T) {
	tests := []struct {
		name          string
		configLines   []string
		wantWindow    int
		wantThreshold int
	}{
		{
			name:          "explicit window derives threshold",
			configLines:   []string{"model_context_window = 300000"},
			wantWindow:    300_000,
			wantThreshold: 285_000,
		},
		{
			name:          "explicit window and threshold",
			configLines:   []string{"model_context_window = 128000", "context_compaction_threshold_tokens = 120000"},
			wantWindow:    128_000,
			wantThreshold: 120_000,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			loaded := loadLaunchConfig(t, workspace, tt.configLines...)
			plan := newLoadedConfigPlan(t, workspace, loaded)

			updated := applyRunPromptOverridesNoWarnings(t, plan, serverapi.RunPromptOverrides{AgentRole: config.BuiltInSubagentRoleFast}, auth.State{Method: auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "test-key"}}})
			if updated.ActiveSettings.ModelContextWindow != tt.wantWindow {
				t.Fatalf("context window = %d, want %d", updated.ActiveSettings.ModelContextWindow, tt.wantWindow)
			}
			if updated.ActiveSettings.ContextCompactionThresholdTokens != tt.wantThreshold {
				t.Fatalf("compaction threshold = %d, want %d", updated.ActiveSettings.ContextCompactionThresholdTokens, tt.wantThreshold)
			}
			if updated.ActiveSettings.Reviewer.ModelContextWindow != tt.wantWindow {
				t.Fatalf("reviewer context window = %d, want %d", updated.ActiveSettings.Reviewer.ModelContextWindow, tt.wantWindow)
			}
		})
	}
}

func TestApplyRunPromptOverridesFastRoleRejectsInvalidExplicitBudgetAfterDerivation(t *testing.T) {
	tests := []struct {
		name        string
		configLines []string
	}{
		{
			name:        "threshold",
			configLines: []string{"context_compaction_threshold_tokens = 180000"},
		},
		{
			name:        "pre submit lead",
			configLines: []string{"pre_submit_compaction_lead_tokens = 70000"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			loaded := loadLaunchConfig(t, workspace, tt.configLines...)
			plan := newLoadedConfigPlan(t, workspace, loaded)

			if _, _, err := ApplyRunPromptOverrides(plan, serverapi.RunPromptOverrides{AgentRole: config.BuiltInSubagentRoleFast}, auth.State{Method: auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "test-key"}}}); err == nil {
				t.Fatal("expected invalid derived context budget error")
			}
		})
	}
}

func TestApplyRunPromptOverridesFastRoleUnknownModelFallsBackToBaseBudget(t *testing.T) {
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace,
		"[subagents.fast]",
		"model = \"my-team-alias\"",
	)
	plan := newLoadedConfigPlan(t, workspace, loaded)

	updated := applyRunPromptOverridesNoWarnings(t, plan, serverapi.RunPromptOverrides{AgentRole: config.BuiltInSubagentRoleFast}, auth.State{Method: auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "test-key"}}})
	if updated.ActiveSettings.Model != "my-team-alias" {
		t.Fatalf("model = %q, want my-team-alias", updated.ActiveSettings.Model)
	}
	assertBudget(t, updated.ActiveSettings, loaded.Settings.ModelContextWindow, loaded.Settings.ContextCompactionThresholdTokens, loaded.Settings.PreSubmitCompactionLeadTokens)
}

func TestApplyRunPromptOverridesFastRoleCLIUnknownModelFallsBackToBaseBudget(t *testing.T) {
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace, "model_context_window = 300000")
	plan := newLoadedConfigPlan(t, workspace, loaded)

	updated := applyRunPromptOverridesNoWarnings(t, plan, serverapi.RunPromptOverrides{
		AgentRole: config.BuiltInSubagentRoleFast,
		Model:     "my-team-alias",
	}, auth.State{Method: auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "test-key"}}})
	if updated.ActiveSettings.Model != "my-team-alias" {
		t.Fatalf("model = %q, want my-team-alias", updated.ActiveSettings.Model)
	}
	assertBudget(t, updated.ActiveSettings, 300_000, 285_000, 35_000)
	if updated.ActiveSettings.Reviewer.Model != "my-team-alias" {
		t.Fatalf("reviewer model = %q, want my-team-alias", updated.ActiveSettings.Reviewer.Model)
	}
	if updated.ActiveSettings.Reviewer.ModelContextWindow != 300_000 {
		t.Fatalf("reviewer context window = %d, want 300000", updated.ActiveSettings.Reviewer.ModelContextWindow)
	}
}

func TestApplyRunPromptOverridesFastRoleCLIKnownModelUpdatesReviewerContext(t *testing.T) {
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace)
	plan := newLoadedConfigPlan(t, workspace, loaded)

	updated := applyRunPromptOverridesNoWarnings(t, plan, serverapi.RunPromptOverrides{
		AgentRole: config.BuiltInSubagentRoleFast,
		Model:     "gpt-5.3-codex-spark",
	}, auth.State{Method: auth.Method{Type: auth.MethodAPIKey, APIKey: &auth.APIKeyMethod{Key: "test-key"}}})
	if updated.ActiveSettings.Reviewer.Model != "gpt-5.3-codex-spark" {
		t.Fatalf("reviewer model = %q, want gpt-5.3-codex-spark", updated.ActiveSettings.Reviewer.Model)
	}
	if updated.ActiveSettings.Reviewer.ModelContextWindow != 128_000 {
		t.Fatalf("reviewer context window = %d, want 128000", updated.ActiveSettings.Reviewer.ModelContextWindow)
	}
}

func TestPlannerResumeFastRoleReconcilesContextBudget(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace,
		"model = \"gpt-5.5\"",
		"provider_override = \"openai\"",
	)
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	store := createTestSessionInContainer(t, containerDir, "workspace-a", workspace)
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: config.BuiltInSubagentRoleFast}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}
	planner := Planner{Config: config.App{WorkspaceRoot: workspace, PersistenceRoot: root, Settings: loaded.Settings, Source: loaded.Source}, ContainerDir: containerDir}

	plan, err := planner.PlanSession(context.Background(), SessionRequest{Mode: ModeInteractive, SelectedSessionID: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("PlanSession: %v", err)
	}
	assertFastMiniBudget(t, plan.ActiveSettings)
}

func TestPlannerResumeFastRoleRejectsInvalidExplicitBudgetAfterDerivation(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace,
		"model = \"gpt-5.5\"",
		"provider_override = \"openai\"",
		"context_compaction_threshold_tokens = 180000",
	)
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	store := createTestSessionInContainer(t, containerDir, "workspace-a", workspace)
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: config.BuiltInSubagentRoleFast}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}
	planner := Planner{Config: config.App{WorkspaceRoot: workspace, PersistenceRoot: root, Settings: loaded.Settings, Source: loaded.Source}, ContainerDir: containerDir}

	if _, err := planner.PlanSession(context.Background(), SessionRequest{Mode: ModeInteractive, SelectedSessionID: store.Meta().SessionID}); err == nil {
		t.Fatal("expected invalid persisted fast role budget error")
	}
}

func TestPlannerLockedResumeFastRoleUsesLockedContextBudget(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace,
		"model = \"gpt-5.5\"",
		"provider_override = \"openai\"",
	)
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	store := createTestSessionInContainer(t, containerDir, "workspace-a", workspace)
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: config.BuiltInSubagentRoleFast}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}
	if err := store.MarkModelDispatchLocked(session.LockedContract{Model: "gpt-5.4-mini", ContextWindow: 128_000, ContextPercent: 95, EnabledTools: []string{"shell"}}); err != nil {
		t.Fatalf("MarkModelDispatchLocked: %v", err)
	}
	planner := Planner{Config: config.App{WorkspaceRoot: workspace, PersistenceRoot: root, Settings: loaded.Settings, Source: loaded.Source}, ContainerDir: containerDir}

	plan, err := planner.PlanSession(context.Background(), SessionRequest{Mode: ModeInteractive, SelectedSessionID: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("PlanSession: %v", err)
	}
	assertBudget(t, plan.ActiveSettings, 128_000, 121_600, 35_000)
}

func TestPlannerLockedResumeWithoutContinuationReconcilesContextBudget(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace)
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	store := createTestSessionInContainer(t, containerDir, "workspace-a", workspace)
	if err := store.MarkModelDispatchLocked(session.LockedContract{Model: "gpt-5.4-mini", ContextWindow: 128_000, ContextPercent: 95, EnabledTools: []string{"shell"}}); err != nil {
		t.Fatalf("MarkModelDispatchLocked: %v", err)
	}
	planner := Planner{Config: config.App{WorkspaceRoot: workspace, PersistenceRoot: root, Settings: loaded.Settings, Source: loaded.Source}, ContainerDir: containerDir}

	plan, err := planner.PlanSession(context.Background(), SessionRequest{Mode: ModeInteractive, SelectedSessionID: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("PlanSession: %v", err)
	}
	assertBudget(t, plan.ActiveSettings, 128_000, 121_600, 35_000)
}

func TestPlannerLockedResumeFastRolePreservesCompatibleBaseThreshold(t *testing.T) {
	root := t.TempDir()
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace,
		"model = \"gpt-5.5\"",
		"provider_override = \"openai\"",
		"context_compaction_threshold_tokens = 180000",
		"",
		"[subagents.fast]",
		"model_context_window = 100000",
	)
	containerDir := filepath.Join(root, "projects", "project-a", "sessions")
	store := createTestSessionInContainer(t, containerDir, "workspace-a", workspace)
	if err := store.SetContinuationContext(session.ContinuationContext{AgentRole: config.BuiltInSubagentRoleFast}); err != nil {
		t.Fatalf("SetContinuationContext: %v", err)
	}
	if err := store.MarkModelDispatchLocked(session.LockedContract{Model: "gpt-5.4", ContextWindow: 272_000, ContextPercent: 95, EnabledTools: []string{"shell"}}); err != nil {
		t.Fatalf("MarkModelDispatchLocked: %v", err)
	}
	planner := Planner{Config: config.App{WorkspaceRoot: workspace, PersistenceRoot: root, Settings: loaded.Settings, Source: loaded.Source}, ContainerDir: containerDir}

	plan, err := planner.PlanSession(context.Background(), SessionRequest{Mode: ModeInteractive, SelectedSessionID: store.Meta().SessionID})
	if err != nil {
		t.Fatalf("PlanSession: %v", err)
	}
	assertBudget(t, plan.ActiveSettings, 272_000, 180_000, 35_000)
}

func assertFastMiniBudget(t *testing.T, settings config.Settings) {
	t.Helper()
	if settings.Model != "gpt-5.4-mini" {
		t.Fatalf("model = %q, want gpt-5.4-mini", settings.Model)
	}
	if !settings.PriorityRequestMode {
		t.Fatal("expected priority request mode")
	}
	assertBudget(t, settings, 128_000, 121_600, 35_000)
	if settings.Reviewer.Model != "gpt-5.4-mini" {
		t.Fatalf("reviewer model = %q, want gpt-5.4-mini", settings.Reviewer.Model)
	}
	if settings.Reviewer.ModelContextWindow != 128_000 {
		t.Fatalf("reviewer context window = %d, want 128000", settings.Reviewer.ModelContextWindow)
	}
}

func assertBudget(t *testing.T, settings config.Settings, wantWindow int, wantThreshold int, wantLead int) {
	t.Helper()
	if settings.ModelContextWindow != wantWindow {
		t.Fatalf("context window = %d, want %d", settings.ModelContextWindow, wantWindow)
	}
	if settings.ContextCompactionThresholdTokens != wantThreshold {
		t.Fatalf("compaction threshold = %d, want %d", settings.ContextCompactionThresholdTokens, wantThreshold)
	}
	if settings.PreSubmitCompactionLeadTokens != wantLead {
		t.Fatalf("pre-submit lead = %d, want %d", settings.PreSubmitCompactionLeadTokens, wantLead)
	}
}
