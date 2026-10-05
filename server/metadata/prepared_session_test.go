package metadata

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"core/server/session"
	"core/shared/runtimeids"
	"core/shared/sessioncontract"
)

func TestRetainedSessionTargetSharesCallerTransaction(t *testing.T) {
	store, cfg, binding := newMetadataTestStore(t)
	retained := createMetadataTestSession(t, store, cfg, binding)
	id := retained.Meta().SessionID
	worktree := WorktreeRecord{
		ID: runtimeids.NewGraphEntityID(), WorkspaceID: binding.WorkspaceID,
		CanonicalRoot: t.TempDir(), Managed: true,
	}
	if err := store.UpsertWorktreeRecord(t.Context(), worktree); err != nil {
		t.Fatal(err)
	}
	update := SessionExecutionTargetUpdate{
		SessionID:  id,
		Workspace:  &SessionExecutionTargetUpdateWorkspace{ID: binding.WorkspaceID},
		Worktree:   &SessionExecutionTargetUpdateWorktree{ID: worktree.ID},
		CwdRelpath: ".",
	}
	for _, commit := range []bool{false, true} {
		tx, err := store.db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := UpdateSessionExecutionTargetInTransaction(t.Context(), tx, update); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatal(err)
		}
		target, err := store.ResolveSessionExecutionTarget(t.Context(), id)
		if err != nil {
			t.Fatal(err)
		}
		if !commit && target.Worktree != nil {
			t.Fatalf("rolled back target was published: %+v", target)
		}
		if commit && (target.Worktree == nil || target.Worktree.Id != worktree.ID) {
			t.Fatalf("committed target was not published: %+v", target)
		}
	}
	foreign, err := store.RegisterWorkspaceBinding(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	update.Workspace = &SessionExecutionTargetUpdateWorkspace{ID: foreign.WorkspaceID}
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	err = UpdateSessionExecutionTargetInTransaction(t.Context(), tx, update)
	rollbackErr := tx.Rollback()
	var mismatch *WorktreeWorkspaceMismatchError
	if !errors.As(err, &mismatch) || rollbackErr != nil {
		t.Fatalf("cross-workspace worktree update = %v, rollback=%v; want rejected ownership", err, rollbackErr)
	}
}

func TestPreparedSessionSharesCallerTransaction(t *testing.T) {
	store, cfg := newMetadataTestStoreWithoutBinding(t)
	binding, err := store.RegisterWorkspaceBinding(t.Context(), cfg.WorkspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	id := runtimeids.NewSessionID()
	descriptor, err := session.NewCreateSessionDescriptor(id, filepath.Join(cfg.PersistenceRoot, "projects", binding.ProjectID, "sessions"), "workspace", cfg.WorkspaceRoot, sessioncontract.SessionCategoryMain)
	if err != nil {
		t.Fatal(err)
	}
	creation, err := session.PrepareCreation(session.CreationRequest{Descriptor: descriptor})
	if err != nil {
		t.Fatal(err)
	}
	prepared, err := store.PrepareSessionSnapshot(t.Context(), creation.Snapshot(), SessionExecutionTargetUpdate{
		SessionID: id.String(), Workspace: &SessionExecutionTargetUpdateWorkspace{ID: binding.WorkspaceID}, CwdRelpath: ".",
	})
	if err != nil {
		t.Fatal(err)
	}
	invalid := creation.Snapshot()
	invalid.Meta.WorktreeReminder = &session.WorktreeReminderState{}
	if _, err := store.PrepareSessionSnapshot(t.Context(), invalid, prepared.ExecutionTarget()); err == nil {
		t.Fatal("preparation accepted invalid prompt-facing worktree metadata")
	}
	tx, err := store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.ExecContext(t.Context(), "UPDATE workspaces SET canonical_root_path = ? WHERE id = ?", t.TempDir(), binding.WorkspaceID); err != nil {
		_ = tx.Rollback()
		t.Fatal(err)
	}
	if err := InsertPreparedSession(t.Context(), tx, prepared); err == nil {
		_ = tx.Rollback()
		t.Fatal("cutover accepted a changed workspace")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	for _, commit := range []bool{false, true} {
		tx, err := store.db.BeginTx(t.Context(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := InsertPreparedSession(t.Context(), tx, prepared); err != nil {
			_ = tx.Rollback()
			t.Fatal(err)
		}
		if commit {
			err = tx.Commit()
		} else {
			err = tx.Rollback()
		}
		if err != nil {
			t.Fatal(err)
		}
		record, err := store.ResolvePersistedSession(t.Context(), id.String())
		if !commit {
			if !errors.Is(err, session.ErrSessionNotFound) {
				t.Fatalf("rolled back Session was published: %v", err)
			}
		} else if err != nil || record.Meta.SessionID != id.String() {
			t.Fatalf("committed Session not published: %+v, %v", record, err)
		}
	}
	if _, err := os.Stat(creation.Snapshot().SessionDir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("database cutover created Session artifacts: %v", err)
	}
	established := creation.Snapshot()
	established.Meta.LastSequence = 7
	established.Meta.Name = "established"
	if err := store.ImportSessionSnapshot(t.Context(), established); err != nil {
		t.Fatal(err)
	}
	tx, err = store.db.BeginTx(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := InsertPreparedSession(t.Context(), tx, prepared); err == nil {
		_ = tx.Rollback()
		t.Fatal("cutover overwrote an existing Session")
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	record, err := store.ResolvePersistedSession(t.Context(), id.String())
	if err != nil || record.Meta.LastSequence != established.Meta.LastSequence || record.Meta.Name != established.Meta.Name {
		t.Fatalf("failed duplicate insertion changed existing Session: %+v, %v", record, err)
	}
}
