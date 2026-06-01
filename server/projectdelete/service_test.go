package projectdelete

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"builder/server/metadata"
	"builder/server/projectgate"
	"builder/shared/serverapi"
)

func TestServiceDeleteProjectRemovesMetadataAndSessionArtifactsOnly(t *testing.T) {
	store, binding := newProjectDeleteTestStore(t)
	service := newProjectDeleteTestService(t, store)
	workspaceMarker := filepath.Join(binding.CanonicalRoot, "keep.txt")
	if err := os.WriteFile(workspaceMarker, []byte("user file"), 0o644); err != nil {
		t.Fatal(err)
	}
	sessionID := "session-delete-service"
	sessionDir := insertProjectDeleteServiceSession(t, store, binding.ProjectID, binding.WorkspaceID, sessionID, 0)
	if err := os.WriteFile(filepath.Join(sessionDir, "events.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	preview, err := service.PreviewProjectDelete(context.Background(), projectDeletePreviewRequest(binding.ProjectID))
	if err != nil {
		t.Fatalf("PreviewProjectDelete: %v", err)
	}
	if len(preview.Impact.Blockers) != 0 || preview.Impact.SessionCount != 1 || preview.Impact.SessionArtifactCount != 1 {
		t.Fatalf("preview impact = %+v, want no blockers and one session artifact", preview.Impact)
	}
	deleted, err := service.DeleteProject(context.Background(), projectDeleteRequestFromImpact(preview.Impact))
	if err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if !deleted.Deleted || len(deleted.Blockers) != 0 {
		t.Fatalf("delete response = %+v, want deleted", deleted)
	}
	assertProjectDeleteRowCount(t, store.DB(), "projects", "id = ?", 0, binding.ProjectID)
	assertProjectDeleteRowCount(t, store.DB(), "sessions", "id = ?", 0, sessionID)
	assertProjectDeleteRowCount(t, store.DB(), "project_delete_jobs", "project_id = ?", 0, binding.ProjectID)
	assertProjectDeleteAbsent(t, sessionDir)
	assertProjectDeleteExists(t, workspaceMarker)
}

func TestServiceDeleteProjectBlocksActiveSession(t *testing.T) {
	store, binding := newProjectDeleteTestStore(t)
	service := newProjectDeleteTestService(t, store)
	insertProjectDeleteServiceSession(t, store, binding.ProjectID, binding.WorkspaceID, "session-active-delete", 1)

	preview, err := service.PreviewProjectDelete(context.Background(), projectDeletePreviewRequest(binding.ProjectID))
	if err != nil {
		t.Fatalf("PreviewProjectDelete: %v", err)
	}
	if !hasProjectDeleteBlocker(preview.Impact.Blockers, "active_sessions") {
		t.Fatalf("blockers = %+v, want active_sessions", preview.Impact.Blockers)
	}
	deleted, err := service.DeleteProject(context.Background(), projectDeleteRequestFromImpact(preview.Impact))
	if err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if deleted.Deleted || !hasProjectDeleteBlocker(deleted.Blockers, "active_sessions") {
		t.Fatalf("delete response = %+v, want active_sessions blocker", deleted)
	}
	assertProjectDeleteRowCount(t, store.DB(), "projects", "id = ?", 1, binding.ProjectID)
	assertProjectDeleteRowCount(t, store.DB(), "project_delete_jobs", "project_id = ?", 0, binding.ProjectID)
}

func TestServiceDeleteProjectResumeDoesNotBypassFreshDeleteBlockers(t *testing.T) {
	store, binding := newProjectDeleteTestStore(t)
	service := newProjectDeleteTestService(t, store, WithAttachedProjectID(binding.ProjectID))

	preview, err := service.PreviewProjectDelete(context.Background(), projectDeletePreviewRequest(binding.ProjectID))
	if err != nil {
		t.Fatalf("PreviewProjectDelete: %v", err)
	}
	if !hasProjectDeleteBlocker(preview.Impact.Blockers, "active_attached_project") {
		t.Fatalf("blockers = %+v, want active_attached_project", preview.Impact.Blockers)
	}
	req := projectDeleteRequestFromImpact(preview.Impact)
	req.Resume = true
	deleted, err := service.DeleteProject(context.Background(), req)
	if err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if deleted.Deleted || !hasProjectDeleteBlocker(deleted.Blockers, "active_attached_project") {
		t.Fatalf("delete response = %+v, want active_attached_project blocker", deleted)
	}
	assertProjectDeleteRowCount(t, store.DB(), "projects", "id = ?", 1, binding.ProjectID)
	assertProjectDeleteRowCount(t, store.DB(), "project_delete_jobs", "project_id = ?", 0, binding.ProjectID)
}

func TestServiceDeleteProjectCleanupFailureLeavesJobResumable(t *testing.T) {
	store, binding := newProjectDeleteTestStore(t)
	service := newProjectDeleteTestService(t, store)
	sessionDir := insertProjectDeleteServiceSession(t, store, binding.ProjectID, binding.WorkspaceID, "session-symlink-delete", 0)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(sessionDir, "unsafe-link")); err != nil {
		t.Fatal(err)
	}

	preview, err := service.PreviewProjectDelete(context.Background(), projectDeletePreviewRequest(binding.ProjectID))
	if err != nil {
		t.Fatalf("PreviewProjectDelete: %v", err)
	}
	deleted, err := service.DeleteProject(context.Background(), projectDeleteRequestFromImpact(preview.Impact))
	if err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}
	if deleted.Deleted || !hasProjectDeleteBlocker(deleted.Blockers, "artifact_cleanup_failed") {
		t.Fatalf("delete response = %+v, want cleanup blocker", deleted)
	}
	assertProjectDeleteRowCount(t, store.DB(), "projects", "id = ?", 1, binding.ProjectID)
	assertProjectDeleteRowCount(t, store.DB(), "project_delete_jobs", "project_id = ?", 1, binding.ProjectID)
	assertProjectDeleteRowCount(t, store.DB(), "project_delete_session_artifacts", "project_id = ? AND state = 'failed'", 1, binding.ProjectID)
	assertProjectDeleteExists(t, sessionDir)
	assertProjectDeleteExists(t, outside)
}

