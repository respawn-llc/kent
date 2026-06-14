package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"builder/server/metadata"
	"builder/server/session"
	"builder/shared/buildinfo"
)

// seedFixtureDB creates a metadata DB under root/db with one project, one
// workspace pointing at an external repo, and one builder-managed worktree
// whose canonical_root_path lies under worktreeRootPrefix (which the caller sets
// to the OLD root to simulate pre-rebase state).
func seedFixtureDB(t *testing.T, root string, externalRepo string, worktreePath string) {
	t.Helper()
	store, err := metadata.Open(root)
	if err != nil {
		t.Fatalf("open fixture db: %v", err)
	}
	defer func() { _ = store.Close() }()
	db := store.DB()
	now := time.Now().UnixMilli()
	if _, err := db.Exec(`INSERT INTO projects (id, display_name, created_at_unix_ms, updated_at_unix_ms) VALUES (?, ?, ?, ?)`,
		"proj1", "Fixture", now, now); err != nil {
		t.Fatalf("insert project: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO workspaces (id, project_id, canonical_root_path, created_at_unix_ms, updated_at_unix_ms) VALUES (?, ?, ?, ?, ?)`,
		"ws1", "proj1", externalRepo, now, now); err != nil {
		t.Fatalf("insert workspace: %v", err)
	}
	if _, err := db.Exec(`INSERT INTO worktrees (id, workspace_id, canonical_root_path, builder_managed, created_at_unix_ms, updated_at_unix_ms) VALUES (?, ?, ?, 1, ?, ?)`,
		"wt1", "ws1", worktreePath, now, now); err != nil {
		t.Fatalf("insert worktree: %v", err)
	}
}

func readWorktreePath(t *testing.T, root string, id string) string {
	t.Helper()
	store, err := metadata.Open(root)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer func() { _ = store.Close() }()
	var path string
	if err := store.DB().QueryRow("SELECT canonical_root_path FROM worktrees WHERE id = ?", id).Scan(&path); err != nil {
		t.Fatalf("read worktree %s: %v", id, err)
	}
	return path
}

func writeFixtureSession(t *testing.T, root string, projectID string, sessionID string, meta session.Meta) string {
	t.Helper()
	dir := filepath.Join(root, "projects", projectID, "sessions", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture meta: %v", err)
	}
	path := filepath.Join(dir, "session.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture session: %v", err)
	}
	return path
}

func TestRebaseDatabaseRewritesWorktreeNotWorkspace(t *testing.T) {
	tmp := t.TempDir()
	oldRoot := filepath.Join(tmp, ".builder")
	newRoot := filepath.Join(tmp, ".kent")
	externalRepo := filepath.Join(tmp, "code", "myrepo")
	oldWorktree := filepath.Join(oldRoot, "worktrees", "wt1")

	// DB lives at the (already-moved) new root but still holds old paths.
	seedFixtureDB(t, newRoot, externalRepo, oldWorktree)

	if err := rebaseDatabase(context.Background(), newRoot, oldRoot); err != nil {
		t.Fatalf("rebaseDatabase: %v", err)
	}

	gotWorktree := readWorktreePath(t, newRoot, "wt1")
	wantWorktree := filepath.Join(newRoot, "worktrees", "wt1")
	if gotWorktree != wantWorktree {
		t.Fatalf("worktree path = %q, want %q", gotWorktree, wantWorktree)
	}

	// Workspace (external repo) must be untouched.
	store, err := metadata.Open(newRoot)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer func() { _ = store.Close() }()
	var wsPath string
	if err := store.DB().QueryRow("SELECT canonical_root_path FROM workspaces WHERE id = ?", "ws1").Scan(&wsPath); err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	if wsPath != externalRepo {
		t.Fatalf("workspace path was rewritten: got %q, want %q", wsPath, externalRepo)
	}
}

