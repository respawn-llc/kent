package processview

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"core/internal/testharness/postprocessfixture"
	"core/internal/testharness/testsetup"
	"core/server/sessionstore"
	"core/server/tools"
	shelltool "core/server/tools/shell"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
	processpb "core/shared/protoapi/gen/kent/api/process"
	"core/shared/serverapi"
	"core/shared/toolspec"
	"google.golang.org/protobuf/proto"
)

const processViewTestWaitTimeout = 10 * time.Second

func TestServiceListProcessesIncludesStepOwnership(t *testing.T) {
	fixture := newProcessViewFixture(t)
	result := fixture.startCommand(t, "call-1", "printf 'working\n'; sleep 30", "step-1")
	if result.IsError {
		t.Fatalf("expected successful tool result, got %+v", result)
	}

	waitForProcessSnapshot(t, processViewTestWaitTimeout, func() (shelltool.Snapshot, bool) {
		entries := fixture.manager.List()
		if len(entries) != 1 {
			return shelltool.Snapshot{}, false
		}
		process := entries[0]
		if process.LogPath == "" || process.RecentOutput == "" {
			return shelltool.Snapshot{}, false
		}
		return process, true
	})
	ownerSessionID := "session-1"
	ownerStepID := "step-1"
	resp, err := fixture.service.ListProcesses(context.Background(), &processpb.ListRequest{
		ProjectId:      fixture.projectID,
		OwnerSessionId: &ownerSessionID,
		OwnerStepId:    &ownerStepID,
	})
	if err != nil {
		t.Fatalf("ListProcesses: %v", err)
	}
	if len(resp.Processes) != 1 {
		t.Fatalf("expected one process, got %+v", resp.Processes)
	}
	process := resp.Processes[0]
	if process.OwnerSessionId != "session-1" || process.GetOwnerStepId() != "step-1" {
		t.Fatalf("unexpected ownership: %+v", process)
	}
	if !process.Backgrounded || !process.Running {
		t.Fatalf("expected backgrounded running process, got %+v", process)
	}
	if process.LogPath == "" || process.RecentOutput == "" {
		t.Fatalf("expected log path and output preview, got %+v", process)
	}

	got, err := fixture.service.GetProcess(context.Background(), &processpb.GetRequest{ProcessId: process.Id})
	if err != nil {
		t.Fatalf("GetProcess: %v", err)
	}
	if got.Process == nil || got.Process.GetOwnerStepId() != "step-1" {
		t.Fatalf("unexpected process payload: %+v", got.Process)
	}
	if got.Process.LogPath != process.LogPath || got.Process.RecentOutput == "" {
		t.Fatalf("expected log path and output preview from get, got %+v", got.Process)
	}
}

func TestServiceListExcludesForegroundProcesses(t *testing.T) {
	fixture := newProcessViewFixture(t)
	sub, err := fixture.service.ObserveProcesses(context.Background(), &processpb.ObserveRequest{
		SessionId: "session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	if _, err := sub.Next(context.Background()); err != nil {
		t.Fatal(err)
	}
	fixture.manager.SetMinimumExecToBgTime(2 * time.Second)
	done := make(chan tools.Result, 1)
	go func() {
		done <- fixture.startCommand(t, "foreground", "sleep 1", "step-1")
	}()
	waitForProcessCount(t, fixture.manager, 1)
	list, err := fixture.service.ListProcesses(context.Background(), &processpb.ListRequest{ProjectId: fixture.projectID})
	if err != nil {
		t.Fatal(err)
	}
	if len(list.Processes) != 0 {
		t.Errorf("foreground command appeared in background list: %v", list.Processes)
	}
	<-done
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Millisecond)
	defer cancel()
	if _, err := sub.Next(ctx); err != context.DeadlineExceeded {
		t.Fatalf("foreground activity emitted update: %v", err)
	}
}

