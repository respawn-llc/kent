package metadata

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"core/server/session"
	"core/shared/config"
	"core/shared/serverapi"
	"core/shared/sessioncontract"
)

type sessionRetargetFixture struct {
	store         *Store
	config        config.App
	source        Binding
	targetProject Binding
	session       *session.Store
}

func newSessionRetargetFixture(t *testing.T) sessionRetargetFixture {
	t.Helper()
	sourceRoot := t.TempDir()
	cfg := loadMetadataTestConfig(t, sourceRoot, filepath.Join(t.TempDir(), "persistence"))
	store := openInMemoryMetadataTestStore(t, cfg.PersistenceRoot)
	source, err := store.RegisterWorkspaceBinding(context.Background(), sourceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding source: %v", err)
	}
	targetProject, err := store.CreateProjectForWorkspace(context.Background(), t.TempDir(), "Target")
	if err != nil {
		t.Fatalf("CreateProjectForWorkspace target: %v", err)
	}
	sessionStore, err := session.Create(
		filepath.Join(cfg.PersistenceRoot, "projects", source.ProjectID, "sessions"),
		source.WorkspaceName,
		source.CanonicalRoot,
		sessioncontract.SessionCategoryMain,
		store.AuthoritativeSessionStoreOptions()...,
	)
	if err != nil {
		t.Fatalf("session.Create: %v", err)
	}
	return sessionRetargetFixture{
		store:         store,
		config:        cfg,
		source:        source,
		targetProject: targetProject,
		session:       sessionStore,
	}
}

func TestPlanSessionWorkspaceRetargetSelectsSoleForeignBindingByDefault(t *testing.T) {
	t.Parallel()
	fixture := newSessionRetargetFixture(t)
	targetRoot := t.TempDir()
	targetBinding, err := fixture.store.AttachWorkspaceToProject(context.Background(), fixture.targetProject.ProjectID, targetRoot)
	if err != nil {
		t.Fatalf("AttachWorkspaceToProject target: %v", err)
	}

	plan, err := fixture.store.PlanSessionWorkspaceRetarget(context.Background(), SessionWorkspaceRetargetRequest{
		SessionID:     fixture.session.Meta().SessionID,
		WorkspaceRoot: targetRoot,
	})
	if err != nil {
		t.Fatalf("PlanSessionWorkspaceRetarget: %v", err)
	}
	if plan.TargetProject.ID != fixture.targetProject.ProjectID {
		t.Fatalf("planned target project = %q, want %q", plan.TargetProject.ID, fixture.targetProject.ProjectID)
	}
	result, err := fixture.store.CommitSessionWorkspaceRetarget(context.Background(), plan, time.Now().UTC())
	if err != nil {
		t.Fatalf("CommitSessionWorkspaceRetarget: %v", err)
	}
	if result.WorkspaceBindingCreated {
		t.Fatal("WorkspaceBindingCreated = true, want reuse of existing foreign binding")
	}
	if result.Binding.WorkspaceID != targetBinding.WorkspaceID || result.Binding.ProjectID != fixture.targetProject.ProjectID {
		t.Fatalf("result binding = %+v, want existing target binding %+v", result.Binding, targetBinding)
	}
	target, err := fixture.store.ResolveSessionExecutionTarget(context.Background(), fixture.session.Meta().SessionID)
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget: %v", err)
	}
	if target.GetWorkspaceId() != targetBinding.WorkspaceID {
		t.Fatalf("session workspace = %q, want target %q", target.GetWorkspaceId(), targetBinding.WorkspaceID)
	}
	belongs, err := fixture.store.SessionBelongsToProject(context.Background(), fixture.session.Meta().SessionID, fixture.targetProject.ProjectID)
	if err != nil {
		t.Fatalf("SessionBelongsToProject target: %v", err)
	}
	if !belongs {
		t.Fatal("session did not move to the sole foreign Project")
	}
	sourceBelongs, err := fixture.store.SessionBelongsToProject(context.Background(), fixture.session.Meta().SessionID, fixture.source.ProjectID)
	if err != nil {
		t.Fatalf("SessionBelongsToProject source: %v", err)
	}
	if sourceBelongs {
		t.Fatal("session retained source Project ownership after cross-Project rebind")
	}
}