func TestRebaseSessionsAndVerify(t *testing.T) {
	tmp := t.TempDir()
	oldRoot := filepath.Join(tmp, ".builder")
	newRoot := filepath.Join(tmp, ".kent")
	externalRepo := filepath.Join(tmp, "code", "myrepo")

	meta := session.Meta{
		SessionID:     "sess1",
		WorkspaceRoot: externalRepo,
		WorktreeReminder: &session.WorktreeReminderState{
			WorktreePath:  filepath.Join(oldRoot, "worktrees", "wt1"),
			EffectiveCwd:  filepath.Join(oldRoot, "worktrees", "wt1", "sub"),
			WorkspaceRoot: externalRepo,
		},
	}
	path := writeFixtureSession(t, newRoot, "proj1", "sess1", meta)

	rebased, err := rebaseSessions(newRoot, oldRoot)
	if err != nil {
		t.Fatalf("rebaseSessions: %v", err)
	}
	if rebased != 1 {
		t.Fatalf("rebased = %d, want 1", rebased)
	}

	got, err := session.ReadMetaFromDir(filepath.Dir(path))
	if err != nil {
		t.Fatalf("reread meta: %v", err)
	}
	if want := filepath.Join(newRoot, "worktrees", "wt1"); got.WorktreeReminder.WorktreePath != want {
		t.Fatalf("worktree_path = %q, want %q", got.WorktreeReminder.WorktreePath, want)
	}
	if got.WorktreeReminder.WorkspaceRoot != externalRepo {
		t.Fatalf("reminder workspace_root rewritten: %q", got.WorktreeReminder.WorkspaceRoot)
	}

	// A clean tree (no DB worktrees, rebased sessions) verifies with no offenders.
	offenders, err := verifyNoOldRootRefs(context.Background(), newRoot, oldRoot)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(offenders) != 0 {
		t.Fatalf("expected no offenders, got %v", offenders)
	}
}

func TestVerifyDetectsResidualOldPath(t *testing.T) {
	tmp := t.TempDir()
	oldRoot := filepath.Join(tmp, ".builder")
	newRoot := filepath.Join(tmp, ".kent")

	// A session left un-rebased (still under old root) must be reported.
	meta := session.Meta{
		SessionID: "sess1",
		WorktreeReminder: &session.WorktreeReminderState{
			WorktreePath: filepath.Join(oldRoot, "worktrees", "wt1"),
		},
	}
	writeFixtureSession(t, newRoot, "proj1", "sess1", meta)

	offenders, err := verifyNoOldRootRefs(context.Background(), newRoot, oldRoot)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(offenders) == 0 {
		t.Fatal("expected verification to report the residual old-root path")
	}
}

