package app

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestLoadCorpusSnapshotIncludesDerivedDirectoriesAndExcludesGit(t *testing.T) {
	runner := &stubUIPathReferenceCommandRunner{output: nulJoinPathReferenceOutput("./cli/app/ui.go", ".github/workflows/release.yml", ".git/config")}
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
	if runner.name != "rg" {
		t.Fatalf("runner executable = %q, want rg", runner.name)
	}
	if runner.dir != "/tmp/workspace" {
		t.Fatalf("runner dir = %q, want /tmp/workspace", runner.dir)
	}
	if !reflect.DeepEqual(runner.args, []string{"--no-config", "--files", "-0", "--hidden", "-g", "!.git"}) {
		t.Fatalf("runner args = %+v", runner.args)
	}
}

func TestLoadCorpusSnapshotFiltersControlBearingFileAndKeepsSafeDirectory(t *testing.T) {
	runner := &stubUIPathReferenceCommandRunner{output: nulJoinPathReferenceOutput("dir/line\nbreak.txt")}
	service := &uiPathReferenceSearchService{runner: runner}

	snapshot, err := service.loadCorpusSnapshot(context.Background(), "/tmp/workspace")
	if err != nil {
		t.Fatalf("loadCorpusSnapshot() error = %v", err)
	}
	want := []uiPathReferenceCandidate{{Path: "dir", Directory: true}}
	if !reflect.DeepEqual(snapshot.Candidates, want) {
		t.Fatalf("snapshot candidates = %+v, want %+v", snapshot.Candidates, want)
	}
}

func TestPathReferenceSearchServiceRunsOnlyOneMatcherAtATime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		matcher := newBlockingUIPathReferenceMatcher()
		service := newTestUIPathReferenceSearchService(&stubUIPathReferenceCommandRunner{output: nulJoinPathReferenceOutput("cli/app/ui.go")}, matcher)
		t.Cleanup(service.Stop)

		service.Search(uiPathReferenceSearchRequest{WorkspaceRoot: "/tmp/workspace", DraftToken: 1, QueryToken: 1, NormalizedQuery: "ab"})
		waitForPathReferenceEventType[uiPathReferenceCorpusReadyMsg](t, service.Events())
		if got := <-matcher.started; got != "ab" {
			t.Fatalf("first started query = %q, want ab", got)
		}

		service.Search(uiPathReferenceSearchRequest{WorkspaceRoot: "/tmp/workspace", DraftToken: 2, QueryToken: 2, NormalizedQuery: "abc"})
		synctest.Wait()
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
	})
}

func TestPathReferenceSearchFiltersUnsafeCandidatesBeforeMatchLimit(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		paths := make([]string, 0, slashCommandPickerLines+2)
		for index := 0; index <= slashCommandPickerLines; index++ {
			paths = append(paths, fmt.Sprintf("unsafe/match-%02d\x1b[31m.go", index))
		}
		const safePath = "safe/match.go"
		paths = append(paths, safePath)

		service := newTestUIPathReferenceSearchService(
			&stubUIPathReferenceCommandRunner{output: nulJoinPathReferenceOutput(paths...)},
			unsafeFirstUIPathReferenceMatcher{},
		)
		t.Cleanup(service.Stop)
		service.Search(uiPathReferenceSearchRequest{WorkspaceRoot: "/tmp/workspace", DraftToken: 1, QueryToken: 1, NormalizedQuery: "match"})
		waitForPathReferenceEventType[uiPathReferenceCorpusReadyMsg](t, service.Events())
		result := waitForPathReferenceEventType[uiPathReferenceMatchResultMsg](t, service.Events())

		foundSafePath := false
		for _, candidate := range result.Matches {
			if !isTerminalSafePathReference(candidate.Path) {
				t.Fatalf("unsafe candidate reached limited matches: %q", candidate.Path)
			}
			if candidate.Path == safePath {
				foundSafePath = true
			}
		}
		if !foundSafePath {
			t.Fatalf("safe path missing after unsafe higher-ranked candidates: %+v", result.Matches)
		}
	})
}

func TestPathReferenceSearchServiceEmitsLoadingDelayForPendingQuery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		matcher := newBlockingUIPathReferenceMatcher()
		service := newTestUIPathReferenceSearchService(&stubUIPathReferenceCommandRunner{output: nulJoinPathReferenceOutput("cli/app/ui.go")}, matcher)
		t.Cleanup(service.Stop)
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
	})
}

func TestPathReferenceSearchServiceRetriesAfterCorpusBuildFailure(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		runner := &sequenceUIPathReferenceCommandRunner{
			results: []sequenceUIPathReferenceCommandResult{
				{err: errors.New("boom")},
				{output: nulJoinPathReferenceOutput("cli/app/ui.go")},
			},
		}
		matcher := newBlockingUIPathReferenceMatcher()
		service := newTestUIPathReferenceSearchService(runner, matcher)
		t.Cleanup(service.Stop)

		service.Search(uiPathReferenceSearchRequest{WorkspaceRoot: "/tmp/workspace", DraftToken: 1, QueryToken: 1, NormalizedQuery: "ab"})
		failed := waitForPathReferenceEventType[uiPathReferenceCorpusFailedMsg](t, service.Events())
		if failed.Err == nil || failed.Err.Error() != "boom" {
			t.Fatalf("unexpected failure event: %+v", failed)
		}

		service.Search(uiPathReferenceSearchRequest{WorkspaceRoot: "/tmp/workspace", DraftToken: 2, QueryToken: 2, NormalizedQuery: "abc"})
		ready := waitForPathReferenceEventType[uiPathReferenceCorpusReadyMsg](t, service.Events())
		if ready.WorkspaceRoot != "/tmp/workspace" {
			t.Fatalf("unexpected ready event: %+v", ready)
		}
		if got := <-matcher.started; got != "abc" {
			t.Fatalf("started query = %q, want abc", got)
		}
		matcher.releaseOne()
		result := waitForPathReferenceEventType[uiPathReferenceMatchResultMsg](t, service.Events())
		if result.NormalizedQuery != "abc" {
			t.Fatalf("result query = %q, want abc", result.NormalizedQuery)
		}
		if runner.callCount() != 2 {
			t.Fatalf("runner call count = %d, want 2", runner.callCount())
		}
	})
}

