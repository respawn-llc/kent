package session

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ptrMeta(meta Meta) *Meta {
	return &meta
}

func TestOpenByIDUsesPersistedSessionResolver(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "projects", "project-b", "sessions", "session-b")
	target, err := Create(sessionDir, "sessions", "/tmp/work-b")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := target.SetContinuationContext(ContinuationContext{OpenAIBaseURL: "http://target.local/v1"}); err != nil {
		t.Fatalf("set continuation context: %v", err)
	}

	opened, err := OpenByID(root, target.Meta().SessionID, WithPersistedSessionResolver(stubPersistedSessionResolver{record: PersistedSessionRecord{
		SessionDir: target.Dir(),
		Meta:       ptrMeta(target.Meta()),
	}}))
	if err != nil {
		t.Fatalf("open by id: %v", err)
	}
	meta := opened.Meta()
	if meta.SessionID != target.Meta().SessionID {
		t.Fatalf("expected session id %q, got %q", target.Meta().SessionID, meta.SessionID)
	}
	if meta.WorkspaceRoot != "/tmp/work-b" {
		t.Fatalf("expected workspace root from target session, got %q", meta.WorkspaceRoot)
	}
	if meta.Continuation == nil || meta.Continuation.OpenAIBaseURL != "http://target.local/v1" {
		t.Fatalf("expected continuation context from target session, got %+v", meta.Continuation)
	}
}

func TestOpenByIDRejectsWithoutPersistedSessionResolver(t *testing.T) {
	root := t.TempDir()
	if _, err := OpenByID(root, "missing-session"); err == nil || !errors.Is(err, errPersistedSessionResolverRequired) {
		t.Fatalf("expected missing resolver error, got %v", err)
	}
}

func TestSetWorkspaceRootPreservesWorkspaceContainer(t *testing.T) {
	root := t.TempDir()
	store, err := Create(root, "workspace-container", "/tmp/work-a")
	if err != nil {
		t.Fatalf("create store: %v", err)
	}

	if err := store.SetWorkspaceRoot("/tmp/work-b"); err != nil {
		t.Fatalf("SetWorkspaceRoot: %v", err)
	}
	if got := store.Meta().WorkspaceContainer; got != "workspace-container" {
		t.Fatalf("workspace container = %q, want workspace-container", got)
	}
	if got := store.Meta().WorkspaceRoot; got != "/tmp/work-b" {
		t.Fatalf("workspace root = %q, want /tmp/work-b", got)
	}

	reopened, err := Open(store.Dir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if got := reopened.Meta().WorkspaceContainer; got != "workspace-container" {
		t.Fatalf("persisted workspace container = %q, want workspace-container", got)
	}
	if got := reopened.Meta().WorkspaceRoot; got != "/tmp/work-b" {
		t.Fatalf("persisted workspace root = %q, want /tmp/work-b", got)
	}
}

func TestEventLogOnlySessionDirectoryRemainsUndiscoverable(t *testing.T) {
	root := t.TempDir()
	container := filepath.Join(root, "projects", "project-1", "sessions")
	sessionDir := filepath.Join(container, "ghost-session")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	writeSessionFixtureEvents(t, sessionDir, []Event{{
		Seq:       1,
		Timestamp: time.Now().UTC(),
		Kind:      "message",
		StepID:    "legacy-step",
		Payload:   mustFixtureJSON(t, map[string]any{"role": "user", "content": "hello"}),
	}})

	items, err := ListSessions(container)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected event-log-only session to stay invisible in picker, got %+v", items)
	}
	if _, err := Open(sessionDir); err == nil || !errors.Is(err, ErrReadSessionMeta) {
		t.Fatalf("expected direct open to fail on missing session meta, got %v", err)
	}
	if _, err := OpenByID(root, "ghost-session"); err == nil {
		t.Fatal("expected event-log-only session to remain undiscoverable via OpenByID")
	}
}

func TestListSessionsSkipsMalformedSessionMetadata(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "bad-session")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, sessionFile), []byte("{"), 0o644); err != nil {
		t.Fatalf("write malformed session file: %v", err)
	}

	items, err := ListSessions(root)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected malformed metadata to be skipped, got %+v", items)
	}
}

