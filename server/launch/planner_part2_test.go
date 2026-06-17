package launch

import (
	"context"
	"core/server/auth"
	"core/server/session"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/toolspec"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestApplyRunPromptOverridesCLIModelOverridePreservesExplicitThreshold(t *testing.T) {
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace,
		"model = \"gpt-5.4\"",
		"model_context_window = 128000",
		"context_compaction_threshold_tokens = 120000",
	)
	store := createTestSession(t, workspace)
	plan := SessionPlan{
		Store:               store,
		ActiveSettings:      loaded.Settings,
		EnabledTools:        []toolspec.ID{toolspec.ToolExecCommand},
		ConfiguredModelName: loaded.Settings.Model,
		WorkspaceRoot:       workspace,
		Source:              loaded.Source,
	}

	updated := applyRunPromptOverridesNoWarnings(t, plan, serverapi.RunPromptOverrides{Model: "gpt-5.4-mini"}, auth.EmptyState())
	if updated.ActiveSettings.Model != "gpt-5.4-mini" {
		t.Fatalf("model = %q, want gpt-5.4-mini", updated.ActiveSettings.Model)
	}
	if updated.ActiveSettings.ModelContextWindow != 128_000 {
		t.Fatalf("context window = %d, want 128000", updated.ActiveSettings.ModelContextWindow)
	}
	if updated.ActiveSettings.ContextCompactionThresholdTokens != 120_000 {
		t.Fatalf("compaction threshold = %d, want 120000", updated.ActiveSettings.ContextCompactionThresholdTokens)
	}
}

func TestApplyRunPromptOverridesCLIModelOverrideRejectsInvalidExplicitBudget(t *testing.T) {
	tests := []struct {
		name        string
		configLines []string
	}{
		{
			name:        "threshold",
			configLines: []string{"model = \"gpt-5.4\"", "context_compaction_threshold_tokens = 180000"},
		},
		{
			name:        "pre submit lead",
			configLines: []string{"model = \"gpt-5.4\"", "pre_submit_compaction_lead_tokens = 70000"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			loaded := loadLaunchConfig(t, workspace, tt.configLines...)
			plan := newLoadedConfigPlan(t, workspace, loaded)

			if _, _, err := ApplyRunPromptOverrides(plan, serverapi.RunPromptOverrides{Model: "gpt-5.4-mini"}, auth.EmptyState()); err == nil {
				t.Fatal("expected invalid derived context budget error")
			}
		})
	}
}

func TestApplyRunPromptOverridesCLIModelOverrideReconcilesSameModelSelection(t *testing.T) {
	workspace := t.TempDir()
	loaded := loadLaunchConfig(t, workspace, "model = \"gpt-5.4-mini\"")
	plan := newLoadedConfigPlan(t, workspace, loaded)

	updated := applyRunPromptOverridesNoWarnings(t, plan, serverapi.RunPromptOverrides{Model: "gpt-5.4-mini"}, auth.EmptyState())
	if updated.ActiveSettings.ModelContextWindow != 128_000 {
		t.Fatalf("context window = %d, want 128000", updated.ActiveSettings.ModelContextWindow)
	}
	if updated.ActiveSettings.ContextCompactionThresholdTokens != 121_600 {
		t.Fatalf("compaction threshold = %d, want 121600", updated.ActiveSettings.ContextCompactionThresholdTokens)
	}
	if updated.ActiveSettings.Reviewer.ModelContextWindow != 128_000 {
		t.Fatalf("reviewer context window = %d, want 128000", updated.ActiveSettings.Reviewer.ModelContextWindow)
	}
}