func TestPathReferenceSearchServiceStopUnblocksAndPreventsFurtherRequests(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		matcher := newBlockingUIPathReferenceMatcher()
		var releaseOnce sync.Once
		releaseMatcher := func() { releaseOnce.Do(matcher.releaseOne) }
		t.Cleanup(releaseMatcher)
		service := newTestUIPathReferenceSearchService(
			&stubUIPathReferenceCommandRunner{output: nulJoinPathReferenceOutput("cli/app/ui.go")},
			matcher,
		)
		t.Cleanup(service.Stop)
		service.Search(uiPathReferenceSearchRequest{WorkspaceRoot: "/tmp/workspace", DraftToken: 1, QueryToken: 1, NormalizedQuery: "ab"})
		waitForPathReferenceEventType[uiPathReferenceCorpusReadyMsg](t, service.Events())
		if got := <-matcher.started; got != "ab" {
			t.Fatalf("started query = %q, want ab", got)
		}

		stopped := make(chan struct{})
		go func() {
			service.Stop()
			close(stopped)
		}()
		<-stopped
		releaseMatcher()
		if got := <-matcher.finished; got != "ab" {
			t.Fatalf("finished query = %q, want ab", got)
		}

		service.StartPrewarm("/tmp/workspace")
		service.Search(uiPathReferenceSearchRequest{WorkspaceRoot: "/tmp/workspace", DraftToken: 1, QueryToken: 1, NormalizedQuery: "ab"})

		synctest.Wait()
		select {
		case msg := <-service.Events():
			t.Fatalf("did not expect events after stop, got %T", msg)
		default:
		}
	})
}

func TestLoadPathReferenceCorpusSnapshotUsesListedFilesAndExcludesEmptyDirs(t *testing.T) {
	// The runner supplies ripgrep's ignore-filtered file listing; only listed
	// files and their derived parent directories belong in the corpus.
	runner := &stubUIPathReferenceCommandRunner{
		output: nulJoinPathReferenceOutput(".gitignore", "src/pkg/main.go"),
	}
	service := &uiPathReferenceSearchService{runner: runner}
	snapshot, err := service.loadCorpusSnapshot(context.Background(), "/tmp/workspace")
	if err != nil {
		t.Fatalf("loadCorpusSnapshot() error = %v", err)
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

type sequenceUIPathReferenceCommandResult struct {
	output []byte
	err    error
}

type sequenceUIPathReferenceCommandRunner struct {
	mu      sync.Mutex
	results []sequenceUIPathReferenceCommandResult
	index   int
}

func (s *sequenceUIPathReferenceCommandRunner) Output(_ context.Context, _ string, _ string, _ ...string) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.index >= len(s.results) {
		return nil, errors.New("unexpected extra call")
	}
	result := s.results[s.index]
	s.index++
	if result.err != nil {
		return nil, result.err
	}
	return append([]byte(nil), result.output...), nil
}

func (s *sequenceUIPathReferenceCommandRunner) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.index
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
	mu       sync.Mutex
	calls    []string
	started  chan string
	release  chan struct{}
	finished chan string
}

type unsafeFirstUIPathReferenceMatcher struct{}

func (unsafeFirstUIPathReferenceMatcher) Match(_ string, candidates []uiPathReferenceCandidate, limit int) []uiPathReferenceCandidate {
	results := make([]uiPathReferenceCandidate, 0, min(limit, len(candidates)))
	for _, candidate := range candidates {
		if isTerminalSafePathReference(candidate.Path) {
			continue
		}
		results = append(results, candidate)
		if len(results) == limit {
			return results
		}
	}
	for _, candidate := range candidates {
		if !isTerminalSafePathReference(candidate.Path) {
			continue
		}
		results = append(results, candidate)
		if len(results) == limit {
			break
		}
	}
	return results
}

func newBlockingUIPathReferenceMatcher() *blockingUIPathReferenceMatcher {
	return &blockingUIPathReferenceMatcher{
		started:  make(chan string, 8),
		release:  make(chan struct{}, 8),
		finished: make(chan string, 8),
	}
}

func (m *blockingUIPathReferenceMatcher) Match(query string, _ []uiPathReferenceCandidate, _ int) []uiPathReferenceCandidate {
	m.mu.Lock()
	m.calls = append(m.calls, query)
	m.mu.Unlock()
	m.started <- query
	<-m.release
	m.finished <- query
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
	for {
		msg := <-events
		if typed, ok := msg.(T); ok {
			return typed
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

func TestLoadCorpusSnapshotPropagatesRunnerError(t *testing.T) {
	runner := &stubUIPathReferenceCommandRunner{err: errors.New("boom")}
	service := &uiPathReferenceSearchService{runner: runner}
	if _, err := service.loadCorpusSnapshot(context.Background(), "/tmp/workspace"); err == nil || err.Error() != "boom" {
		t.Fatalf("expected runner error, got %v", err)
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
