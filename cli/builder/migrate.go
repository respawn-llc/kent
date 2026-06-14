package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"builder/server/metadata"
	"builder/server/rootlock"
	"builder/server/session"
	"builder/shared/buildinfo"
)

const migrateBackupDirName = ".migrate-backup"

type migrateOptions struct {
	dryRun bool
}

// writeMigrationNotice prints the message shown for every command the builder
// 2.0 compat build refuses to run. The guidance is rendered for the host OS so
// Windows users see PowerShell-correct commands.
func writeMigrationNotice(w io.Writer) {
	fmt.Fprint(w, migrationNoticeText(runtime.GOOS))
}

// runCompatGate implements the builder 2.0 refuse-to-start behavior and returns
// the process exit code. Only `builder migrate` and `builder service uninstall`
// are routed; every other invocation prints the migration notice and returns 1.
func runCompatGate(args []string, stdout io.Writer, stderr io.Writer) int {
	if len(args) > 0 {
		switch args[0] {
		case "migrate":
			return migrateSubcommand(args[1:], stdout, stderr)
		case "--version", "-version", "-v":
			// Identification probes are not "starting" the agent; installers and
			// package managers rely on them, so answer rather than refuse.
			fmt.Fprintln(stdout, buildinfo.Version)
			return 0
		case "--help", "-help", "-h":
			fmt.Fprint(stdout, migrationNoticeText(runtime.GOOS))
			return 0
		}
	}
	if len(args) > 1 && args[0] == "service" && args[1] == "uninstall" {
		return serviceSubcommand(args[1:], stdout, stderr)
	}
	writeMigrationNotice(stderr)
	return 1
}

func migrateSubcommand(args []string, stdout io.Writer, stderr io.Writer) int {
	fs := flag.NewFlagSet("builder migrate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "report what migrate would do without changing anything")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintln(stderr, "migrate does not accept positional arguments")
		return 2
	}
	if err := runMigration(context.Background(), migrateOptions{dryRun: *dryRun}, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "migrate: %v\n", err)
		return 1
	}
	return 0
}