func TestObservationReplacesWithLatestCompletedListInServerOrder(t *testing.T) {
	fixture := newProcessViewFixture(t)
	sub, err := fixture.service.ObserveProcesses(context.Background(), &processpb.ObserveRequest{
		SessionId: "session-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	defer sub.Close()
	empty, err := sub.Next(context.Background())
	if err != nil || len(empty.Processes) != 0 {
		t.Fatalf("initial contents: %v (%v)", empty, err)
	}
	for _, id := range []string{"first", "second"} {
		fixture.startCommand(t, id, "sleep 30", "step-1")
	}
	initial, err := sub.Next(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(initial.Processes) != 2 {
		t.Fatalf("initial: %v", initial)
	}
	for _, process := range initial.Processes {
		if _, err := fixture.service.KillProcess(context.Background(), &processpb.KillRequest{ProcessId: process.Id}); err != nil {
			t.Fatal(err)
		}
	}
	waitForProcessSnapshot(t, processViewTestWaitTimeout, func() (shelltool.Snapshot, bool) {
		snapshots := fixture.manager.List()
		return snapshots[0], !snapshots[0].Running && !snapshots[1].Running
	})
	ctx, cancel := context.WithTimeout(context.Background(), processViewTestWaitTimeout)
	defer cancel()
	replacement, err := sub.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	current, err := fixture.service.ListProcesses(ctx, &processpb.ListRequest{
		ProjectId: fixture.projectID, OwnerSessionId: proto.String("session-1"),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(replacement, current) || len(replacement.Processes) != 2 {
		t.Fatalf("replacement %v does not match latest ordered list %v", replacement, current)
	}
}

func TestObservationInitialSessionScopeAndClose(t *testing.T) {
	fixture := newProcessViewFixture(t)
	fixture.startCommand(t, "background", "sleep 30", "step-1")
	for _, sessionID := range []string{"session-1", "session-2"} {
		sub, err := fixture.service.ObserveProcesses(context.Background(), &processpb.ObserveRequest{
			SessionId: sessionID,
		})
		if err != nil {
			t.Fatal(err)
		}
		list, err := sub.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		want := 0
		if sessionID == "session-1" {
			want = 1
		}
		if len(list.Processes) != want {
			t.Fatalf("session %s: got %v", sessionID, list)
		}
		if err := sub.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if snapshot := fixture.manager.List()[0]; !snapshot.Running {
		t.Fatal("closing observation stopped process")
	}
}

func TestObservationFollowsSessionAfterProjectMembershipChanges(t *testing.T) {
	sessionID := "session-1"
	membership := &mutableProjectSessionMembership{byProject: make(map[string][]string)}
	fixture := newProcessViewFixtureWithMembership(t, membership)
	membership.setProject(fixture.projectID, sessionID)
	if result := fixture.startCommand(t, "moved-session", "sleep 30", "step-1"); result.IsError {
		t.Fatalf("expected successful tool result, got %+v", result)
	}
	process := waitForProcessSnapshot(t, processViewTestWaitTimeout, func() (shelltool.Snapshot, bool) {
		snapshots := fixture.manager.List()
		if len(snapshots) != 1 || !snapshots[0].Backgrounded {
			return shelltool.Snapshot{}, false
		}
		return snapshots[0], true
	})

	ctx, cancel := context.WithTimeout(context.Background(), processViewTestWaitTimeout)
	defer cancel()
	openedBeforeMove, err := fixture.service.ObserveProcesses(ctx, &processpb.ObserveRequest{
		SessionId: sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer openedBeforeMove.Close()
	initial, err := openedBeforeMove.Next(ctx)
	if err != nil || len(initial.Processes) != 1 || initial.Processes[0].Id != process.ID {
		t.Fatalf("initial observation: %+v (%v)", initial, err)
	}

	membership.setProject(fixture.projectID)
	membership.setProject("project-2", sessionID)
	fixture.service.BackgroundListChanged(sessionID)
	updated, err := openedBeforeMove.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Processes) != 1 || updated.Processes[0].Id != process.ID {
		t.Errorf("open observation after Project move: %+v", updated.Processes)
	}

	openedAfterMove, err := fixture.service.ObserveProcesses(ctx, &processpb.ObserveRequest{
		SessionId: sessionID,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer openedAfterMove.Close()
	current, err := openedAfterMove.Next(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(current.Processes) != 1 || current.Processes[0].Id != process.ID {
		t.Errorf("new observation after Project move: %+v", current.Processes)
	}
}

func TestObservationBatchesOnlyMatchingChanges(t *testing.T) {
	fixture := newProcessViewFixture(t)
	synctest.Test(t, func(t *testing.T) {
		sub, err := fixture.service.ObserveProcesses(context.Background(), &processpb.ObserveRequest{
			SessionId: "session-1",
		})
		if err != nil {
			t.Fatal(err)
		}
		defer sub.Close()
		if _, err := sub.Next(context.Background()); err != nil {
			t.Fatal(err)
		}
		delivered := make(chan *processpb.ListSuccess, 4)
		go func() {
			for {
				list, err := sub.Next(context.Background())
				if err != nil {
					return
				}
				delivered <- list
			}
		}()
		synctest.Wait()
		fixture.service.BackgroundListChanged("session-2")
		time.Sleep(time.Second)
		synctest.Wait()
		if len(delivered) != 0 {
			t.Fatal("other Session or idle time emitted a list")
		}
		for range 10 {
			fixture.service.BackgroundListChanged("session-1")
		}
		synctest.Wait()
		time.Sleep(499 * time.Millisecond)
		synctest.Wait()
		if len(delivered) != 0 {
			t.Fatal("burst emitted before cadence")
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if len(delivered) != 1 {
			t.Fatalf("burst delivered %d lists", len(delivered))
		}
		fixture.service.BackgroundListChanged("session-1")
		synctest.Wait()
		time.Sleep(499 * time.Millisecond)
		synctest.Wait()
		if len(delivered) != 1 {
			t.Fatal("consecutive replacements exceeded the selected cadence")
		}
		time.Sleep(time.Millisecond)
		synctest.Wait()
		if len(delivered) != 2 {
			t.Fatal("next burst did not deliver its replacement")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if len(delivered) != 2 {
			t.Fatal("idle observation emitted a list")
		}
	})
}

func TestObservationFailsWhenReplacementCannotBeDelivered(t *testing.T) {
	fixture := newProcessViewFixture(t)
	synctest.Test(t, func(t *testing.T) {
		sub, err := fixture.service.ObserveProcesses(context.Background(), &processpb.ObserveRequest{
			SessionId: "session-1",
		})
		if err != nil {
			t.Fatal(err)
		}
		defer sub.Close()
		// Leave the initial list waiting while a replacement becomes available.
		fixture.service.BackgroundListChanged("session-1")
		synctest.Wait()
		time.Sleep(500 * time.Millisecond)
		synctest.Wait()
		if _, err := sub.Next(context.Background()); !errors.Is(err, serverapi.ErrStreamGap) {
			t.Fatalf("slow observation returned %v, want stream gap", err)
		}
	})
}

type processViewFixture struct {
	manager   *shelltool.Manager
	tool      tools.Handler
	service   *ProcessViewService
	projectID string
}

func newProcessViewFixture(t *testing.T) processViewFixture {
	return newProcessViewFixtureWithMembership(t, nil)
}

func newProcessViewFixtureWithMembership(
	t *testing.T,
	membership ProjectSessionMembership,
) processViewFixture {
	t.Helper()
	manager, err := shelltool.NewManager(t.TempDir(), shelltool.WithMinimumExecToBgTime(250*time.Millisecond))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	workspace := t.TempDir()
	root := t.TempDir()
	owner := testsetup.OpenStore(t, root)
	binding, err := owner.RegisterWorkspaceBinding(t.Context(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.ImportSessionSnapshot(t.Context(), session.PersistedStoreSnapshot{
		SessionDir: filepath.Join(root, "projects", binding.ProjectID, "sessions", "session-1"),
		Meta: session.Meta{
			SessionID: "session-1", Name: proto.String("session-1"),
			WorkspaceRoot: workspace, WorkspaceContainer: filepath.Base(workspace),
			CreatedAt: time.Now(), UpdatedAt: time.Now(),
		},
	}); err != nil {
		t.Fatal(err)
	}
	if membership == nil {
		membership = owner
	}
	tool := shelltool.NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "session-1", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	service := NewProcessViewService(manager, membership)
	manager.SetBackgroundListChangeHandler(service.BackgroundListChanged)
	return processViewFixture{
		manager:   manager,
		tool:      tool,
		service:   service,
		projectID: binding.ProjectID,
	}
}

func (f processViewFixture) startCommand(t *testing.T, id string, command string, stepID string) tools.Result {
	t.Helper()
	input, err := json.Marshal(map[string]any{
		"cmd":           command,
		"shell":         "/bin/sh",
		"login":         false,
		"yield_time_ms": 250,
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	result, err := f.tool.Call(context.Background(), tools.Call{
		ID:     id,
		Name:   toolspec.ToolExecCommand,
		Input:  input,
		StepID: stepID,
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	return result
}

func TestServiceListProcessesFiltersByOwnerStepID(t *testing.T) {
	fixture := newProcessViewFixture(t)
	for _, stepID := range []string{"step-a", "step-b"} {
		fixture.startCommand(t, stepID, "sleep 1", stepID)
	}

	waitForProcessCount(t, fixture.manager, 2)

	ownerStepID := "step-b"
	resp, err := fixture.service.ListProcesses(context.Background(), &processpb.ListRequest{
		ProjectId:   fixture.projectID,
		OwnerStepId: &ownerStepID,
	})
	if err != nil {
		t.Fatalf("ListProcesses: %v", err)
	}
	if len(resp.Processes) != 1 || resp.Processes[0].GetOwnerStepId() != "step-b" {
		t.Fatalf("unexpected filtered processes: %+v", resp.Processes)
	}
}

func TestServiceGetInlineOutputReturnsManagerPreview(t *testing.T) {
	fixture := newProcessViewFixture(t)
	result := fixture.startCommand(t, "call-inline", "printf 'inline-preview\n'; sleep 1", "step-1")
	if result.IsError {
		t.Fatalf("expected successful tool result, got %+v", result)
	}

	waitForInlineOutput(t, processViewTestWaitTimeout, func() (*processpb.InlineOutputSuccess, error) {
		return fixture.service.GetInlineOutput(context.Background(), &processpb.InlineOutputRequest{ProcessId: "1000", MaxChars: 12_000})
	}, func(resp *processpb.InlineOutputSuccess) bool {
		return resp.LogPath != "" && strings.Contains(resp.Output, "inline-preview")
	})
	resp, err := fixture.service.GetInlineOutput(context.Background(), &processpb.InlineOutputRequest{ProcessId: "1000", MaxChars: 12_000})
	if err != nil {
		t.Fatalf("GetInlineOutput: %v", err)
	}
	if resp.LogPath == "" || !strings.Contains(resp.Output, "inline-preview") {
		t.Fatalf("unexpected inline output response: %+v", resp)
	}
}

func TestServiceKillProcessHonorsCancellationAndSignalsManagerEntry(t *testing.T) {
	fixture := newProcessViewFixture(t)
	result := fixture.startCommand(t, "call-kill", "sleep 30", "step-1")
	if result.IsError {
		t.Fatalf("expected successful tool result, got %+v", result)
	}

	processes := fixture.manager.List()
	if len(processes) != 1 {
		t.Fatalf("process count = %d, want 1", len(processes))
	}
	id := processes[0].ID
	req := &processpb.KillRequest{ProcessId: id}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := fixture.service.KillProcess(ctx, req); err != context.Canceled {
		t.Fatalf("KillProcess error = %v, want context canceled", err)
	}
	snapshot, err := fixture.manager.Snapshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.KillRequested || !snapshot.Running {
		t.Fatalf("canceled request changed process state: %+v", snapshot)
	}
	if _, err := fixture.service.KillProcess(context.Background(), req); err != nil {
		t.Fatalf("KillProcess: %v", err)
	}
	waitForProcessKilled(t, fixture.manager, id)
}

func TestServiceListProcessesMembershipAndFilters(t *testing.T) {
	fixture := newProcessViewFixture(t)
	for _, stepID := range []string{"step-a", "step-b"} {
		if result := fixture.startCommand(t, stepID, "sleep 30", stepID); result.IsError {
			t.Fatalf("start: %+v", result)
		}
	}
	for _, test := range []struct {
		name    string
		project string
		session *string
		want    int
	}{
		{"members", fixture.projectID, nil, 2},
		{"foreign project", "foreign", nil, 0},
		{"session filter", fixture.projectID, new("foreign"), 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, err := fixture.service.ListProcesses(t.Context(), &processpb.ListRequest{
				ProjectId: test.project, OwnerSessionId: test.session,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Processes) != test.want {
				t.Fatalf("process count = %d, want %d", len(got.Processes), test.want)
			}
			if test.want == 2 {
				for i, snapshot := range fixture.manager.List() {
					if got.Processes[i].Id != snapshot.ID {
						t.Fatal("process ordering differs from manager")
					}
				}
			}
		})
	}
}

func TestServiceListProcessesPropagatesMembershipError(t *testing.T) {
	fixture := newProcessViewFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := fixture.service.ListProcesses(ctx, &processpb.ListRequest{ProjectId: fixture.projectID})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want canceled", err)
	}
}

type mutableProjectSessionMembership struct {
	mu        sync.RWMutex
	byProject map[string][]string
}

func (m *mutableProjectSessionMembership) FilterProjectSessionIDs(
	_ context.Context,
	projectID string,
	sessionIDs []string,
) ([]string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make([]string, 0, len(sessionIDs))
	for _, sessionID := range sessionIDs {
		for _, member := range m.byProject[projectID] {
			if sessionID == member {
				result = append(result, sessionID)
				break
			}
		}
	}
	return result, nil
}

func (m *mutableProjectSessionMembership) setProject(projectID string, sessionIDs ...string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.byProject[projectID] = append([]string{}, sessionIDs...)
}

func waitForProcessCount(t *testing.T, manager *shelltool.Manager, count int) {
	t.Helper()
	testsetup.RequireUntil(t, time.Now().Add(processViewTestWaitTimeout), 10*time.Millisecond, func() bool {
		return len(manager.List()) >= count
	}, "timed out waiting for %d processes", count)
}

func waitForProcessKilled(t *testing.T, manager *shelltool.Manager, id string) {
	t.Helper()
	testsetup.RequireUntil(t, time.Now().Add(processViewTestWaitTimeout), 10*time.Millisecond, func() bool {
		for _, entry := range manager.List() {
			if entry.ID == id && (entry.KillRequested || !entry.Running) {
				return true
			}
		}
		return false
	}, "timed out waiting for process %s to be kill-requested", id)
}

func waitForProcessSnapshot(t *testing.T, timeout time.Duration, check func() (shelltool.Snapshot, bool)) shelltool.Snapshot {
	t.Helper()
	var snapshot shelltool.Snapshot
	testsetup.RequireUntil(t, time.Now().Add(timeout), 10*time.Millisecond, func() bool {
		var ok bool
		snapshot, ok = check()
		return ok
	}, "timed out waiting for process snapshot condition")
	return snapshot
}

func waitForInlineOutput(t *testing.T, timeout time.Duration, call func() (*processpb.InlineOutputSuccess, error), match func(*processpb.InlineOutputSuccess) bool) *processpb.InlineOutputSuccess {
	t.Helper()
	var resp *processpb.InlineOutputSuccess
	testsetup.RequireUntil(t, time.Now().Add(timeout), 10*time.Millisecond, func() bool {
		var err error
		resp, err = call()
		return err == nil && match(resp)
	}, "timed out waiting for inline output")
	return resp
}
