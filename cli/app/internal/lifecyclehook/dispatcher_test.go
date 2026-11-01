package lifecyclehook_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"core/cli/app/internal/lifecyclehook"
	"core/shared/lifecyclecontract"
)

const dispatcherProcessModeEnv = "KENT_TEST_LIFECYCLE_DISPATCHER_MODE"

func TestDispatcherInheritsProcessContextAndForwardsPayload(t *testing.T) {
	root := t.TempDir()
	t.Chdir(root)
	t.Setenv("KENT_TEST_LIFECYCLE_INHERITED", "present")
	processes := newDispatcherProcesses(t, "inspect")
	dispatcher := lifecyclehook.New(context.Background(), dispatcherProcessCommand())
	t.Cleanup(dispatcher.Close)

	event := testLifecycleEvent()
	dispatcher.Submit(event)
	record := processes.accept()
	if record.Workdir != root || record.Inherited != "present" {
		t.Fatalf("process context = %+v, want cwd %q and inherited env", record, root)
	}
	wantPayload, err := lifecyclecontract.Encode(event)
	if err != nil {
		t.Fatalf("encode submitted event: %v", err)
	}
	gotPayload, err := json.Marshal(record.Event)
	if err != nil {
		t.Fatalf("encode received event: %v", err)
	}
	if !bytes.Equal(gotPayload, wantPayload) {
		t.Fatalf("payload = %+v, want %+v", record.Event, event)
	}
}

func TestDispatcherSurfacesLaunchAndBoundedStderrFailures(t *testing.T) {
	t.Run("launch failure", func(t *testing.T) {
		dispatcher := lifecyclehook.New(t.Context(), []string{filepath.Join(t.TempDir(), "missing-hook")})
		t.Cleanup(dispatcher.Close)
		dispatcher.Submit(testLifecycleEvent())
		process := requireProcessIssue(t, waitForDispatcherIssue(t, dispatcher.Issues()))
		if process.Cause == nil || process.Category != lifecyclecontract.CategorySessionStart {
			t.Fatalf("issue = %+v", process)
		}
	})

	t.Run("nonzero stderr is bounded", func(t *testing.T) {
		processes := newDispatcherProcesses(t, "fail")
		dispatcher := lifecyclehook.New(t.Context(), dispatcherProcessCommand())
		t.Cleanup(dispatcher.Close)
		dispatcher.Submit(testLifecycleEvent())
		processes.accept()
		process := requireProcessIssue(t, waitForDispatcherIssue(t, dispatcher.Issues()))
		if process.Cause == nil {
			t.Fatal("missing nonzero exit error")
		}
		if len(process.Stderr) > 4*1024 {
			t.Fatalf("stderr length = %d, want at most 4096", len(process.Stderr))
		}
		if process.Stderr == "" {
			t.Fatal("bounded stderr omitted hook diagnostics")
		}
	})
}

func TestDispatcherOverlapsHooks(t *testing.T) {
	processes := newDispatcherProcesses(t, "block")
	dispatcher := lifecyclehook.New(t.Context(), dispatcherProcessCommand())
	t.Cleanup(dispatcher.Close)

	// Each hook remains blocked until cleanup, so the second start proves
	// submission and execution do not wait for the previous hook to finish.
	for range 2 {
		dispatcher.Submit(testLifecycleEvent())
		processes.accept()
	}
}

func TestDispatcherReportsEveryFailureWhenPresentationIsBacklogged(t *testing.T) {
	dispatcher := lifecyclehook.New(t.Context(), dispatcherProcessCommand())
	t.Cleanup(dispatcher.Close)

	const pendingCount = 192
	cause := errors.New("hook failed")
	latestCause := errors.New("latest hook failed")
	for index := range pendingCount {
		reportedCause := cause
		if index == pendingCount-1 {
			reportedCause = latestCause
		}
		dispatcher.Report(lifecyclehook.NewProcessIssue(
			lifecyclecontract.CategoryTaskError, reportedCause, "diagnostic",
		))
	}
	totalFailures := 0
	var latest lifecyclehook.ProcessIssue
	for totalFailures < pendingCount {
		issue := waitForDispatcherIssue(t, dispatcher.Issues())
		process := requireProcessIssue(t, issue)
		if (!errors.Is(process.Cause, cause) && !errors.Is(process.Cause, latestCause)) ||
			process.Category != lifecyclecontract.CategoryTaskError {
			t.Fatalf("issue lost failure details: %+v", issue)
		}
		latest = process
		totalFailures += issue.Count
	}
	if totalFailures != pendingCount {
		t.Fatalf("reported failure count = %d, want %d", totalFailures, pendingCount)
	}
	if !errors.Is(latest.Cause, latestCause) {
		t.Fatalf("latest failure = %v, want final reported failure", latest.Cause)
	}
}

