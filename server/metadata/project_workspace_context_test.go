package metadata

import (
	"path/filepath"
	"testing"

	"core/server/session"
	"core/shared/sessioncontract"
)

func TestReadSessionProjectWorkspaceContextUsesOwningProjectAndBoundedCatalog(t *testing.T) {
	store, cfg, source := newMetadataTestStore(t)
	target, err := store.CreateProjectForWorkspace(t.Context(), t.TempDir(), "Target Project")
	if err != nil {
		t.Fatalf("create target Project: %v", err)
	}
	for range 5 {
		if _, err := store.AttachWorkspaceToProject(t.Context(), target.ProjectID, t.TempDir()); err != nil {
			t.Fatalf("attach Workspace: %v", err)
		}
	}

	container := filepath.Join(cfg.PersistenceRoot, "projects", target.ProjectID, "sessions")
	targetSession, err := session.Create(
		container,
		filepath.Base(container),
		target.CanonicalRoot,
		sessioncontract.SessionCategoryMain,
		store.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("create target Session: %v", err)
	}
	worktreeRoot := filepath.Join(t.TempDir(), "managed-worktree")
	createMetadataTestWorktree(t, t.Context(), store, target.WorkspaceID, "target-worktree", worktreeRoot)
	if err := store.UpdateSessionExecutionTarget(t.Context(), SessionExecutionTargetUpdate{
		SessionID:  targetSession.Meta().SessionID,
		Workspace:  &SessionExecutionTargetUpdateWorkspace{ID: target.WorkspaceID},
		Worktree:   &SessionExecutionTargetUpdateWorktree{ID: "target-worktree"},
		CwdRelpath: "nested",
	}); err != nil {
		t.Fatalf("move Session into managed Worktree: %v", err)
	}
	t.Chdir(source.CanonicalRoot)

	got, err := store.ReadSessionProjectWorkspaceContext(t.Context(), targetSession.Meta().SessionID)
	if err != nil {
		t.Fatalf("read Session Project Workspace context: %v", err)
	}
	if got.ProjectName != target.ProjectName {
		t.Fatalf("Project name = %q, want %q", got.ProjectName, target.ProjectName)
	}
	if got.WorkspaceCount != 6 {
		t.Fatalf("Workspace count = %d, want 6", got.WorkspaceCount)
	}
	if len(got.Workspaces) != 5 {
		t.Fatalf("context Workspaces = %d, want the bounded first five", len(got.Workspaces))
	}
	catalog, err := store.ListProjectWorkspaceCatalogPage(t.Context(), target.ProjectID, 0, 5)
	if err != nil {
		t.Fatalf("list Project Workspace catalog: %v", err)
	}
	for index, want := range catalog.Workspaces {
		if got.Workspaces[index] != want {
			t.Fatalf("Workspace %d = %+v, want catalog row %+v", index, got.Workspaces[index], want)
		}
	}
	if !got.Workspaces[0].IsDefault {
		t.Fatal("first Workspace is not the Project default")
	}
	for _, workspace := range got.Workspaces {
		if workspace.CanonicalRoot == worktreeRoot {
			t.Fatalf("managed Worktree appeared as an attached Workspace: %+v", workspace)
		}
	}
	if got.ProjectName == "" || source.ProjectID == target.ProjectID {
		t.Fatal("test did not distinguish the Session Project from the current Working Directory Project")
	}
}
