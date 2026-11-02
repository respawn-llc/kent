package metadata

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"core/server/sessionstore"
	"core/shared/sessioncontract"
)

func TestSessionContextFactsPersistIndependentlyFromFullSnapshots(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openInMemoryMetadataTestStore(t, root)
	workspaceRoot := root + "/workspace"
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll workspace: %v", err)
	}
	workspace, err := store.RegisterWorkspaceBinding(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	options := store.AuthoritativeSessionStoreOptions()
	sessionStore, err := session.Create(
		workspace.CanonicalRoot,
		"workspace",
		workspace.CanonicalRoot,
		sessioncontract.SessionCategoryMain,
		options...,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if sessionStore.ContextFacts().LastModelToolActivityAt != nil {
		t.Fatal("new Session has activity")
	}
	completedAt := time.UnixMilli(1_800_000_000_123)
	if err := sessionStore.RecordModelToolActivity(completedAt); err != nil {
		t.Fatalf("RecordModelToolActivity: %v", err)
	}
	if err := sessionStore.RecordModelToolActivity(completedAt.Add(-time.Hour)); err != nil {
		t.Fatalf("late completion: %v", err)
	}
	if err := sessionStore.SetInputDraft("unsent", nil); err != nil {
		t.Fatal(err)
	}
	if err := sessionStore.SetThinkingOverride(metadataStringPointer("low")); err != nil {
		t.Fatal(err)
	}
	if err := sessionStore.SetSessionContextFacts(4, true); err != nil {
		t.Fatalf("SetSessionContextFacts: %v", err)
	}
	if err := sessionStore.SetName(metadataStringPointer("ordinary snapshot")); err != nil {
		t.Fatalf("SetName: %v", err)
	}
	reopened, err := session.OpenByID(root, sessionStore.Meta().SessionID, options...)
	if err != nil {
		t.Fatalf("OpenByID: %v", err)
	}
	facts := reopened.ContextFacts()
	if facts.LastModelToolActivityAt == nil || !facts.LastModelToolActivityAt.Equal(completedAt) {
		t.Fatalf("reopened activity = %v, want %v", facts.LastModelToolActivityAt, completedAt)
	}
	if facts.CompletedCompactionCount == nil || *facts.CompletedCompactionCount != 4 ||
		facts.ManualCompactEligible == nil || !*facts.ManualCompactEligible {
		t.Fatalf("reopened Context facts = %+v", facts)
	}

	var count sql.NullInt64
	var eligible sql.NullInt64
	if err := store.DB().QueryRowContext(ctx, `
SELECT completed_compaction_count, manual_compact_eligible
FROM sessions
WHERE id = ?`, sessionStore.Meta().SessionID).Scan(&count, &eligible); err != nil {
		t.Fatalf("query Context facts: %v", err)
	}
	if !count.Valid || count.Int64 != 4 || !eligible.Valid || eligible.Int64 != 1 {
		t.Fatalf("stored Context facts = count:%+v eligible:%+v", count, eligible)
	}
}

func TestOlderSessionContextFactsRemainAbsent(t *testing.T) {
	root := t.TempDir()
	store := openInMemoryMetadataTestStore(t, root)
	workspaceRoot := root + "/workspace"
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll workspace: %v", err)
	}
	workspace, err := store.RegisterWorkspaceBinding(context.Background(), workspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	options := store.AuthoritativeSessionStoreOptions()
	sessionStore, err := session.Create(
		workspace.CanonicalRoot,
		"workspace",
		workspace.CanonicalRoot,
		sessioncontract.SessionCategoryMain,
		options...,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.DB().ExecContext(
		context.Background(),
		`UPDATE sessions SET completed_compaction_count = NULL, manual_compact_eligible = NULL WHERE id = ?`,
		sessionStore.Meta().SessionID,
	); err != nil {
		t.Fatalf("clear Context facts: %v", err)
	}

	reopened, err := session.OpenByID(root, sessionStore.Meta().SessionID, options...)
	if err != nil {
		t.Fatalf("OpenByID: %v", err)
	}
	facts := reopened.ContextFacts()
	if facts.CompletedCompactionCount != nil || facts.ManualCompactEligible != nil {
		t.Fatalf("older Session Context facts = %+v, want absent", facts)
	}
}

func TestSessionContextFactWriterRejectsMissingSession(t *testing.T) {
	store := openInMemoryMetadataTestStore(t, t.TempDir())
	count := 1
	eligible := true
	err := store.WriteSessionContextFacts(context.Background(), "missing", session.SessionContextFacts{
		CompletedCompactionCount: &count,
		ManualCompactEligible:    &eligible,
	})
	if !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("WriteSessionContextFacts error = %v, want Session not found", err)
	}
	if _, err := store.WriteModelToolActivity(t.Context(), "missing", time.Now()); !errors.Is(err, session.ErrSessionNotFound) {
		t.Fatalf("WriteModelToolActivity error = %v, want Session not found", err)
	}
}

func TestFailedActivityWriteIsNotPublishedByLaterSnapshot(t *testing.T) {
	root := t.TempDir()
	store := openInMemoryMetadataTestStore(t, root)
	workspaceRoot := root + "/workspace"
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	workspace, err := store.RegisterWorkspaceBinding(t.Context(), workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	options := store.AuthoritativeSessionStoreOptions()
	conversation, err := session.Create(workspace.CanonicalRoot, "workspace", workspace.CanonicalRoot,
		sessioncontract.SessionCategoryMain, options...)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.DB().ExecContext(t.Context(), `
CREATE TRIGGER fail_activity_write BEFORE UPDATE OF last_model_tool_activity_at_unix_ms ON sessions
BEGIN SELECT RAISE(FAIL, 'forced activity write failure'); END;`); err != nil {
		t.Fatal(err)
	}
	if err := conversation.RecordModelToolActivity(time.Now()); err == nil {
		t.Fatal("activity write succeeded despite trigger")
	}
	if conversation.ContextFacts().LastModelToolActivityAt != nil {
		t.Fatal("failed activity was published")
	}
	if _, err := store.DB().ExecContext(t.Context(), `DROP TRIGGER fail_activity_write`); err != nil {
		t.Fatal(err)
	}
	if err := conversation.SetName(metadataStringPointer("later snapshot")); err != nil {
		t.Fatal(err)
	}
	reopened, err := session.OpenByID(root, conversation.Meta().SessionID, options...)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.ContextFacts().LastModelToolActivityAt != nil {
		t.Fatal("snapshot persisted failed activity")
	}
}

func TestActivityMigrationLeavesExistingSessionAbsent(t *testing.T) {
	db, err := openDatabaseAtVersionForTest(t, 101)
	if err != nil {
		t.Fatal(err)
	}
	execSeed(t, db, "project", `INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms)
VALUES ('activity-project', 'Project', 1000, 1000)`)
	execSeed(t, db, "session", `INSERT INTO sessions (id, project_id, artifact_relpath, short_id, created_at_unix_ms, updated_at_unix_ms)
VALUES ('activity-session', 'activity-project', 'sessions/activity-session', 1, 1000, 2000)`)
	store := migrateMetadataTestStore(t, t.TempDir(), db)
	record, err := store.ResolvePersistedSession(t.Context(), "activity-session")
	if err != nil {
		t.Fatal(err)
	}
	if record.ContextFacts.LastModelToolActivityAt != nil {
		t.Fatal("migration backfilled activity")
	}
}

func TestFailedContextWriteIsNotPublishedByLaterFullSnapshot(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	store := openInMemoryMetadataTestStore(t, root)
	workspaceRoot := root + "/workspace"
	if err := os.MkdirAll(workspaceRoot, 0o755); err != nil {
		t.Fatalf("MkdirAll workspace: %v", err)
	}
	workspace, err := store.RegisterWorkspaceBinding(ctx, workspaceRoot)
	if err != nil {
		t.Fatalf("RegisterWorkspaceBinding: %v", err)
	}
	options := store.AuthoritativeSessionStoreOptions()
	sessionStore, err := session.Create(
		workspace.CanonicalRoot,
		"workspace",
		workspace.CanonicalRoot,
		sessioncontract.SessionCategoryMain,
		options...,
	)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := store.DB().ExecContext(
		ctx,
		`DROP TRIGGER IF EXISTS fail_context_write;
CREATE TRIGGER fail_context_write
BEFORE UPDATE OF manual_compact_eligible ON sessions
BEGIN
    SELECT RAISE(FAIL, 'forced Context write failure');
END;`,
	); err != nil {
		t.Fatalf("install failing trigger: %v", err)
	}
	if err := sessionStore.SetManualCompactEligibility(true); err == nil {
		t.Fatal("SetManualCompactEligibility succeeded despite trigger")
	}
	if _, err := store.DB().ExecContext(ctx, `DROP TRIGGER fail_context_write`); err != nil {
		t.Fatalf("drop failing trigger: %v", err)
	}
	if err := sessionStore.SetName(metadataStringPointer("later full snapshot")); err != nil {
		t.Fatalf("SetName: %v", err)
	}

	var eligible sql.NullInt64
	if err := store.DB().QueryRowContext(
		ctx,
		`SELECT manual_compact_eligible FROM sessions WHERE id = ?`,
		sessionStore.Meta().SessionID,
	).Scan(&eligible); err != nil {
		t.Fatalf("query eligibility: %v", err)
	}
	if !eligible.Valid || eligible.Int64 != 0 {
		t.Fatalf("later full snapshot published failed eligibility: %+v", eligible)
	}
}