func runMigration(ctx context.Context, opts migrateOptions, stdout io.Writer, stderr io.Writer) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("resolve home directory: %w", err)
	}
	oldRoot := filepath.Join(home, ".builder")
	newRoot := filepath.Join(home, ".kent")

	// Step 1 — guards.
	oldInfo, err := os.Lstat(oldRoot)
	if errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stdout, "Nothing to migrate: %s does not exist.\n", oldRoot)
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect %s: %w", oldRoot, err)
	}
	if isCompatLink(oldInfo) {
		fmt.Fprintf(stdout, "Already migrated: %s is already a compat link to %s; nothing to do.\n", oldRoot, newRoot)
		return nil
	}
	if !oldInfo.IsDir() {
		return fmt.Errorf("%s exists but is not a directory", oldRoot)
	}

	newRootEmpty, err := collisionGuard(newRoot)
	if err != nil {
		return err
	}

	if err := liveStateGuard(oldRoot); err != nil {
		return err
	}

	if opts.dryRun {
		return reportMigrationPlan(ctx, oldRoot, newRoot, stdout)
	}

	// Step 2 — stop + uninstall the old background service (best effort) and
	// record whether one existed so the summary can prompt a kent reinstall.
	serviceExisted := teardownOldService(ctx, stderr)

	// Remove a pre-existing empty ~/.kent so the rename can land.
	if newRootEmpty {
		if err := os.Remove(newRoot); err != nil {
			return fmt.Errorf("remove empty %s: %w", newRoot, err)
		}
	}

	// Step 3 — move.
	if err := os.Rename(oldRoot, newRoot); err != nil {
		return fmt.Errorf("move %s to %s: %w", oldRoot, newRoot, err)
	}
	fmt.Fprintf(stdout, "Moved %s -> %s\n", oldRoot, newRoot)

	// Step 4 — snapshot the to-be-mutated files (DB + session.json) before any
	// rewrite. events.jsonl is never touched, so the snapshot stays small.
	backupDir := filepath.Join(newRoot, migrateBackupDirName)
	if err := snapshotBeforeRewrite(newRoot, backupDir); err != nil {
		return fmt.Errorf("snapshot before rewrite: %w", err)
	}

	// Step 5 — rebase absolute paths in structured fields only.
	if err := rebaseDatabase(ctx, newRoot, oldRoot); err != nil {
		return fmt.Errorf("rebase database paths: %w", err)
	}
	sessionsRebased, err := rebaseSessions(newRoot, oldRoot)
	if err != nil {
		return fmt.Errorf("rebase session paths: %w", err)
	}

	// Step 6 — repair each moved worktree's reverse linkage in its external repo.
	repairWorktrees(ctx, newRoot, oldRoot, stderr)

	// Step 7 — repoint internal symlinks whose target moved with the root.
	if err := repointInternalSymlinks(newRoot, oldRoot); err != nil {
		return fmt.Errorf("repoint internal symlinks: %w", err)
	}

	// Step 8 — verification pass.
	offenders, err := verifyNoOldRootRefs(ctx, newRoot, oldRoot)
	if err != nil {
		return fmt.Errorf("verification pass: %w", err)
	}
	if len(offenders) > 0 {
		fmt.Fprintf(stderr, "Migration verification FAILED — the following still reference %s:\n", oldRoot)
		for _, offender := range offenders {
			fmt.Fprintf(stderr, "  - %s\n", offender)
		}
		return fmt.Errorf("verification failed; left %s and its snapshot at %s for manual restore and did NOT create the compat link", newRoot, backupDir)
	}

	// Step 9 — compat link old -> new (only on a clean verification). On Unix
	// this is a symlink; on Windows it is a directory junction (createCompatLink),
	// which, unlike a directory symlink, needs no elevation.
	if err := createCompatLink(newRoot, oldRoot); err != nil {
		return fmt.Errorf("create compat link %s -> %s: %w", oldRoot, newRoot, err)
	}

	// Step 10 — drop generated assets so kent regenerates them fresh.
	if err := os.RemoveAll(filepath.Join(newRoot, ".generated")); err != nil {
		return fmt.Errorf("drop generated assets: %w", err)
	}

	// Step 11 — summary.
	printMigrationSummary(stdout, oldRoot, newRoot, backupDir, serviceExisted, sessionsRebased)
	return nil
}

// collisionGuard inspects the target root. It returns (empty, nil) describing
// whether an existing ~/.kent is an empty directory that may be removed before
// the rename. It refuses when ~/.kent exists and is non-empty.
func collisionGuard(newRoot string) (bool, error) {
	info, err := os.Lstat(newRoot)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", newRoot, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return false, fmt.Errorf("%s already exists and is not a directory; remove or rename it and re-run", newRoot)
	}
	empty, err := dirIsEmpty(newRoot)
	if err != nil {
		return false, fmt.Errorf("inspect %s: %w", newRoot, err)
	}
	if !empty {
		return false, fmt.Errorf("%s already exists and is not empty — you ran kent before migrating; remove or rename the fresh %s and re-run", newRoot, newRoot)
	}
	return true, nil
}

// liveStateGuard refuses to migrate while any app-server holds the persistence
// root lock (a running service, `builder serve`, or active agents).
func liveStateGuard(oldRoot string) error {
	lease, err := rootlock.Acquire(oldRoot)
	if err != nil {
		if errors.Is(err, rootlock.ErrPersistenceRootBusy) {
			return errors.New("builder is still running (the persistence root is locked). Stop all builder activity first: quit interactive sessions, stop `builder serve`, and run `builder service stop` if the background service is installed, then re-run migrate")
		}
		return fmt.Errorf("check builder liveness: %w", err)
	}
	// We only needed to confirm nothing is live; release immediately so the
	// rename can proceed.
	return lease.Close()
}

