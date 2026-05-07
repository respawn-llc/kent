package projectview

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"builder/server/metadata"
	"builder/server/session"
	"builder/shared/clientui"
	"builder/shared/config"
	"builder/shared/serverapi"
)

func TestServiceListsSingleProjectAndSessions(t *testing.T) {
	store, cfg, binding := newProjectViewMetadataStore(t)
	containerDir := config.ProjectSessionsRoot(cfg, binding.ProjectID)
	first, err := session.Create(containerDir, filepath.Base(containerDir), cfg.WorkspaceRoot, store.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("create first session: %v", err)
	}
	if err := first.SetName("first"); err != nil {
		t.Fatalf("persist first session meta: %v", err)
	}
	second, err := session.Create(containerDir, filepath.Base(containerDir), cfg.WorkspaceRoot, store.AuthoritativeSessionStoreOptions()...)
	if err != nil {
		t.Fatalf("create second session: %v", err)
	}
	if err := second.SetName("second"); err != nil {
		t.Fatalf("persist second session meta: %v", err)
	}

	svc, err := NewMetadataService(store, binding.ProjectID, "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}

	projects, err := svc.ListProjects(context.Background(), serverapi.ProjectListRequest{})
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects.Projects) != 1 {
		t.Fatalf("expected one project, got %+v", projects)
	}
	if projects.Projects[0].ProjectID != binding.ProjectID {
		t.Fatalf("unexpected project summary: %+v", projects.Projects[0])
	}
	if projects.Projects[0].Availability != clientui.ProjectAvailabilityAvailable {
		t.Fatalf("expected available workspace availability, got %+v", projects.Projects[0])
	}

	sessions, err := svc.ListSessionsByProject(context.Background(), serverapi.SessionListByProjectRequest{ProjectID: binding.ProjectID})
	if err != nil {
		t.Fatalf("ListSessionsByProject: %v", err)
	}
	if len(sessions.Sessions) != 2 {
		t.Fatalf("expected two sessions, got %+v", sessions)
	}
	if sessions.Sessions[0].SessionID != second.Meta().SessionID {
		t.Fatalf("expected most recent session first, got %+v", sessions.Sessions)
	}

	overview, err := svc.GetProjectOverview(context.Background(), serverapi.ProjectGetOverviewRequest{ProjectID: binding.ProjectID})
	if err != nil {
		t.Fatalf("GetProjectOverview: %v", err)
	}
	if overview.Overview.Project.SessionCount != 2 {
		t.Fatalf("unexpected overview session count: %+v", overview.Overview)
	}
	if len(overview.Overview.Sessions) != 2 {
		t.Fatalf("unexpected overview sessions: %+v", overview.Overview)
	}
}

