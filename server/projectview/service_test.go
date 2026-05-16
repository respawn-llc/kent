package projectview

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"builder/server/metadata"
	"builder/server/session"
	"builder/server/workflowstore"
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

func TestMetadataServiceCreatesProjectWithExplicitKey(t *testing.T) {
	store, _, _ := newProjectViewMetadataStore(t)
	workspace := t.TempDir()
	svc, err := NewMetadataService(store, "", "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}

	created, err := svc.CreateProject(context.Background(), serverapi.ProjectCreateRequest{
		DisplayName:   "GUI Project",
		ProjectKey:    "GUI1",
		WorkspaceRoot: workspace,
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if created.Binding.ProjectKey != "GUI1" {
		t.Fatalf("project key = %q, want GUI1", created.Binding.ProjectKey)
	}

	overview, err := svc.GetProjectOverview(context.Background(), serverapi.ProjectGetOverviewRequest{ProjectID: created.Binding.ProjectID})
	if err != nil {
		t.Fatalf("GetProjectOverview: %v", err)
	}
	if overview.Overview.Project.ProjectKey != "GUI1" {
		t.Fatalf("overview project key = %q, want GUI1", overview.Overview.Project.ProjectKey)
	}
}

func TestMetadataServiceRejectsInvalidAndDuplicateProjectKeys(t *testing.T) {
	store, _, _ := newProjectViewMetadataStore(t)
	svc, err := NewMetadataService(store, "", "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}

	if _, err := svc.CreateProject(context.Background(), serverapi.ProjectCreateRequest{
		DisplayName:   "Invalid",
		ProjectKey:    "bad-key",
		WorkspaceRoot: t.TempDir(),
	}); err == nil {
		t.Fatal("expected invalid project key error")
	}
	if _, err := svc.CreateProject(context.Background(), serverapi.ProjectCreateRequest{
		DisplayName:   "First",
		ProjectKey:    "DUP1",
		WorkspaceRoot: t.TempDir(),
	}); err != nil {
		t.Fatalf("CreateProject first: %v", err)
	}
	if _, err := svc.CreateProject(context.Background(), serverapi.ProjectCreateRequest{
		DisplayName:   "Second",
		ProjectKey:    "DUP1",
		WorkspaceRoot: t.TempDir(),
	}); err == nil {
		t.Fatal("expected duplicate project key error")
	}
	projects, err := svc.ListProjects(context.Background(), serverapi.ProjectListRequest{})
	if err != nil {
		t.Fatalf("ListProjects after duplicate key: %v", err)
	}
	if len(projects.Projects) != 2 {
		t.Fatalf("project count after duplicate key = %d, want 2: %+v", len(projects.Projects), projects.Projects)
	}
}

func TestMetadataServiceCreatesProjectWithoutExplicitKey(t *testing.T) {
	store, _, _ := newProjectViewMetadataStore(t)
	svc, err := NewMetadataService(store, "", "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}

	created, err := svc.CreateProject(context.Background(), serverapi.ProjectCreateRequest{
		DisplayName:   "Default Key",
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if created.Binding.ProjectKey == "" {
		t.Fatalf("expected generated project key, got %+v", created.Binding)
	}
}

func TestMetadataServiceListsProjectWorkspacesForGUI(t *testing.T) {
	store, _, binding := newProjectViewMetadataStore(t)
	attached, err := store.AttachWorkspaceToProject(context.Background(), binding.ProjectID, t.TempDir())
	if err != nil {
		t.Fatalf("AttachWorkspaceToProject: %v", err)
	}
	svc, err := NewMetadataService(store, "", "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}

	list, err := svc.ListProjectWorkspaces(context.Background(), serverapi.ProjectWorkspaceListRequest{ProjectID: binding.ProjectID})
	if err != nil {
		t.Fatalf("ListProjectWorkspaces: %v", err)
	}
	if list.ProjectID != binding.ProjectID {
		t.Fatalf("project id = %q, want %q", list.ProjectID, binding.ProjectID)
	}
	if list.DefaultWorkspaceID != binding.WorkspaceID {
		t.Fatalf("default workspace = %q, want %q", list.DefaultWorkspaceID, binding.WorkspaceID)
	}
	if len(list.Workspaces) != 2 {
		t.Fatalf("workspace count = %d, want 2: %+v", len(list.Workspaces), list.Workspaces)
	}
	if list.Workspaces[0].WorkspaceID != binding.WorkspaceID || !list.Workspaces[0].IsPrimary {
		t.Fatalf("first workspace = %+v, want primary %q", list.Workspaces[0], binding.WorkspaceID)
	}
	if list.Workspaces[1].WorkspaceID != attached.WorkspaceID {
		t.Fatalf("second workspace = %+v, want %q", list.Workspaces[1], attached.WorkspaceID)
	}
}

func TestMetadataServiceListsProjectHomeForGUI(t *testing.T) {
	store, _, binding := newProjectViewMetadataStore(t)
	svc, err := NewMetadataService(store, "", "")
	if err != nil {
		t.Fatalf("NewMetadataService: %v", err)
	}
	created, err := svc.CreateProject(context.Background(), serverapi.ProjectCreateRequest{
		DisplayName:   "GUI Home",
		ProjectKey:    "HOME",
		WorkspaceRoot: t.TempDir(),
	})
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	workflowStore, err := workflowstore.New(store)
	if err != nil {
		t.Fatalf("workflowstore.New: %v", err)
	}
	workflow, err := workflowStore.CreateWorkflow(context.Background(), workflowstore.CreateWorkflowRequest{Name: "Default Board"})
	if err != nil {
		t.Fatalf("CreateWorkflow: %v", err)
	}
	if _, err := workflowStore.LinkWorkflow(context.Background(), created.Binding.ProjectID, workflow.ID, true); err != nil {
		t.Fatalf("LinkWorkflow: %v", err)
	}

	firstPage, err := svc.ListProjectHome(context.Background(), serverapi.ProjectHomeListRequest{PageSize: 1})
	if err != nil {
		t.Fatalf("ListProjectHome first page: %v", err)
	}
	if len(firstPage.Projects) != 1 {
		t.Fatalf("first page count = %d, want 1: %+v", len(firstPage.Projects), firstPage.Projects)
	}
	if firstPage.NextPageToken == "" {
		t.Fatalf("expected next page token: %+v", firstPage)
	}
	first := firstPage.Projects[0]
	if first.ProjectID != created.Binding.ProjectID || first.ProjectKey != "HOME" {
		t.Fatalf("first project = %+v, want created HOME project", first)
	}
	if first.PrimaryWorkspace.WorkspaceID != created.Binding.WorkspaceID || !first.PrimaryWorkspace.IsPrimary {
		t.Fatalf("primary workspace = %+v, want %q", first.PrimaryWorkspace, created.Binding.WorkspaceID)
	}
	if first.DefaultWorkflowID != string(workflow.ID) || first.DefaultWorkflowName != "Default Board" || !first.DefaultWorkflowValid {
		t.Fatalf("default workflow = %+v, want linked workflow %s", first, workflow.ID)
	}
	if first.WorkflowCount != 1 {
		t.Fatalf("workflow count = %d, want 1", first.WorkflowCount)
	}
	if first.AttentionCount != 0 {
		t.Fatalf("attention count = %d, want 0", first.AttentionCount)
	}
	if firstPage.GeneratedAtUnixMs <= 0 {
		t.Fatalf("generated_at_unix_ms = %d, want positive", firstPage.GeneratedAtUnixMs)
	}
	if firstPage.LatestEventSequence != 0 {
		t.Fatalf("latest_event_sequence = %d, want foundation watermark 0", firstPage.LatestEventSequence)
	}

	secondPage, err := svc.ListProjectHome(context.Background(), serverapi.ProjectHomeListRequest{PageSize: 1, PageToken: firstPage.NextPageToken})
	if err != nil {
		t.Fatalf("ListProjectHome second page: %v", err)
	}
	if len(secondPage.Projects) != 1 {
		t.Fatalf("second page count = %d, want 1: %+v", len(secondPage.Projects), secondPage.Projects)
	}
	second := secondPage.Projects[0]
	if second.ProjectID != binding.ProjectID {
		t.Fatalf("second project = %+v, want initial project %s", second, binding.ProjectID)
	}
	if second.DefaultWorkflowValid || second.DefaultWorkflowID != "" || second.DefaultWorkflowName != "" {
		t.Fatalf("empty default workflow = %+v, want invalid empty default workflow", second)
	}
	if _, err := svc.ListProjectHome(context.Background(), serverapi.ProjectHomeListRequest{PageToken: "bad"}); err == nil {
		t.Fatal("expected invalid page token error")
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