func teardownOldService(ctx context.Context, stderr io.Writer) bool {
	spec, err := loadServiceSpec()
	if err != nil {
		fmt.Fprintf(stderr, "warning: could not load service spec for teardown: %v\n", err)
		return false
	}
	backend := serviceBackendFactory()
	sctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	status, statusErr := readServiceStatus(sctx, backend, spec)
	existed := statusErr == nil && (status.Installed || status.Loaded)
	if err := backend.Stop(sctx, spec); err != nil {
		fmt.Fprintf(stderr, "warning: stop old service: %v\n", err)
	}
	if err := backend.Uninstall(sctx, spec, true); err != nil {
		fmt.Fprintf(stderr, "warning: uninstall old service: %v\n", err)
	}
	return existed
}

func snapshotBeforeRewrite(newRoot string, backupDir string) error {
	if err := os.MkdirAll(backupDir, 0o700); err != nil {
		return err
	}
	dbDir := filepath.Join(newRoot, "db")
	for _, name := range []string{"main.sqlite3", "main.sqlite3-wal", "main.sqlite3-shm"} {
		src := filepath.Join(dbDir, name)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := copyFile(src, filepath.Join(backupDir, "db", name)); err != nil {
			return err
		}
	}
	projectsRoot := filepath.Join(newRoot, "projects")
	return filepath.WalkDir(projectsRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || d.Name() != "session.json" {
			return nil
		}
		rel, relErr := filepath.Rel(newRoot, p)
		if relErr != nil {
			return relErr
		}
		return copyFile(p, filepath.Join(backupDir, rel))
	})
}

func copyFile(src string, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}

func dirIsEmpty(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer func() { _ = f.Close() }()
	names, err := f.Readdirnames(1)
	if errors.Is(err, io.EOF) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return len(names) == 0, nil
}

// rebaseDatabase rewrites worktrees.canonical_root_path rows that lie under the
// old root. workspaces.canonical_root_path (the user's external repos) is never
// touched because those values do not lie under the persistence root.
func rebaseDatabase(ctx context.Context, newRoot string, oldRoot string) error {
	store, err := metadata.Open(newRoot)
	if err != nil {
		return err
	}
	defer func() { _ = store.Close() }()
	db := store.DB()

	type update struct {
		id      string
		newPath string
	}
	var updates []update
	rows, err := db.QueryContext(ctx, "SELECT id, canonical_root_path FROM worktrees")
	if err != nil {
		return err
	}
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			_ = rows.Close()
			return err
		}
		if newPath, ok := rebaseUnderRoot(path, oldRoot, newRoot); ok {
			updates = append(updates, update{id: id, newPath: newPath})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return err
	}
	// Fully drain + close before opening a transaction: the store caps the pool
	// at a single connection, so an open cursor would block BeginTx.
	if err := rows.Close(); err != nil {
		return err
	}
	if len(updates) == 0 {
		return nil
	}

	now := time.Now().UnixMilli()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	for _, u := range updates {
		if _, err := tx.ExecContext(ctx,
			"UPDATE worktrees SET canonical_root_path = ?, updated_at_unix_ms = ? WHERE id = ?",
			u.newPath, now, u.id); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

func rebaseSessions(newRoot string, oldRoot string) (int, error) {
	projectsRoot := filepath.Join(newRoot, "projects")
	count := 0
	err := filepath.WalkDir(projectsRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || d.Name() != "session.json" {
			return nil
		}
		meta, readErr := session.ReadMetaFromDir(filepath.Dir(p))
		if readErr != nil {
			return fmt.Errorf("read %s: %w", p, readErr)
		}
		if rebaseSessionMeta(&meta, oldRoot, newRoot) {
			if writeErr := writeSessionMeta(p, meta); writeErr != nil {
				return writeErr
			}
			count++
		}
		return nil
	})
	return count, err
}

// writeSessionMeta atomically rewrites session.json, matching the runtime's own
// on-disk format (2-space indented JSON, tmp file + rename).
func writeSessionMeta(path string, meta session.Meta) error {
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session meta %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write session meta tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace session meta %s: %w", path, err)
	}
	return nil
}