func TestWorkflowSessionCannotMoveAcrossProjects(t *testing.T) {
	f := newSessionRetargetFixture(t)
	if err := f.session.EnsureDurable(); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UnixMilli()
	seedWorkflowGraph(t, f.store.db, f.source.ProjectID, now)
	execSeed(t, f.store.db, "Workflow Task", `INSERT INTO tasks
		(id, project_workflow_link_id, workflow_revision_seen, task_seq, short_id, title, body, created_at_unix_ms, updated_at_unix_ms, metadata_json)
		VALUES ('task-move', 'link-1', 1, 1, 'BLD-1', 'Move', '', ?, ?, '{}')`, now, now)
	execSeed(t, f.store.db, "Workflow Session", "UPDATE sessions SET task_id = ? WHERE id = ?", "task-move", f.session.Meta().SessionID)
	_, err := f.store.PlanSessionWorkspaceRetarget(t.Context(), SessionWorkspaceRetargetRequest{
		SessionID: f.session.Meta().SessionID, WorkspaceRoot: f.targetProject.CanonicalRoot, ProjectID: &f.targetProject.ProjectID,
	})
	var rejected *serverapi.SessionRetargetError
	if !errors.As(err, &rejected) || rejected.Reason != serverapi.SessionRetargetWorkflowOwned {
		t.Fatalf("Workflow move error = %v", err)
	}
	if projectID, err := f.store.ResolveSessionProjectID(t.Context(), f.session.Meta().SessionID); err != nil || projectID != f.source.ProjectID {
		t.Fatalf("Workflow Session moved: %q, %v", projectID, err)
	}
}

func TestCommitSessionWorkspaceRetargetUsesSourceBindingWhenPathIsShared(t *testing.T) {
	t.Parallel()
	fixture := newSessionRetargetFixture(t)
	targetRoot := t.TempDir()
	sourceBinding, err := fixture.store.AttachWorkspaceToProject(context.Background(), fixture.source.ProjectID, targetRoot)
	if err != nil {
		t.Fatalf("AttachWorkspaceToProject source: %v", err)
	}
	if _, err := fixture.store.AttachWorkspaceToProject(context.Background(), fixture.targetProject.ProjectID, targetRoot); err != nil {
		t.Fatalf("AttachWorkspaceToProject target: %v", err)
	}

	plan, err := fixture.store.PlanSessionWorkspaceRetarget(context.Background(), SessionWorkspaceRetargetRequest{
		SessionID:     fixture.session.Meta().SessionID,
		WorkspaceRoot: targetRoot,
	})
	if err != nil {
		t.Fatalf("PlanSessionWorkspaceRetarget: %v", err)
	}
	result, err := fixture.store.CommitSessionWorkspaceRetarget(context.Background(), plan, time.Now().UTC())
	if err != nil {
		t.Fatalf("CommitSessionWorkspaceRetarget: %v", err)
	}
	if result.Binding.ProjectID != fixture.source.ProjectID || result.Binding.WorkspaceID != sourceBinding.WorkspaceID {
		t.Fatalf("binding = %+v, want source-project binding %+v", result.Binding, sourceBinding)
	}
}