func newProjectDeleteTestStore(t *testing.T) (*metadata.Store, metadata.Binding) {
	t.Helper()
	root := t.TempDir()
	workspace := t.TempDir()
	store, err := metadata.Open(root)
	if err != nil {
		t.Fatalf("open metadata store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	binding, err := store.CreateProjectForWorkspaceWithKey(context.Background(), workspace, "Delete Project", "DEL")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	return store, binding
}

func newProjectDeleteTestService(t *testing.T, store *metadata.Store, opts ...Option) *Service {
	t.Helper()
	service, err := New(store, projectgate.New(), opts...)
	if err != nil {
		t.Fatalf("new project delete service: %v", err)
	}
	return service
}

func insertProjectDeleteServiceSession(t *testing.T, store *metadata.Store, projectID string, workspaceID string, sessionID string, inFlight int64) string {
	t.Helper()
	now := int64(1234)
	artifactRelpath := filepath.ToSlash(filepath.Join("projects", projectID, "sessions", sessionID))
	sessionDir := filepath.Join(store.PersistenceRoot(), filepath.FromSlash(artifactRelpath))
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(context.Background(), `INSERT INTO sessions (
    id,
    project_id,
    workspace_id,
    worktree_id,
    artifact_relpath,
    name,
    first_prompt_preview,
    input_draft,
    parent_session_id,
    created_at_unix_ms,
    updated_at_unix_ms,
    last_sequence,
    model_request_count,
    in_flight_step,
    agents_injected,
    launch_visible,
    cwd_relpath,
    continuation_json,
    locked_json,
    usage_state_json,
    metadata_json
) VALUES (?, ?, ?, NULL, ?, 'Session', '', '', '', ?, ?, 0, 0, ?, 0, 1, '.', '{}', '{}', '{}', '{}')`,
		sessionID, projectID, workspaceID, artifactRelpath, now, now, inFlight); err != nil {
		t.Fatalf("insert session: %v", err)
	}
	return sessionDir
}

func projectDeletePreviewRequest(projectID string) serverapi.ProjectDeletePreviewRequest {
	return serverapi.ProjectDeletePreviewRequest{ProjectID: projectID}
}

func projectDeleteRequestFromImpact(impact serverapi.ProjectDeleteImpact) serverapi.ProjectDeleteRequest {
	return serverapi.ProjectDeleteRequest{
		ProjectID:                    impact.ProjectID,
		ImpactToken:                  impact.ImpactToken,
		ExpectedWorkspaceCount:       impact.WorkspaceCount,
		ExpectedWorkflowLinkCount:    impact.WorkflowLinkCount,
		ExpectedTaskCount:            impact.TaskCount,
		ExpectedTerminalTaskCount:    impact.TerminalTaskCount,
		ExpectedNonTerminalTaskCount: impact.NonTerminalTaskCount,
		ExpectedSessionCount:         impact.SessionCount,
		ExpectedSessionArtifactCount: impact.SessionArtifactCount,
	}
}

func hasProjectDeleteBlocker(blockers []serverapi.ProjectDeleteBlocker, code string) bool {
	for _, blocker := range blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}

func assertProjectDeleteRowCount(t *testing.T, db *sql.DB, table string, where string, want int, args ...any) {
	t.Helper()
	var count int
	if err := db.QueryRowContext(context.Background(), `SELECT COUNT(*) FROM `+table+` WHERE `+where, args...).Scan(&count); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	if count != want {
		t.Fatalf("%s rows matching %q = %d, want %d", table, where, count, want)
	}
}

func assertProjectDeleteExists(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); err != nil {
		t.Fatalf("expected %s to exist: %v", path, err)
	}
}

func assertProjectDeleteAbsent(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected %s to be absent, got %v", path, err)
	}
}