func repairWorktrees(ctx context.Context, newRoot string, oldRoot string, stderr io.Writer) {
	store, err := metadata.Open(newRoot)
	if err != nil {
		fmt.Fprintf(stderr, "warning: open db for worktree repair: %v\n", err)
		return
	}
	defer func() { _ = store.Close() }()

	type pair struct {
		worktreePath string
		repoPath     string
	}
	var pairs []pair
	rows, err := store.DB().QueryContext(ctx,
		"SELECT wt.canonical_root_path, ws.canonical_root_path FROM worktrees wt JOIN workspaces ws ON wt.workspace_id = ws.id")
	if err != nil {
		fmt.Fprintf(stderr, "warning: query worktrees for repair: %v\n", err)
		return
	}
	for rows.Next() {
		var worktreePath, repoPath string
		if err := rows.Scan(&worktreePath, &repoPath); err != nil {
			_ = rows.Close()
			fmt.Fprintf(stderr, "warning: scan worktree row for repair: %v\n", err)
			return
		}
		pairs = append(pairs, pair{worktreePath: worktreePath, repoPath: repoPath})
	}
	_ = rows.Close()

	for _, pr := range pairs {
		// Only builder-managed worktrees live under the persistence root and
		// were moved; main/external worktrees are left alone.
		if !pathStillUnderRoot(pr.worktreePath, newRoot) {
			continue
		}
		if info, err := os.Stat(pr.repoPath); err != nil || !info.IsDir() {
			fmt.Fprintf(stderr, "warning: external repo %s for worktree %s is missing; skipping git worktree repair (orphaned worktree, non-fatal)\n", pr.repoPath, pr.worktreePath)
			continue
		}
		cmd := exec.CommandContext(ctx, "git", "-C", pr.repoPath, "worktree", "repair", pr.worktreePath)
		if out, err := cmd.CombinedOutput(); err != nil {
			fmt.Fprintf(stderr, "warning: git worktree repair for %s failed: %v: %s\n", pr.worktreePath, err, strings.TrimSpace(string(out)))
		}
	}
}

func repointInternalSymlinks(newRoot string, oldRoot string) error {
	return filepath.WalkDir(newRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink == 0 {
			return nil
		}
		target, readErr := os.Readlink(p)
		if readErr != nil {
			// Unreadable symlink: leave it as-is rather than fail the migration.
			return nil
		}
		absTarget := target
		if !filepath.IsAbs(absTarget) {
			absTarget = filepath.Join(filepath.Dir(p), absTarget)
		}
		newTarget, ok := rebaseUnderRoot(absTarget, oldRoot, newRoot)
		if !ok {
			return nil
		}
		if err := os.Remove(p); err != nil {
			return err
		}
		return os.Symlink(newTarget, p)
	})
}

func verifyNoOldRootRefs(ctx context.Context, newRoot string, oldRoot string) ([]string, error) {
	var offenders []string

	store, err := metadata.Open(newRoot)
	if err != nil {
		return nil, err
	}
	rows, err := store.DB().QueryContext(ctx, "SELECT id, canonical_root_path FROM worktrees")
	if err != nil {
		_ = store.Close()
		return nil, err
	}
	for rows.Next() {
		var id, path string
		if err := rows.Scan(&id, &path); err != nil {
			_ = rows.Close()
			_ = store.Close()
			return nil, err
		}
		if pathStillUnderRoot(path, oldRoot) {
			offenders = append(offenders, fmt.Sprintf("db worktree %s -> %s", id, path))
		}
	}
	rowsErr := rows.Err()
	_ = rows.Close()
	_ = store.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}

	projectsRoot := filepath.Join(newRoot, "projects")
	walkErr := filepath.WalkDir(projectsRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		if d.IsDir() || d.Name() != "session.json" {
			return nil
		}
		meta, readErr := session.ReadMetaFromDir(filepath.Dir(p))
		if readErr != nil {
			// Unreadable meta is not a path-prefix offender; skip it.
			return nil
		}
		if meta.WorktreeReminder == nil {
			return nil
		}
		if pathStillUnderRoot(meta.WorktreeReminder.WorktreePath, oldRoot) {
			offenders = append(offenders, fmt.Sprintf("session %s -> worktree_path %s", p, meta.WorktreeReminder.WorktreePath))
		}
		if pathStillUnderRoot(meta.WorktreeReminder.EffectiveCwd, oldRoot) {
			offenders = append(offenders, fmt.Sprintf("session %s -> effective_cwd %s", p, meta.WorktreeReminder.EffectiveCwd))
		}
		return nil
	})
	if walkErr != nil {
		return offenders, walkErr
	}
	return offenders, nil
}

