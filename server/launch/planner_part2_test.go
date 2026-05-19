package launch

import (
	"builder/server/auth"
	"builder/server/session"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/toolspec"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyRunPromptOverridesCLIModelOverridePreservesExplicitThreshold(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)
	configPath := filepath.Join(home, ".builder", "config.toml")
	if err := os.MkdirAll(filepath.Dir(configPath), 0o755); err != nil {
		t.Fatalf("mkdir config dir: %v", err)
	}
	contents := strings.Join([]string{
		"model = \"gpt-5.4\"",
		"context_compaction_threshold_tokens = 180000",
	}, "\n")
	if err := os.WriteFile(configPath, []byte(contents), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	loaded, err := config.Load(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := session.Create(filepath.Join(t.TempDir(), "projects", "project-a", "sessions"), "workspace-a", workspace)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	plan := SessionPlan{
		Store:               store,
		ActiveSettings:      loaded.Settings,
		EnabledTools:        []toolspec.ID{toolspec.ToolExecCommand},
		ConfiguredModelName: loaded.Settings.Model,
		WorkspaceRoot:       workspace,
		Source:              loaded.Source,
	}

	updated, warnings, err := ApplyRunPromptOverrides(plan, serverapi.RunPromptOverrides{Model: "gpt-5.4-mini"}, auth.EmptyState())
	if err != nil {
		t.Fatalf("ApplyRunPromptOverrides: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("unexpected warnings: %+v", warnings)
	}
	if updated.ActiveSettings.Model != "gpt-5.4-mini" {
		t.Fatalf("model = %q, want gpt-5.4-mini", updated.ActiveSettings.Model)
	}
	if updated.ActiveSettings.ModelContextWindow != 272_000 {
		t.Fatalf("context window = %d, want 272000", updated.ActiveSettings.ModelContextWindow)
	}
	if updated.ActiveSettings.ContextCompactionThresholdTokens != 180_000 {
		t.Fatalf("compaction threshold = %d, want 180000", updated.ActiveSettings.ContextCompactionThresholdTokens)
	}
}

func TestPlannerInteractiveReopensSelectedSessionWithinActiveContainer(t *testing.T) {
	root := t.TempDir()
	containerA := filepath.Join(root, "projects", "project-a", "sessions")
	containerB := filepath.Join(root, "projects", "project-b", "sessions")
	selected, err := session.Create(containerA, "sessions", "/tmp/workspace-a")
	if err != nil {
		t.Fatalf("create selected session: %v", err)
	}
	if err := selected.SetName("selected"); err != nil {
		t.Fatalf("persist selected session meta: %v", err)
	}
	otherDir := filepath.Join(containerB, selected.Meta().SessionID)
	if err := os.MkdirAll(otherDir, 0o755); err != nil {
		t.Fatalf("mkdir duplicate session dir: %v", err)
	}
	duplicateMeta := selected.Meta()
	duplicateMeta.WorkspaceContainer = "sessions"
	duplicateMeta.WorkspaceRoot = "/tmp/workspace-b"
	duplicateMeta.Name = "duplicate"
	duplicateData, err := json.Marshal(duplicateMeta)
	if err != nil {
		t.Fatalf("marshal duplicate session meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "session.json"), duplicateData, 0o644); err != nil {
		t.Fatalf("write duplicate session meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(otherDir, "events.jsonl"), nil, 0o644); err != nil {
		t.Fatalf("write duplicate session events: %v", err)
	}
	planner := Planner{
		Config:       config.App{WorkspaceRoot: "/tmp/workspace-a", PersistenceRoot: root, Settings: config.Settings{}},
		ContainerDir: containerA,
	}

	plan, err := planner.PlanSession(context.Background(), SessionRequest{Mode: ModeInteractive, SelectedSessionID: selected.Meta().SessionID})
	if err != nil {
		t.Fatalf("plan session: %v", err)
	}
	openedDir, err := filepath.EvalSymlinks(plan.Store.Dir())
	if err != nil {
		t.Fatalf("EvalSymlinks opened dir: %v", err)
	}
	selectedDir, err := filepath.EvalSymlinks(selected.Dir())
	if err != nil {
		t.Fatalf("EvalSymlinks selected dir: %v", err)
	}
	if openedDir != selectedDir {
		t.Fatalf("opened session dir = %q, want %q", openedDir, selectedDir)
	}
	if plan.Store.Meta().WorkspaceRoot != "/tmp/workspace-a" {
		t.Fatalf("opened workspace root = %q, want /tmp/workspace-a", plan.Store.Meta().WorkspaceRoot)
	}
}

func TestPlannerSelectedSessionIDUsesActiveContainerScope(t *testing.T) {
	root := t.TempDir()
	containerA := filepath.Join(root, "projects", "project-a", "sessions")
	containerB := filepath.Join(root, "projects", "project-b", "sessions")
	selected, err := session.Create(containerA, "sessions", "/tmp/workspace-a")
	if err != nil {
		t.Fatalf("create selected session: %v", err)
	}
	if err := selected.SetName("selected"); err != nil {
		t.Fatalf("persist selected session meta: %v", err)
	}
	duplicateDir := filepath.Join(containerB, selected.Meta().SessionID)
	if err := os.MkdirAll(duplicateDir, 0o755); err != nil {
		t.Fatalf("mkdir duplicate session dir: %v", err)
	}
	duplicateMeta := selected.Meta()
	duplicateMeta.WorkspaceContainer = "sessions"
	duplicateMeta.WorkspaceRoot = "/tmp/workspace-b"
	duplicateData, err := json.Marshal(duplicateMeta)
	if err != nil {
		t.Fatalf("marshal duplicate session meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(duplicateDir, "session.json"), duplicateData, 0o644); err != nil {
		t.Fatalf("write duplicate session meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(duplicateDir, "events.jsonl"), nil, 0o644); err != nil {
		t.Fatalf("write duplicate session events: %v", err)
	}
	planner := Planner{
		Config:       config.App{WorkspaceRoot: "/tmp/workspace-a", PersistenceRoot: root, Settings: config.Settings{}},
		ContainerDir: containerA,
	}

	plan, err := planner.PlanSession(context.Background(), SessionRequest{Mode: ModeInteractive, SelectedSessionID: selected.Meta().SessionID})
	if err != nil {
		t.Fatalf("plan session: %v", err)
	}
	openedDir, err := filepath.EvalSymlinks(plan.Store.Dir())
	if err != nil {
		t.Fatalf("EvalSymlinks opened dir: %v", err)
	}
	selectedDir, err := filepath.EvalSymlinks(selected.Dir())
	if err != nil {
		t.Fatalf("EvalSymlinks selected dir: %v", err)
	}
	if openedDir != selectedDir {
		t.Fatalf("opened session dir = %q, want %q", openedDir, selectedDir)
	}
}

func TestPlannerSelectedSessionIDDoesNotFallbackOutsideActiveContainer(t *testing.T) {
	root := t.TempDir()
	projectContainer := filepath.Join(root, "projects", "project-123", "sessions")
	otherProjectContainer := filepath.Join(root, "projects", "project-456", "sessions")
	if err := os.MkdirAll(projectContainer, 0o755); err != nil {
		t.Fatalf("mkdir project container: %v", err)
	}
	otherProjectSession, err := session.Create(otherProjectContainer, "sessions", "/tmp/other-project-workspace")
	if err != nil {
		t.Fatalf("create other project session: %v", err)
	}
	if err := otherProjectSession.SetName("other project session"); err != nil {
		t.Fatalf("persist other project session meta: %v", err)
	}
	planner := Planner{
		Config:       config.App{WorkspaceRoot: "/tmp/project-workspace", PersistenceRoot: root, Settings: config.Settings{}},
		ContainerDir: projectContainer,
	}

	_, err = planner.PlanSession(context.Background(), SessionRequest{Mode: ModeInteractive, SelectedSessionID: otherProjectSession.Meta().SessionID})
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
	escaped, err := session.Create(containerB, "sessions", "/tmp/workspace-b")
	if err != nil {
		t.Fatalf("create escaped session: %v", err)
	}
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

type stubLaunchProjectViewService struct {
	overview      serverapi.ProjectGetOverviewResponse
	overviewCalls int
}

func (s *stubLaunchProjectViewService) ListProjects(_ context.Context, _ serverapi.ProjectListRequest) (serverapi.ProjectListResponse, error) {
	return serverapi.ProjectListResponse{}, nil
}

func (s *stubLaunchProjectViewService) ResolveProjectPath(_ context.Context, _ serverapi.ProjectResolvePathRequest) (serverapi.ProjectResolvePathResponse, error) {
	return serverapi.ProjectResolvePathResponse{}, errors.New("ResolveProjectPath should not be called in planner tests")
}

func (s *stubLaunchProjectViewService) CreateProject(_ context.Context, _ serverapi.ProjectCreateRequest) (serverapi.ProjectCreateResponse, error) {
	return serverapi.ProjectCreateResponse{}, errors.New("CreateProject should not be called in planner tests")
}

func (s *stubLaunchProjectViewService) AttachWorkspaceToProject(_ context.Context, _ serverapi.ProjectAttachWorkspaceRequest) (serverapi.ProjectAttachWorkspaceResponse, error) {
	return serverapi.ProjectAttachWorkspaceResponse{}, errors.New("AttachWorkspaceToProject should not be called in planner tests")
}

func (s *stubLaunchProjectViewService) RebindWorkspace(_ context.Context, _ serverapi.ProjectRebindWorkspaceRequest) (serverapi.ProjectRebindWorkspaceResponse, error) {
	return serverapi.ProjectRebindWorkspaceResponse{}, errors.New("RebindWorkspace should not be called in planner tests")
}

func (s *stubLaunchProjectViewService) GetProjectOverview(_ context.Context, _ serverapi.ProjectGetOverviewRequest) (serverapi.ProjectGetOverviewResponse, error) {
	s.overviewCalls++
	return s.overview, nil
}

func (s *stubLaunchProjectViewService) ListSessionsByProject(_ context.Context, _ serverapi.SessionListByProjectRequest) (serverapi.SessionListByProjectResponse, error) {
	return serverapi.SessionListByProjectResponse{}, errors.New("ListSessionsByProject should not be called when project overview is available")
}