func TestPlanSessionWorkspaceRetargetRejectsAmbiguousForeignBindingsWithoutMutation(t *testing.T) {
	t.Parallel()
	fixture := newSessionRetargetFixture(t)
	targetRoot := t.TempDir()
	if _, err := fixture.store.AttachWorkspaceToProject(context.Background(), fixture.targetProject.ProjectID, targetRoot); err != nil {
		t.Fatalf("AttachWorkspaceToProject target: %v", err)
	}
	otherProject, err := fixture.store.CreateProjectForWorkspace(context.Background(), t.TempDir(), "Other")
	if err != nil {
		t.Fatalf("CreateProjectForWorkspace other: %v", err)
	}
	if _, err := fixture.store.AttachWorkspaceToProject(context.Background(), otherProject.ProjectID, targetRoot); err != nil {
		t.Fatalf("AttachWorkspaceToProject other: %v", err)
	}

	_, err = fixture.store.PlanSessionWorkspaceRetarget(context.Background(), SessionWorkspaceRetargetRequest{
		SessionID:     fixture.session.Meta().SessionID,
		WorkspaceRoot: targetRoot,
	})
	var retargetErr *serverapi.SessionRetargetError
	if !errors.As(err, &retargetErr) || retargetErr.Reason != serverapi.SessionRetargetTargetProjectRequired {
		t.Fatalf("PlanSessionWorkspaceRetarget error = %v, want target-project-required", err)
	}
	if len(retargetErr.CandidateProjects) != 2 {
		t.Fatalf("candidate Projects = %d, want 2", len(retargetErr.CandidateProjects))
	}
	for _, candidate := range retargetErr.CandidateProjects {
		if candidate.ID != fixture.targetProject.ProjectID && candidate.ID != otherProject.ProjectID {
			t.Fatalf("unexpected candidate Project %q", candidate.ID)
		}
	}
	target, err := fixture.store.ResolveSessionExecutionTarget(context.Background(), fixture.session.Meta().SessionID)
	if err != nil {
		t.Fatalf("ResolveSessionExecutionTarget: %v", err)
	}
	if target.GetWorkspaceId() != fixture.source.WorkspaceID {
		t.Fatalf("session workspace = %q, want unchanged %q", target.GetWorkspaceId(), fixture.source.WorkspaceID)
	}
	belongs, err := fixture.store.SessionBelongsToProject(context.Background(), fixture.session.Meta().SessionID, fixture.source.ProjectID)
	if err != nil {
		t.Fatalf("SessionBelongsToProject source: %v", err)
	}
	if !belongs {
		t.Fatal("ambiguous plan changed source Project ownership")
	}
}

func TestCommitSessionWorkspaceRetargetMovesProjectAndAutoAttachesWorkspace(t *testing.T) {
	t.Parallel()
	fixture := newSessionRetargetFixture(t)
	targetRoot := t.TempDir()
	targetProjectID := fixture.targetProject.ProjectID

	plan, err := fixture.store.PlanSessionWorkspaceRetarget(context.Background(), SessionWorkspaceRetargetRequest{
		SessionID:     fixture.session.Meta().SessionID,
		WorkspaceRoot: targetRoot,
		ProjectID:     &targetProjectID,
	})
	if err != nil {
		t.Fatalf("PlanSessionWorkspaceRetarget: %v", err)
	}
	result, err := fixture.store.CommitSessionWorkspaceRetarget(context.Background(), plan, time.Now().UTC())
	if err != nil {
		t.Fatalf("CommitSessionWorkspaceRetarget: %v", err)
	}
	if !result.WorkspaceBindingCreated {
		t.Fatal("WorkspaceBindingCreated = false, want true")
	}
	if result.Binding.ProjectID != targetProjectID {
		t.Fatalf("target project = %q, want %q", result.Binding.ProjectID, targetProjectID)
	}
	belongs, err := fixture.store.SessionBelongsToProject(context.Background(), fixture.session.Meta().SessionID, targetProjectID)
	if err != nil {
		t.Fatalf("SessionBelongsToProject: %v", err)
	}
	if !belongs {
		t.Fatal("session did not move to target project")
	}
}

