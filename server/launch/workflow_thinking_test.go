package launch

import (
	"testing"

	"core/server/auth"
	"core/server/workflow"
	"core/shared/config"
	"core/shared/serverapi"
)

func TestApplyRunPromptOverridesAppliesWorkflowThinkingAfterRoleResolution(t *testing.T) {
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace)
	loaded.Settings.Subagents["reviewer"] = config.SubagentRole{
		Description: "Reviewer",
		Settings:    config.Settings{Model: "workflow-reviewer"},
		Sources:     map[string]config.Origin{"model": {Kind: config.SourceInput, Property: config.PropertyAddress{Key: "model"}}},
	}
	plan := newLoadedConfigPlan(t, workspace, loaded)
	role := "reviewer"
	thinking, err := workflow.NewThinkingValue("max")
	if err != nil {
		t.Fatalf("NewThinkingValue: %v", err)
	}
	planner, err := testPlannerForPlan(plan)
	if err != nil {
		t.Fatalf("testPlannerForPlan: %v", err)
	}
	store := testStoreForPlanForOverride(plan)
	updated, _, err := planner.ApplyRunPromptOverridesWithStore(
		plan,
		store,
		serverapi.RunPromptOverrides{AgentRole: &role},
		auth.EmptyState(),
		RunPromptOverrideOptions{WorkflowThinking: workflow.SetThinking(thinking)},
	)
	if err != nil {
		t.Fatalf("ApplyRunPromptOverridesWithStore: %v", err)
	}
	if updated.ActiveSettings.Model != "workflow-reviewer" {
		t.Fatalf("model = %q, want workflow-reviewer", updated.ActiveSettings.Model)
	}
	if updated.ActiveSettings.ThinkingLevel != "max" {
		t.Fatalf("thinking level = %q, want max", updated.ActiveSettings.ThinkingLevel)
	}
}

func TestApplyRunPromptOverridesClearsWorkflowThinking(t *testing.T) {
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace)
	plan := newLoadedConfigPlan(t, workspace, loaded)
	planner, err := testPlannerForPlan(plan)
	if err != nil {
		t.Fatalf("testPlannerForPlan: %v", err)
	}
	store := testStoreForPlanForOverride(plan)
	updated, _, err := planner.ApplyRunPromptOverridesWithStore(
		plan,
		store,
		serverapi.RunPromptOverrides{},
		auth.EmptyState(),
		RunPromptOverrideOptions{WorkflowThinking: workflow.ClearThinking()},
	)
	if err != nil {
		t.Fatalf("ApplyRunPromptOverridesWithStore: %v", err)
	}
	if updated.ActiveSettings.ThinkingLevel != loaded.Settings.ThinkingLevel {
		t.Fatalf("thinking level = %q, want configured %q", updated.ActiveSettings.ThinkingLevel, loaded.Settings.ThinkingLevel)
	}
}

func TestPreparedOverridesApplyWorkflowThinkingWithoutWritingRetainedSession(t *testing.T) {
	loaded := loadLaunchConfig(t, t.TempDir())
	plan := newLoadedConfigPlan(t, loaded.WorkspaceRoot, loaded)
	planner, err := testPlannerForPlan(plan)
	if err != nil {
		t.Fatal(err)
	}
	store := testStoreForPlanForOverride(plan)
	before := store.Meta()
	prepared, err := PrepareRunPromptOverridesWithContext(loaded, serverapi.RunPromptOverrides{}, auth.EmptyState(), RunPromptPreparationContext{
		Mode: ModeHeadless, SkipProviderReadinessValidation: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	thinking, err := workflow.NewThinkingValue("max")
	if err != nil {
		t.Fatal(err)
	}
	updated, _, err := planner.ApplyPreparedRunPromptOverridesFromMeta(plan, before, serverapi.RunPromptOverrides{}, prepared, RunPromptOverrideOptions{
		WorkflowThinking: workflow.SetThinking(thinking),
	})
	if err != nil {
		t.Fatal(err)
	}
	if updated.ActiveSettings.ThinkingLevel != "max" {
		t.Fatalf("prepared Thinking = %q, want max", updated.ActiveSettings.ThinkingLevel)
	}
	if !store.Meta().UpdatedAt.Equal(before.UpdatedAt) {
		t.Fatal("prepared overrides persisted retained metadata")
	}
}