func TestListSessionsSkipsSymlinkedSessionMetadata(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "target-session")
	writeSessionFixtureMeta(t, targetDir, Meta{
		SessionID:          "target-session",
		WorkspaceRoot:      "/tmp/work-target",
		WorkspaceContainer: "workspace-target",
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	})
	sessionDir := filepath.Join(root, "bad-session")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.Symlink(filepath.Join(targetDir, sessionFile), filepath.Join(sessionDir, sessionFile)); err != nil {
		t.Fatalf("symlink session meta: %v", err)
	}

	items, err := ListSessions(root)
	if err != nil {
		t.Fatalf("list sessions: %v", err)
	}
	if len(items) != 0 {
		t.Fatalf("expected symlinked metadata to be skipped, got %+v", items)
	}
}

func TestOpenRejectsSymlinkedSessionMetadata(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "target-session")
	writeSessionFixtureMeta(t, targetDir, Meta{
		SessionID:          "target-session",
		WorkspaceRoot:      "/tmp/work-target",
		WorkspaceContainer: "workspace-target",
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	})
	sessionDir := filepath.Join(root, "bad-session")
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	if err := os.Symlink(filepath.Join(targetDir, sessionFile), filepath.Join(sessionDir, sessionFile)); err != nil {
		t.Fatalf("symlink session meta: %v", err)
	}
	writeSessionFixtureEvents(t, sessionDir, nil)

	if _, err := Open(sessionDir); err == nil || !errors.Is(err, ErrSessionFileSymlink) {
		t.Fatalf("expected open to reject symlinked session meta, got %v", err)
	}
}

func TestOpenRejectsSymlinkedEventsFile(t *testing.T) {
	root := t.TempDir()
	targetDir := filepath.Join(root, "target-session")
	writeSessionFixtureEvents(t, targetDir, []Event{{
		Seq:       1,
		Timestamp: time.Now().UTC(),
		Kind:      "message",
		StepID:    "target-step",
		Payload:   mustFixtureJSON(t, map[string]any{"role": "user", "content": "hello"}),
	}})
	sessionDir := filepath.Join(root, "bad-session")
	writeSessionFixtureMeta(t, sessionDir, Meta{
		SessionID:          "bad-session",
		WorkspaceRoot:      "/tmp/work-bad",
		WorkspaceContainer: "workspace-bad",
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
	})
	if err := os.Symlink(filepath.Join(targetDir, eventsFile), filepath.Join(sessionDir, eventsFile)); err != nil {
		t.Fatalf("symlink events file: %v", err)
	}

	if _, err := Open(sessionDir); err == nil || !errors.Is(err, ErrSessionFileSymlink) {
		t.Fatalf("expected open to reject symlinked events file, got %v", err)
	}
}

func TestOpenInitializesMissingEventsFileFromSessionMetadata(t *testing.T) {
	root := t.TempDir()
	sessionDir := filepath.Join(root, "session-without-events")
	writeSessionFixtureMeta(t, sessionDir, Meta{
		SessionID:          "session-without-events",
		WorkspaceRoot:      "/tmp/work",
		WorkspaceContainer: "workspace-x",
		CreatedAt:          time.Now().UTC(),
		UpdatedAt:          time.Now().UTC(),
		LastSequence:       3,
	})

	opened, err := Open(sessionDir)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if _, err := os.Stat(filepath.Join(sessionDir, eventsFile)); err != nil {
		t.Fatalf("expected missing events file to be recreated: %v", err)
	}
	if opened.Meta().LastSequence != 0 {
		t.Fatalf("expected reopened last sequence to reconcile to zero, got %d", opened.Meta().LastSequence)
	}
	events, err := collectEvents(opened)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected recreated events file to be empty, got %+v", events)
	}
}

func TestReadEventsIgnoresTrailingTruncatedEOFLine(t *testing.T) {
	store := newSessionTestStore(t)
	if _, _, err := store.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "u1"}); err != nil {
		t.Fatalf("append event: %v", err)
	}

	fp, err := os.OpenFile(filepath.Join(store.Dir(), eventsFile), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open events for append: %v", err)
	}
	if _, err := fp.WriteString("{\"seq\":2"); err != nil {
		_ = fp.Close()
		t.Fatalf("append truncated line: %v", err)
	}
	if err := fp.Close(); err != nil {
		t.Fatalf("close events file: %v", err)
	}

	events, err := collectEvents(store)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events len = %d, want 1", len(events))
	}
	if events[0].Seq != 1 {
		t.Fatalf("expected seq=1, got %d", events[0].Seq)
	}
}