func TestPlanSessionWorkspaceRetargetRejectsExplicitForeignBindingWithoutMutation(t *testing.T) {
	t.Parallel()
	fixture := newSessionRetargetFixture(t)
	targetRoot := t.TempDir()
	if _, err := fixture.store.AttachWorkspaceToProject(context.Background(), fixture.targetProject.ProjectID, targetRoot); err != nil {
		t.Fatalf("AttachWorkspaceToProject foreign target: %v", err)
	}
	requestedProject, err := fixture.store.CreateProjectForWorkspace(context.Background(), t.TempDir(), "Requested")
	if err != nil {
		t.Fatalf("CreateProjectForWorkspace requested: %v", err)
	}
	before, err := fixture.store.Queries().CountProjectWorkspaces(context.Background(), requestedProject.ProjectID)
	if err != nil {
		t.Fatalf("ListProjectWorkspaces before: %v", err)
	}
	requestedProjectID := requestedProject.ProjectID

	_, err = fixture.store.PlanSessionWorkspaceRetarget(context.Background(), SessionWorkspaceRetargetRequest{
		SessionID:     fixture.session.Meta().SessionID,
		WorkspaceRoot: targetRoot,
		ProjectID:     &requestedProjectID,
	})
	var retargetErr *serverapi.SessionRetargetError
	if !errors.As(err, &retargetErr) || retargetErr.Reason != serverapi.SessionRetargetTargetProjectConflict {
		t.Fatalf("PlanSessionWorkspaceRetarget error = %v, want target-project-conflict", err)
	}
	after, err := fixture.store.Queries().CountProjectWorkspaces(context.Background(), requestedProject.ProjectID)
	if err != nil {
		t.Fatalf("ListProjectWorkspaces after: %v", err)
	}
	if after != before {
		t.Fatalf("requested project workspace count = %d, want unchanged %d", after, before)
	}
	belongs, err := fixture.store.SessionBelongsToProject(context.Background(), fixture.session.Meta().SessionID, fixture.source.ProjectID)
	if err != nil {
		t.Fatalf("SessionBelongsToProject source: %v", err)
	}
	if !belongs {
		t.Fatal("failed plan changed session ownership")
	}
}

func TestSessionSnapshotImportAndRetargetRemoveLegacyWorkflowMetadata(t *testing.T) {
	t.Parallel()
	fixture := newSessionRetargetFixture(t)
	ctx := context.Background()
	sessionID := fixture.session.Meta().SessionID
	if _, err := fixture.store.db.ExecContext(ctx, `
UPDATE sessions
SET metadata_json = json_set(metadata_json, '$.workflow_session', json_object('run_id', 'stale-run'))
WHERE id = ?`, sessionID); err != nil {
		t.Fatalf("seed stale workflow metadata: %v", err)
	}
	record, err := fixture.store.ResolvePersistedSession(ctx, sessionID)
	if err != nil {
		t.Fatalf("ResolvePersistedSession before import: %v", err)
	}
	if err := fixture.store.ImportSessionSnapshot(ctx, session.PersistedStoreSnapshot{
		SessionDir: record.SessionDir,
		Meta:       *record.Meta,
	}); err != nil {
		t.Fatalf("ImportSessionSnapshot: %v", err)
	}
	assertWorkflowSessionMetadataAbsent(t, fixture.store, sessionID)
	if _, err := fixture.store.db.ExecContext(ctx, `
UPDATE sessions
SET metadata_json = json_set(metadata_json, '$.workflow_session', json_object('run_id', 'stale-run'))
WHERE id = ?`, sessionID); err != nil {
		t.Fatalf("restore stale workflow metadata: %v", err)
	}
	targetProjectID := fixture.targetProject.ProjectID
	plan, err := fixture.store.PlanSessionWorkspaceRetarget(ctx, SessionWorkspaceRetargetRequest{
		SessionID:     sessionID,
		WorkspaceRoot: t.TempDir(),
		ProjectID:     &targetProjectID,
	})
	if err != nil {
		t.Fatalf("PlanSessionWorkspaceRetarget: %v", err)
	}
	if _, err := fixture.store.CommitSessionWorkspaceRetarget(ctx, plan, time.Now().UTC()); err != nil {
		t.Fatalf("CommitSessionWorkspaceRetarget: %v", err)
	}
	assertWorkflowSessionMetadataAbsent(t, fixture.store, sessionID)
}

func assertWorkflowSessionMetadataAbsent(t *testing.T, store *Store, sessionID string) {
	t.Helper()
	var workflowMetadata sql.NullString
	if err := store.db.QueryRowContext(t.Context(), `
SELECT json_type(metadata_json, '$.workflow_session')
FROM sessions
WHERE id = ?`, sessionID).Scan(&workflowMetadata); err != nil {
		t.Fatalf("query workflow session metadata: %v", err)
	}
	if workflowMetadata.Valid {
		t.Fatalf("workflow session metadata = %q, want absent", workflowMetadata.String)
	}
}