func TestPlannerInteractiveReopensSelectedSessionWithinActiveContainer(t *testing.T) {
	root := t.TempDir()
	containerA := filepath.Join(root, "projects", "project-a", "sessions")
	containerB := filepath.Join(root, "projects", "project-b", "sessions")
	selected := createTestSessionInContainer(t, containerA, "sessions", "/tmp/workspace-a")
	if err := selected.SetName("selected"); err != nil {
		t.Fatalf("persist selected session meta: %v", err)
	}
	writeDuplicateSessionMeta(t, filepath.Join(containerB, selected.Meta().SessionID), selected.Meta(), "/tmp/workspace-b", "duplicate")
	planner := Planner{
		Config:       config.App{WorkspaceRoot: "/tmp/workspace-a", PersistenceRoot: root, Settings: config.Settings{}},
		ContainerDir: containerA,
	}

	plan, err := planner.PlanSession(context.Background(), SessionRequest{Mode: ModeInteractive, SelectedSessionID: selected.Meta().SessionID})
	if err != nil {
		t.Fatalf("plan session: %v", err)
	}
	requireSameSessionDir(t, plan.Store.Dir(), selected.Dir())
	if plan.Store.Meta().WorkspaceRoot != "/tmp/workspace-a" {
		t.Fatalf("opened workspace root = %q, want /tmp/workspace-a", plan.Store.Meta().WorkspaceRoot)
	}
}

func TestPlannerSelectedSessionIDUsesActiveContainerScope(t *testing.T) {
	root := t.TempDir()
	containerA := filepath.Join(root, "projects", "project-a", "sessions")
	containerB := filepath.Join(root, "projects", "project-b", "sessions")
	selected := createTestSessionInContainer(t, containerA, "sessions", "/tmp/workspace-a")
	if err := selected.SetName("selected"); err != nil {
		t.Fatalf("persist selected session meta: %v", err)
	}
	writeDuplicateSessionMeta(t, filepath.Join(containerB, selected.Meta().SessionID), selected.Meta(), "/tmp/workspace-b", "")
	planner := Planner{
		Config:       config.App{WorkspaceRoot: "/tmp/workspace-a", PersistenceRoot: root, Settings: config.Settings{}},
		ContainerDir: containerA,
	}

	plan, err := planner.PlanSession(context.Background(), SessionRequest{Mode: ModeInteractive, SelectedSessionID: selected.Meta().SessionID})
	if err != nil {
		t.Fatalf("plan session: %v", err)
	}
	requireSameSessionDir(t, plan.Store.Dir(), selected.Dir())
}

func TestPlannerSelectedSessionIDDoesNotFallbackOutsideActiveContainer(t *testing.T) {
	root := t.TempDir()
	projectContainer := filepath.Join(root, "projects", "project-123", "sessions")
	otherProjectContainer := filepath.Join(root, "projects", "project-456", "sessions")
	if err := os.MkdirAll(projectContainer, 0o755); err != nil {
		t.Fatalf("mkdir project container: %v", err)
	}
	otherProjectSession := createTestSessionInContainer(t, otherProjectContainer, "sessions", "/tmp/other-project-workspace")
	if err := otherProjectSession.SetName("other project session"); err != nil {
		t.Fatalf("persist other project session meta: %v", err)
	}
	planner := Planner{
		Config:       config.App{WorkspaceRoot: "/tmp/project-workspace", PersistenceRoot: root, Settings: config.Settings{}},
		ContainerDir: projectContainer,
	}

	_, err := planner.PlanSession(context.Background(), SessionRequest{Mode: ModeInteractive, SelectedSessionID: otherProjectSession.Meta().SessionID})
	if err == nil || !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("plan session err = %v, want ErrSessionNotFound", err)
	}
}

func TestPlannerSelectedSessionIDRejectsSymlinkOutsideActiveContainer(t *testing.T) {
	root := t.TempDir()
	containerA := filepath.Join(root, "projects", "project-a", "sessions")
	containerB := filepath.Join(root, "projects", "project-b", "sessions")
	if err := os.MkdirAll(containerA, 0o755); err != nil {
		t.Fatalf("mkdir container A: %v", err)
	}
	escaped := createTestSessionInContainer(t, containerB, "sessions", "/tmp/workspace-b")
	if err := os.Symlink(escaped.Dir(), filepath.Join(containerA, "escaped-link")); err != nil {
		t.Fatalf("symlink escaped session: %v", err)
	}
	planner := Planner{
		Config:       config.App{WorkspaceRoot: "/tmp/workspace-a", PersistenceRoot: root, Settings: config.Settings{}},
		ContainerDir: containerA,
	}

	if _, err := planner.PlanSession(context.Background(), SessionRequest{Mode: ModeInteractive, SelectedSessionID: "escaped-link"}); err == nil {
		t.Fatal("expected planner to reject symlinked selected session outside active container")
	}
}