func reportMigrationPlan(ctx context.Context, oldRoot string, newRoot string, stdout io.Writer) error {
	fmt.Fprintln(stdout, "Dry run — no changes will be made.")
	fmt.Fprintf(stdout, "Would move %s -> %s\n", oldRoot, newRoot)

	if spec, err := loadServiceSpec(); err == nil {
		backend := serviceBackendFactory()
		sctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		status, statusErr := readServiceStatus(sctx, backend, spec)
		cancel()
		if statusErr == nil && (status.Installed || status.Loaded) {
			fmt.Fprintf(stdout, "Would stop and uninstall the old background service (installed=%v running=%v).\n", status.Installed, status.Running)
		}
	}

	dbPath := filepath.Join(oldRoot, "db", "main.sqlite3")
	if _, err := os.Stat(dbPath); err == nil {
		if db, err := sql.Open("sqlite", "file:"+dbPath+"?mode=ro"); err == nil {
			worktrees := 0
			rows, queryErr := db.QueryContext(ctx, "SELECT canonical_root_path FROM worktrees")
			if queryErr == nil {
				for rows.Next() {
					var path string
					if scanErr := rows.Scan(&path); scanErr == nil {
						if _, ok := rebaseUnderRoot(path, oldRoot, newRoot); ok {
							worktrees++
						}
					}
				}
				_ = rows.Close()
				fmt.Fprintf(stdout, "Would rebase %d worktree path(s) in the database.\n", worktrees)
			}
			_ = db.Close()
		}
	}

	sessions := 0
	_ = filepath.WalkDir(filepath.Join(oldRoot, "projects"), func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Name() != "session.json" {
			return nil
		}
		meta, readErr := session.ReadMetaFromDir(filepath.Dir(p))
		if readErr == nil {
			candidate := meta
			if rebaseSessionMeta(&candidate, oldRoot, newRoot) {
				sessions++
			}
		}
		return nil
	})
	fmt.Fprintf(stdout, "Would rebase %d session metadata file(s).\n", sessions)
	fmt.Fprintf(stdout, "After a clean verification, would create the compat symlink %s -> %s and drop %s/.generated.\n", oldRoot, newRoot, newRoot)
	return nil
}

func printMigrationSummary(stdout io.Writer, oldRoot string, newRoot string, backupDir string, serviceExisted bool, sessionsRebased int) {
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "Migration complete and verified.")
	fmt.Fprintf(stdout, "  Moved:       %s -> %s\n", oldRoot, newRoot)
	fmt.Fprintf(stdout, "  Compat link: %s -> %s\n", oldRoot, newRoot)
	fmt.Fprintf(stdout, "  Snapshot:    %s (delete when satisfied)\n", backupDir)
	fmt.Fprintf(stdout, "  Sessions:    %d metadata file(s) rebased\n", sessionsRebased)
	fmt.Fprintln(stdout, "")
	goos := runtime.GOOS
	fmt.Fprintln(stdout, "Next steps:")
	fmt.Fprintln(stdout, "  - Install Kent:  "+installKentSummaryHint(goos))
	fmt.Fprintln(stdout, "  - Map env vars:  "+envVarMapHint(goos))
	fmt.Fprintln(stdout, "  - Remove the old builder binary:  "+removeBinaryHint(goos))
	if serviceExisted {
		fmt.Fprintln(stdout, "  - Restore autostart under Kent:  kent service install")
	}
	for _, line := range legacyCompatLinkCleanupLines(goos, oldRoot) {
		fmt.Fprintln(stdout, line)
	}
}