func TestServiceRejectsUnknownProjectID(t *testing.T) {
	store, _, binding := newProjectViewMetadataStore(t)
	svc, err := NewMetadataService(store, binding.ProjectID, "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}
	if _, err := svc.GetProjectOverview(context.Background(), serverapi.ProjectGetOverviewRequest{ProjectID: "project-2"}); err == nil {
		t.Fatal("expected GetProjectOverview to reject unknown project")
	}
	if _, err := svc.ListSessionsByProject(context.Background(), serverapi.SessionListByProjectRequest{ProjectID: "project-2"}); err == nil {
		t.Fatal("expected ListSessionsByProject to reject unknown project")
	}
}

func TestMetadataServiceSupportsWildcardAndScopedProjectListing(t *testing.T) {
	home := t.TempDir()
	workspaceA := t.TempDir()
	workspaceB := t.TempDir()
	t.Setenv("HOME", home)

	cfgA, err := config.Load(workspaceA, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load workspace A: %v", err)
	}
	store, err := metadata.Open(cfgA.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	bindingA, err := store.RegisterWorkspaceBinding(context.Background(), cfgA.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding A: %v", err)
	}

	cfgB, err := config.Load(workspaceB, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load workspace B: %v", err)
	}
	bindingB, err := store.RegisterWorkspaceBinding(context.Background(), cfgB.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding B: %v", err)
	}

	wildcard, err := NewMetadataService(store, "", "")
	if err != nil {
		t.Fatalf("NewMetadataService wildcard: %v", err)
	}
	projects, err := wildcard.ListProjects(context.Background(), serverapi.ProjectListRequest{})
	if err != nil {
		t.Fatalf("ListProjects wildcard: %v", err)
	}
	if len(projects.Projects) != 2 {
		t.Fatalf("expected wildcard metadata service to list both projects, got %+v", projects.Projects)
	}

	scoped, err := NewMetadataService(store, bindingA.ProjectID, "")
	if err != nil {
		t.Fatalf("NewMetadataService scoped: %v", err)
	}
	projects, err = scoped.ListProjects(context.Background(), serverapi.ProjectListRequest{})
	if err != nil {
		t.Fatalf("ListProjects scoped: %v", err)
	}
	if len(projects.Projects) != 1 || projects.Projects[0].ProjectID != bindingA.ProjectID {
		t.Fatalf("expected scoped metadata service to list only project A, got %+v", projects.Projects)
	}
	if _, err := scoped.GetProjectOverview(context.Background(), serverapi.ProjectGetOverviewRequest{ProjectID: bindingB.ProjectID}); err == nil {
		t.Fatal("expected scoped metadata service to reject other project overview")
	}
}

func TestMetadataServiceResolveProjectPathLeavesNestedDirectoryUnbound(t *testing.T) {
	home := t.TempDir()
	workspace := t.TempDir()
	nested := filepath.Join(workspace, "nested", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatalf("MkdirAll nested: %v", err)
	}
	t.Setenv("HOME", home)

	cfg, err := config.Load(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	_, err = store.RegisterWorkspaceBinding(context.Background(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}

	svc, err := NewMetadataService(store, "", "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}

	resolved, err := svc.ResolveProjectPath(context.Background(), serverapi.ProjectResolvePathRequest{Path: nested})
	if err != nil {
		t.Fatalf("ResolveProjectPath: %v", err)
	}
	if resolved.Binding != nil {
		t.Fatalf("expected nested path to remain unbound, got %+v", resolved.Binding)
	}
}

func TestMetadataServicePlansInteractiveLocalUnboundWorkspace(t *testing.T) {
	store, _, binding := newProjectViewMetadataStore(t)
	workspace := t.TempDir()
	svc, err := NewMetadataService(store, "", "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}

	plan, err := svc.PlanWorkspaceBinding(context.Background(), serverapi.ProjectBindingPlanRequest{Path: workspace, Mode: serverapi.ProjectBindingPlanModeInteractive})
	if err != nil {
		t.Fatalf("PlanWorkspaceBinding: %v", err)
	}
	if plan.Kind != serverapi.ProjectBindingPlanKindLocalUnbound {
		t.Fatalf("plan kind = %q, want %q", plan.Kind, serverapi.ProjectBindingPlanKindLocalUnbound)
	}
	if len(plan.Projects) != 1 || plan.Projects[0].ProjectID != binding.ProjectID {
		t.Fatalf("plan projects = %+v, want registered project %q", plan.Projects, binding.ProjectID)
	}
}

func TestMetadataServicePlansHeadlessSingleRemoteWorkspace(t *testing.T) {
	store, _, binding := newProjectViewMetadataStore(t)
	svc, err := NewMetadataService(store, "", "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}

	plan, err := svc.PlanWorkspaceBinding(context.Background(), serverapi.ProjectBindingPlanRequest{Path: filepath.Join(t.TempDir(), "missing"), Mode: serverapi.ProjectBindingPlanModeHeadless})
	if err != nil {
		t.Fatalf("PlanWorkspaceBinding: %v", err)
	}
	if plan.Kind != serverapi.ProjectBindingPlanKindHeadlessRemoteSelected || plan.Workspace == nil {
		t.Fatalf("plan = %+v, want selected remote workspace", plan)
	}
	if plan.Workspace.ProjectID != binding.ProjectID || plan.Workspace.WorkspaceID != binding.WorkspaceID {
		t.Fatalf("selected workspace = %+v, want %s/%s", plan.Workspace, binding.ProjectID, binding.WorkspaceID)
	}
}

func TestMetadataServicePlansHeadlessAmbiguousRemoteWorkspaces(t *testing.T) {
	store, _, binding := newProjectViewMetadataStore(t)
	if _, err := store.AttachWorkspaceToProject(context.Background(), binding.ProjectID, t.TempDir()); err != nil {
		t.Fatalf("AttachWorkspaceToProject: %v", err)
	}
	svc, err := NewMetadataService(store, "", "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}

	plan, err := svc.PlanWorkspaceBinding(context.Background(), serverapi.ProjectBindingPlanRequest{Path: filepath.Join(t.TempDir(), "missing"), Mode: serverapi.ProjectBindingPlanModeHeadless})
	if err != nil {
		t.Fatalf("PlanWorkspaceBinding: %v", err)
	}
	if plan.Kind != serverapi.ProjectBindingPlanKindHeadlessRemoteAmbiguous {
		t.Fatalf("plan kind = %q, want %q", plan.Kind, serverapi.ProjectBindingPlanKindHeadlessRemoteAmbiguous)
	}
}

func newProjectViewMetadataStore(t *testing.T) (*metadata.Store, config.App, metadata.Binding) {
	t.Helper()
	home := t.TempDir()
	workspace := t.TempDir()
	t.Setenv("HOME", home)

	cfg, err := config.Load(workspace, config.LoadOptions{})
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	store, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		t.Fatalf("metadata.Open: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	binding, err := store.RegisterWorkspaceBinding(context.Background(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	return store, cfg, binding
}