func requireProcessIssue(t *testing.T, issue lifecyclehook.Issue) lifecyclehook.ProcessIssue {
	t.Helper()
	process, ok := issue.Detail.(lifecyclehook.ProcessIssue)
	if !ok {
		t.Fatalf("issue detail = %T, want process issue", issue.Detail)
	}
	return process
}

func TestDispatcherCloseReturnsWithoutWaitingForHookProcess(t *testing.T) {
	processes := newDispatcherProcesses(t, "block")
	dispatcher := lifecyclehook.New(t.Context(), dispatcherProcessCommand())
	t.Cleanup(dispatcher.Close)
	dispatcher.Submit(testLifecycleEvent())
	processes.accept()

	// The child is still waiting on IPC, not a timer or a release-file poll.
	dispatcher.Close()
	<-dispatcher.Done()
}

func TestLifecycleHookDispatcherProcess(t *testing.T) {
	mode := os.Getenv(dispatcherProcessModeEnv)
	if mode == "" {
		return
	}
	var event lifecyclecontract.Event
	if err := json.NewDecoder(os.Stdin).Decode(&event); err != nil {
		os.Exit(2)
	}
	conn, err := net.Dial("tcp", os.Getenv("KENT_TEST_LIFECYCLE_ADDRESS"))
	if err != nil {
		os.Exit(3)
	}
	defer conn.Close()
	workdir, err := os.Getwd()
	if err != nil {
		os.Exit(4)
	}
	record := dispatcherProcessRecord{
		Workdir: workdir, Inherited: os.Getenv("KENT_TEST_LIFECYCLE_INHERITED"), Event: event,
	}
	if err := json.NewEncoder(conn).Encode(record); err != nil {
		os.Exit(5)
	}
	switch mode {
	case "inspect":
		_, _ = io.WriteString(os.Stdout, strings.Repeat("ignored stdout", 64*1024))
	case "fail":
		_, _ = io.WriteString(os.Stderr, strings.Repeat("diagnostic", 1024))
		os.Exit(7)
	case "block":
		if _, err := io.Copy(io.Discard, conn); err != nil {
			os.Exit(6)
		}
	default:
		os.Exit(8)
	}
}

type dispatcherProcessRecord struct {
	Workdir   string                  `json:"workdir"`
	Inherited string                  `json:"inherited"`
	Event     lifecyclecontract.Event `json:"event"`
}

type dispatcherProcesses struct {
	t        *testing.T
	listener net.Listener
	conns    []*net.TCPConn
}

func newDispatcherProcesses(t *testing.T, mode string) *dispatcherProcesses {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for hook processes: %v", err)
	}
	processes := &dispatcherProcesses{t: t, listener: listener}
	t.Setenv(dispatcherProcessModeEnv, mode)
	t.Setenv("KENT_TEST_LIFECYCLE_ADDRESS", listener.Addr().String())
	t.Cleanup(func() {
		for _, conn := range processes.conns {
			// Release any live child and wait for its connection to close.
			// Close also cancels hooks, so an already terminated child is fine.
			_ = conn.CloseWrite()
			if _, err := io.Copy(io.Discard, conn); err != nil {
				t.Errorf("join hook connection: %v", err)
			}
			if err := conn.Close(); err != nil {
				t.Errorf("close hook connection: %v", err)
			}
		}
		if err := listener.Close(); err != nil {
			t.Errorf("close hook listener: %v", err)
		}
	})
	return processes
}

func (p *dispatcherProcesses) accept() dispatcherProcessRecord {
	p.t.Helper()
	conn, err := p.listener.Accept()
	if err != nil {
		p.t.Fatalf("accept hook process: %v", err)
	}
	p.conns = append(p.conns, conn.(*net.TCPConn))
	var record dispatcherProcessRecord
	if err := json.NewDecoder(conn).Decode(&record); err != nil {
		p.t.Fatalf("decode hook process record: %v", err)
	}
	return record
}

func dispatcherProcessCommand() []string {
	return []string{os.Args[0], "-test.run=^TestLifecycleHookDispatcherProcess$"}
}

func testLifecycleEvent() lifecyclecontract.Event {
	return lifecyclecontract.NewSessionStart(time.Now().UTC(), false, lifecyclecontract.Context{}, lifecyclecontract.OpeningKindNew)
}

func waitForDispatcherIssue(t *testing.T, issues <-chan lifecyclehook.Issue) lifecyclehook.Issue {
	t.Helper()
	issue, ok := <-issues
	if !ok {
		t.Fatal("lifecycle issue stream closed before reporting the failure")
	}
	return issue
}
