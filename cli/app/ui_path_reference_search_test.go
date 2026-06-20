package app

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLoadCorpusSnapshotIncludesDerivedDirectoriesAndExcludesGit(t *testing.T) {
	runner := &stubUIPathReferenceCommandRunner{output: nulJoinPathReferenceOutput("cli/app/ui.go", ".github/workflows/release.yml", ".git/config")}
	service := &uiPathReferenceSearchService{runner: runner}

	snapshot, err := service.loadCorpusSnapshot(context.Background(), "/tmp/workspace")
	if err != nil {
		t.Fatalf("loadCorpusSnapshot() error = %v", err)
	}
	got := make([]uiPathReferenceCandidate, 0, len(snapshot.Candidates))
	for _, candidate := range snapshot.Candidates {
		got = append(got, candidate)
	}
	want := []uiPathReferenceCandidate{
		{Path: ".github", Directory: true},
		{Path: ".github/workflows", Directory: true},
		{Path: ".github/workflows/release.yml"},
		{Path: "cli", Directory: true},
		{Path: "cli/app", Directory: true},
		{Path: "cli/app/ui.go"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("snapshot candidates = %+v, want %+v", got, want)
	}
	if runner.dir != "/tmp/workspace" {
		t.Fatalf("runner dir = %q, want /tmp/workspace", runner.dir)
	}
	if !reflect.DeepEqual(runner.args, []string{"--no-config", "--files", "-0", "--hidden", "-g", "!.git"}) {
		t.Fatalf("runner args = %+v", runner.args)
	}
}

func TestLoadCorpusSnapshotPreservesFilenameNewlinesWithNullSeparatedOutput(t *testing.T) {
	runner := &stubUIPathReferenceCommandRunner{output: nulJoinPathReferenceOutput("dir/line\nbreak.txt")}
	service := &uiPathReferenceSearchService{runner: runner}

	snapshot, err := service.loadCorpusSnapshot(context.Background(), "/tmp/workspace")
	if err != nil {
		t.Fatalf("loadCorpusSnapshot() error = %v", err)
	}
	want := []uiPathReferenceCandidate{{Path: "dir", Directory: true}, {Path: "dir/line\nbreak.txt"}}
	if !reflect.DeepEqual(snapshot.Candidates, want) {
		t.Fatalf("snapshot candidates = %+v, want %+v", snapshot.Candidates, want)
	}
}

func TestPathReferenceSearchServiceRunsOnlyOneMatcherAtATime(t *testing.T) {
	matcher := newBlockingUIPathReferenceMatcher()
	service := newTestUIPathReferenceSearchService(&stubUIPathReferenceCommandRunner{output: nulJoinPathReferenceOutput("cli/app/ui.go")}, matcher)

	service.Search(uiPathReferenceSearchRequest{WorkspaceRoot: "/tmp/workspace", DraftToken: 1, QueryToken: 1, NormalizedQuery: "ab"})
	waitForPathReferenceEventType[uiPathReferenceCorpusReadyMsg](t, service.Events())
	if got := <-matcher.started; got != "ab" {
		t.Fatalf("first started query = %q, want ab", got)
	}

	service.Search(uiPathReferenceSearchRequest{WorkspaceRoot: "/tmp/workspace", DraftToken: 2, QueryToken: 2, NormalizedQuery: "abc"})
	time.Sleep(20 * time.Millisecond)
	if matcher.callCount() != 1 {
		t.Fatalf("matcher call count = %d, want 1 while first search is still running", matcher.callCount())
	}

	matcher.releaseOne()
	first := waitForPathReferenceEventType[uiPathReferenceMatchResultMsg](t, service.Events())
	if first.NormalizedQuery != "ab" {
		t.Fatalf("first result query = %q, want ab", first.NormalizedQuery)
	}
	if got := <-matcher.started; got != "abc" {
		t.Fatalf("second started query = %q, want abc", got)
	}
	matcher.releaseOne()
	second := waitForPathReferenceEventType[uiPathReferenceMatchResultMsg](t, service.Events())
	if second.NormalizedQuery != "abc" {
		t.Fatalf("second result query = %q, want abc", second.NormalizedQuery)
	}
}

func TestPathReferenceSearchServiceEmitsLoadingDelayForPendingQuery(t *testing.T) {
	matcher := newBlockingUIPathReferenceMatcher()
	service := newTestUIPathReferenceSearchService(&stubUIPathReferenceCommandRunner{output: nulJoinPathReferenceOutput("cli/app/ui.go")}, matcher)
	service.loadingDelay = 10 * time.Millisecond

	service.Search(uiPathReferenceSearchRequest{WorkspaceRoot: "/tmp/workspace", DraftToken: 7, QueryToken: 9, NormalizedQuery: "abc"})
	waitForPathReferenceEventType[uiPathReferenceCorpusReadyMsg](t, service.Events())
	if got := <-matcher.started; got != "abc" {
		t.Fatalf("started query = %q, want abc", got)
	}
	loading := waitForPathReferenceEventType[uiPathReferenceLoadingDelayMsg](t, service.Events())
	if loading.DraftToken != 7 || loading.QueryToken != 9 || loading.NormalizedQuery != "abc" {
		t.Fatalf("unexpected loading event: %+v", loading)
	}
	matcher.releaseOne()
	_ = waitForPathReferenceEventType[uiPathReferenceMatchResultMsg](t, service.Events())
}

func TestPathReferenceSearchServiceStopUnblocksAndPreventsFurtherRequests(t *testing.T) {
	service := newTestUIPathReferenceSearchService(&stubUIPathReferenceCommandRunner{output: nulJoinPathReferenceOutput("cli/app/ui.go")}, fuzzyUIPathReferenceMatcher{})
	service.Stop()

	service.StartPrewarm("/tmp/workspace")
	service.Search(uiPathReferenceSearchRequest{WorkspaceRoot: "/tmp/workspace", DraftToken: 1, QueryToken: 1, NormalizedQuery: "ab"})

	select {
	case msg := <-service.Events():
		t.Fatalf("did not expect events after stop, got %T", msg)
	case <-time.After(50 * time.Millisecond):
	}
}

func TestLoadPathReferenceCorpusSnapshotHonorsIgnorePolicyAndExcludesEmptyDirs(t *testing.T) {
	root := t.TempDir()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = root
	cmd.Env = sanitizedGitEnv(os.Environ())
	if err := cmd.Run(); err != nil {
		t.Fatalf("git init: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("node_modules\nbuild\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src", "pkg"), 0o755); err != nil {
		t.Fatalf("mkdir src/pkg: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "src", "pkg", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write main.go: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "node_modules", "left-pad"), 0o755); err != nil {
		t.Fatalf("mkdir node_modules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "node_modules", "left-pad", "index.js"), []byte("module.exports = 1\n"), 0o644); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "build"), 0o755); err != nil {
		t.Fatalf("mkdir build: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "build", "out.txt"), []byte("ignored\n"), 0o644); err != nil {
		t.Fatalf("write ignored build file: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "empty-dir"), 0o755); err != nil {
		t.Fatalf("mkdir empty-dir: %v", err)
	}

	snapshot, err := loadPathReferenceCorpusSnapshotForWorkspace(context.Background(), root)
	if err != nil {
		t.Fatalf("loadPathReferenceCorpusSnapshotForWorkspace() error = %v", err)
	}
	paths := make([]string, 0, len(snapshot.Candidates))
	for _, candidate := range snapshot.Candidates {
		paths = append(paths, candidate.Path)
	}
	if containsString(paths, "node_modules/left-pad/index.js") || containsString(paths, "node_modules") {
		t.Fatalf("expected ignored node_modules content excluded, got %+v", paths)
	}
	if containsString(paths, "build/out.txt") || containsString(paths, "build") {
		t.Fatalf("expected ignored build content excluded, got %+v", paths)
	}
	if containsString(paths, "empty-dir") {
		t.Fatalf("expected empty directories excluded, got %+v", paths)
	}
	if !containsString(paths, "src/pkg/main.go") || !containsString(paths, "src") || !containsString(paths, "src/pkg") {
		t.Fatalf("expected non-empty file and derived dirs present, got %+v", paths)
	}
}

type stubUIPathReferenceCommandRunner struct {
	output []byte
	err    error
	dir    string
	name   string
	args   []string
}

func (s *stubUIPathReferenceCommandRunner) Output(_ context.Context, dir string, name string, args ...string) ([]byte, error) {
	s.dir = dir
	s.name = name
	s.args = append([]string(nil), args...)
	if s.err != nil {
		return nil, s.err
	}
	return append([]byte(nil), s.output...), nil
}

type blockingUIPathReferenceMatcher struct {
	mu      sync.Mutex
	calls   []string
	started chan string
	release chan struct{}
}

func newBlockingUIPathReferenceMatcher() *blockingUIPathReferenceMatcher {
	return &blockingUIPathReferenceMatcher{
		started: make(chan string, 8),
		release: make(chan struct{}, 8),
	}
}

func (m *blockingUIPathReferenceMatcher) Match(query string, _ []uiPathReferenceCandidate, _ int) []uiPathReferenceCandidate {
	m.mu.Lock()
	m.calls = append(m.calls, query)
	m.mu.Unlock()
	m.started <- query
	<-m.release
	return []uiPathReferenceCandidate{{Path: query}}
}

func (m *blockingUIPathReferenceMatcher) callCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.calls)
}

func (m *blockingUIPathReferenceMatcher) releaseOne() {
	m.release <- struct{}{}
}

func newTestUIPathReferenceSearchService(runner uiPathReferenceCommandRunner, matcher uiPathReferenceMatcher) *uiPathReferenceSearchService {
	service := &uiPathReferenceSearchService{
		events:       make(chan uiPathReferenceSearchEvent, 64),
		requests:     make(chan uiPathReferenceSearchRequestMessage, 64),
		runner:       runner,
		matcher:      matcher,
		buildTimeout: 0,
		loadingDelay: 0,
		stop:         make(chan struct{}),
		stopped:      make(chan struct{}),
	}
	go service.run()
	return service
}

func waitForPathReferenceEventType[T any](t *testing.T, events <-chan uiPathReferenceSearchEvent) T {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		select {
		case msg := <-events:
			if typed, ok := msg.(T); ok {
				return typed
			}
		case <-deadline:
			var zero T
			t.Fatalf("timed out waiting for %T", zero)
			return zero
		}
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func nulJoinPathReferenceOutput(paths ...string) []byte {
	if len(paths) == 0 {
		return nil
	}
	return []byte(strings.Join(paths, "\x00") + "\x00")
}

func TestNormalizePathReferenceCandidateRejectsGitPaths(t *testing.T) {
	if got := normalizePathReferenceCandidate(".git/config"); got != "" {
		t.Fatalf("normalizePathReferenceCandidate(.git/config) = %q, want empty", got)
	}
	if got := normalizePathReferenceCandidate("./cli/app/ui.go"); got != "cli/app/ui.go" {
		t.Fatalf("normalizePathReferenceCandidate returned %q", got)
	}
}

func TestLoadCorpusSnapshotTreatsEmptyRipgrepResultAsReadyEmptyCorpus(t *testing.T) {
	runner := &stubUIPathReferenceCommandRunner{err: stubUIPathReferenceExitError{code: 1}}
	service := &uiPathReferenceSearchService{runner: runner}
	snapshot, err := service.loadCorpusSnapshot(context.Background(), "/tmp/workspace")
	if err != nil {
		t.Fatalf("loadCorpusSnapshot() error = %v", err)
	}
	if len(snapshot.Candidates) != 0 {
		t.Fatalf("expected empty candidate set, got %+v", snapshot.Candidates)
	}
}

type stubUIPathReferenceExitError struct {
	code int
}

func (e stubUIPathReferenceExitError) Error() string {
	return "exit"
}

func (e stubUIPathReferenceExitError) ExitCode() int {
	return e.code
}