func TestCollisionGuard(t *testing.T) {
	tmp := t.TempDir()

	// Absent target: not empty-to-remove, no error.
	empty, err := collisionGuard(filepath.Join(tmp, "absent"))
	if err != nil || empty {
		t.Fatalf("absent: empty=%v err=%v", empty, err)
	}

	// Empty dir: removable.
	emptyDir := filepath.Join(tmp, "empty")
	if err := os.MkdirAll(emptyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	empty, err = collisionGuard(emptyDir)
	if err != nil || !empty {
		t.Fatalf("empty dir: empty=%v err=%v", empty, err)
	}

	// Non-empty dir: refuse.
	full := filepath.Join(tmp, "full")
	if err := os.MkdirAll(full, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(full, "x"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := collisionGuard(full); err == nil {
		t.Fatal("non-empty dir: expected refusal")
	}
}

func TestRunCompatGateRefusesNonMigrateCommands(t *testing.T) {
	for _, args := range [][]string{nil, {"serve"}, {"run", "x"}, {"service"}, {"service", "status"}} {
		var out, errBuf bytes.Buffer
		code := runCompatGate(args, &out, &errBuf)
		if code != 1 {
			t.Fatalf("args %v: exit = %d, want 1", args, code)
		}
		if errBuf.Len() == 0 {
			t.Fatalf("args %v: expected a migration notice on stderr, got none", args)
		}
	}
}

func TestRunCompatGateRoutesMigrate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	var out, errBuf bytes.Buffer
	// No ~/.builder under the fresh HOME, so migrate exits cleanly (0). The gate
	// routing migrate (rather than refusing) is the behavior under test: a refusal
	// would return exit 1, so a clean 0 proves migrate was dispatched.
	code := runCompatGate([]string{"migrate"}, &out, &errBuf)
	if code != 0 {
		t.Fatalf("migrate exit = %d, want 0; stderr=%q", code, errBuf.String())
	}
}

func TestRunCompatGateAllowsVersionAndHelp(t *testing.T) {
	original := buildinfo.Version
	buildinfo.Version = "9.9.9-test"
	t.Cleanup(func() { buildinfo.Version = original })

	for _, flag := range []string{"--version", "-version", "-v"} {
		var out, errBuf bytes.Buffer
		if code := runCompatGate([]string{flag}, &out, &errBuf); code != 0 {
			t.Fatalf("%s exit = %d, want 0; stderr=%q", flag, code, errBuf.String())
		}
		if got := strings.TrimSpace(out.String()); got != buildinfo.Version {
			t.Fatalf("%s stdout = %q, want %q", flag, got, buildinfo.Version)
		}
	}

	for _, flag := range []string{"--help", "-help", "-h"} {
		var out, errBuf bytes.Buffer
		if code := runCompatGate([]string{flag}, &out, &errBuf); code != 0 {
			t.Fatalf("%s exit = %d, want 0", flag, code)
		}
		if out.Len() == 0 {
			t.Fatalf("%s: expected help text on stdout, got none", flag)
		}
	}
}

func TestRunMigrationEarlyReturns(t *testing.T) {
	t.Run("absent old root", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		var out bytes.Buffer
		if err := runMigration(context.Background(), migrateOptions{}, &out, &out); err != nil {
			t.Fatalf("expected nil for absent old root, got %v", err)
		}
	})

	t.Run("already-migrated symlink", func(t *testing.T) {
		home := t.TempDir()
		t.Setenv("HOME", home)
		newRoot := filepath.Join(home, ".kent")
		if err := os.MkdirAll(newRoot, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(newRoot, filepath.Join(home, ".builder")); err != nil {
			t.Fatal(err)
		}
		var out bytes.Buffer
		if err := runMigration(context.Background(), migrateOptions{}, &out, &out); err != nil {
			t.Fatalf("expected nil for already-migrated symlink, got %v", err)
		}
	})
}

func seedSessionMetadataRow(t *testing.T, root string, projectID string, sessionID string, metadataJSON string) {
	t.Helper()
	store, err := metadata.Open(root)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer func() { _ = store.Close() }()
	now := time.Now().UnixMilli()
	if _, err := store.DB().Exec(
		`INSERT INTO sessions (id, project_id, artifact_relpath, created_at_unix_ms, updated_at_unix_ms, metadata_json) VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, projectID, "projects/"+projectID+"/sessions/"+sessionID, now, now, metadataJSON); err != nil {
		t.Fatalf("insert session row: %v", err)
	}
}

func readSessionMetadataJSON(t *testing.T, root string, sessionID string) string {
	t.Helper()
	store, err := metadata.Open(root)
	if err != nil {
		t.Fatalf("reopen db: %v", err)
	}
	defer func() { _ = store.Close() }()
	var md string
	if err := store.DB().QueryRow("SELECT metadata_json FROM sessions WHERE id = ?", sessionID).Scan(&md); err != nil {
		t.Fatalf("read session metadata %s: %v", sessionID, err)
	}
	return md
}

// TestRebaseSessionMetadataDBRewritesWorktreeReminder is the regression that the
// session.json-only rebase missed: modern builder stores worktree reminders in
// the sessions.metadata_json DB column, not on disk. The rebase must rewrite the
// reminder's under-root paths, leave external repo paths and goal prose alone,
// and leave reminder-less rows byte-identical.
func TestRebaseSessionMetadataDBRewritesWorktreeReminder(t *testing.T) {
	tmp := t.TempDir()
	oldRoot := filepath.Join(tmp, ".builder")
	newRoot := filepath.Join(tmp, ".kent")
	externalRepo := filepath.Join(tmp, "code", "myrepo")
	oldWorktree := filepath.Join(oldRoot, "worktrees", "wt1")

	// The worktree row is already at the new root (rebaseDatabase's job); this
	// test isolates the session-metadata rebase, so verify must hinge only on it.
	seedFixtureDB(t, newRoot, externalRepo, filepath.Join(newRoot, "worktrees", "wt1"))

	reminder := session.WorktreeReminderState{
		WorktreePath:  oldWorktree,
		EffectiveCwd:  filepath.Join(oldWorktree, "sub"),
		WorkspaceRoot: externalRepo, // external: must NOT be rebased
	}
	payload := sessionMetadataPayload{
		WorkspaceRoot:      externalRepo,
		WorkspaceContainer: "myrepo",
		WorktreeReminder:   &reminder,
		Goal:               &session.GoalState{Objective: "finish the plan noted under .builder/plans"},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode fixture metadata: %v", err)
	}
	seedSessionMetadataRow(t, newRoot, "proj1", "sess-db-1", string(encoded))

	plainJSON := `{"workspace_root":"` + externalRepo + `","workspace_container":"myrepo","headless_active":true,"compaction_soon_reminder_issued":false,"generated_recovered_warning_issued":false,"worktree_reminder":null,"goal":null}`
	seedSessionMetadataRow(t, newRoot, "proj1", "sess-db-2", plainJSON)

	n, err := rebaseSessionMetadataDB(context.Background(), newRoot, oldRoot)
	if err != nil {
		t.Fatalf("rebaseSessionMetadataDB: %v", err)
	}
	if n != 1 {
		t.Fatalf("rebased = %d, want 1", n)
	}

	var got sessionMetadataPayload
	if err := json.Unmarshal([]byte(readSessionMetadataJSON(t, newRoot, "sess-db-1")), &got); err != nil {
		t.Fatalf("decode rebased metadata: %v", err)
	}
	wantWorktree := filepath.Join(newRoot, "worktrees", "wt1")
	if got.WorktreeReminder.WorktreePath != wantWorktree {
		t.Fatalf("worktree_path = %q, want %q", got.WorktreeReminder.WorktreePath, wantWorktree)
	}
	if want := filepath.Join(wantWorktree, "sub"); got.WorktreeReminder.EffectiveCwd != want {
		t.Fatalf("effective_cwd = %q, want %q", got.WorktreeReminder.EffectiveCwd, want)
	}
	if got.WorkspaceRoot != externalRepo || got.WorktreeReminder.WorkspaceRoot != externalRepo {
		t.Fatalf("external workspace root was rewritten: %q / %q", got.WorkspaceRoot, got.WorktreeReminder.WorkspaceRoot)
	}
	// Goal prose must be preserved verbatim — a ".builder" mention in free text is
	// not a structured path and must never be rewritten.
	if got.Goal == nil || !strings.Contains(got.Goal.Objective, ".builder/plans") {
		t.Fatalf("goal objective prose was altered or dropped: %+v", got.Goal)
	}

	if got2 := readSessionMetadataJSON(t, newRoot, "sess-db-2"); got2 != plainJSON {
		t.Fatalf("reminder-less session metadata changed:\n got=%s\nwant=%s", got2, plainJSON)
	}

	offenders, err := verifyNoOldRootRefs(context.Background(), newRoot, oldRoot)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(offenders) != 0 {
		t.Fatalf("expected no offenders after rebase, got %v", offenders)
	}
}

// TestVerifyDetectsResidualSessionMetadataPath proves the verification pass now
// inspects the DB session metadata, so a missed rebase can no longer pass as a
// false "clean".
func TestVerifyDetectsResidualSessionMetadataPath(t *testing.T) {
	tmp := t.TempDir()
	oldRoot := filepath.Join(tmp, ".builder")
	newRoot := filepath.Join(tmp, ".kent")
	externalRepo := filepath.Join(tmp, "code", "myrepo")
	seedFixtureDB(t, newRoot, externalRepo, filepath.Join(newRoot, "worktrees", "wt1"))

	reminder := session.WorktreeReminderState{WorktreePath: filepath.Join(oldRoot, "worktrees", "wt1")}
	encoded, err := json.Marshal(sessionMetadataPayload{WorktreeReminder: &reminder})
	if err != nil {
		t.Fatalf("encode fixture metadata: %v", err)
	}
	seedSessionMetadataRow(t, newRoot, "proj1", "sess-resid", string(encoded))

	offenders, err := verifyNoOldRootRefs(context.Background(), newRoot, oldRoot)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if len(offenders) == 0 {
		t.Fatal("expected verification to report the residual session metadata path")
	}
}