func TestAppendEventRepairsTruncatedTailBeforeAppend(t *testing.T) {
	store := newSessionTestStore(t)
	if _, _, err := store.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "u1"}); err != nil {
		t.Fatalf("append event 1: %v", err)
	}

	fp, err := os.OpenFile(filepath.Join(store.Dir(), eventsFile), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open events for append: %v", err)
	}
	if _, err := fp.WriteString("{\"seq\":2"); err != nil {
		_ = fp.Close()
		t.Fatalf("append truncated tail: %v", err)
	}
	if err := fp.Close(); err != nil {
		t.Fatalf("close events file: %v", err)
	}

	e2, _, err := store.AppendEvent("s2", "message", map[string]any{"role": "assistant", "content": "a2"})
	if err != nil {
		t.Fatalf("append event 2: %v", err)
	}
	if e2.Seq != 2 {
		t.Fatalf("expected seq=2, got %d", e2.Seq)
	}

	events, err := collectEvents(store)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events len = %d, want 2", len(events))
	}
	if events[0].Seq != 1 || events[1].Seq != 2 {
		t.Fatalf("unexpected event sequence: %+v", events)
	}
}

func TestOpenReconcilesMetaLastSequenceFromEventLog(t *testing.T) {
	store := newSessionTestStore(t)
	if _, _, err := store.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "u1"}); err != nil {
		t.Fatalf("append event 1: %v", err)
	}
	if _, _, err := store.AppendEvent("s2", "message", map[string]any{"role": "assistant", "content": "a1"}); err != nil {
		t.Fatalf("append event 2: %v", err)
	}

	sessionPath := filepath.Join(store.Dir(), sessionFile)
	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatalf("read session file: %v", err)
	}
	var meta Meta
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("decode session meta: %v", err)
	}
	meta.LastSequence = 0
	rewritten, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("encode session meta: %v", err)
	}
	if err := os.WriteFile(sessionPath, rewritten, 0o644); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	reopened, err := Open(store.Dir())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	if reopened.Meta().LastSequence != 2 {
		t.Fatalf("expected reconciled last sequence 2, got %d", reopened.Meta().LastSequence)
	}
	next, _, err := reopened.AppendEvent("s3", "message", map[string]any{"role": "user", "content": "u2"})
	if err != nil {
		t.Fatalf("append event after reconcile: %v", err)
	}
	if next.Seq != 3 {
		t.Fatalf("expected seq=3 after reopen reconciliation, got %d", next.Seq)
	}
}

func TestPeriodicCompactionRewritesCanonicalEventsLog(t *testing.T) {
	root := t.TempDir()
	store, err := Create(
		root,
		"workspace-x",
		"/tmp/work",
		WithEventLogCompaction(1, 1),
		WithEventLogFSyncPolicy(EventLogFSyncNever),
	)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	if _, _, err := store.AppendEvent("s1", "message", map[string]any{"role": "user", "content": "u1"}); err != nil {
		t.Fatalf("append event 1: %v", err)
	}

	fp, err := os.OpenFile(filepath.Join(store.Dir(), eventsFile), os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open events file: %v", err)
	}
	if _, err := fp.WriteString("\n\n"); err != nil {
		_ = fp.Close()
		t.Fatalf("append padding lines: %v", err)
	}
	if err := fp.Close(); err != nil {
		t.Fatalf("close events file: %v", err)
	}

	if _, _, err := store.AppendEvent("s2", "message", map[string]any{"role": "assistant", "content": "a1"}); err != nil {
		t.Fatalf("append event 2: %v", err)
	}

	raw, err := os.ReadFile(filepath.Join(store.Dir(), eventsFile))
	if err != nil {
		t.Fatalf("read events file: %v", err)
	}
	if strings.Contains(string(raw), "\n\n") {
		t.Fatalf("expected compaction to remove blank lines from events log")
	}
}

func writeSessionFixtureMeta(t *testing.T, sessionDir string, meta Meta) {
	t.Helper()
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		t.Fatalf("marshal session meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, sessionFile), data, 0o644); err != nil {
		t.Fatalf("write session meta: %v", err)
	}
}

func writeSessionFixtureEvents(t *testing.T, sessionDir string, events []Event) {
	t.Helper()
	if err := os.MkdirAll(sessionDir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	lines, err := encodeEventLines(events, false)
	if err != nil {
		t.Fatalf("encode events: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sessionDir, eventsFile), lines, 0o644); err != nil {
		t.Fatalf("write events: %v", err)
	}
}

func mustFixtureJSON(t *testing.T, payload any) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal fixture payload: %v", err)
	}
	return data
}
