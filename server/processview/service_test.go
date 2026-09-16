package processview

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"core/internal/testharness/postprocessfixture"
	"core/internal/testharness/testsetup"
	"core/server/tools"
	shelltool "core/server/tools/shell"
	"core/server/tools/shell/postprocess"
	"core/shared/config"
	processpb "core/shared/protoapi/gen/kent/api/process"
	"core/shared/toolspec"
)

const processViewTestWaitTimeout = 10 * time.Second
const processViewTestProjectID = "project-1"

func TestServiceListProcessesIncludesRunOwnership(t *testing.T) {
	fixture := newProcessViewFixture(t)
	result := fixture.startCommand(t, "call-1", "printf 'working\n'; sleep 30", "run-1", "step-1")
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
	ownerRunID := "run-1"
	resp, err := fixture.service.ListProcesses(context.Background(), &processpb.ListRequest{
		ProjectId:      processViewTestProjectID,
		OwnerSessionId: &ownerSessionID,
		OwnerRunId:     &ownerRunID,
	})
	if err != nil {
		t.Fatalf("ListProcesses: %v", err)
	}
	if len(resp.Processes) != 1 {
		t.Fatalf("expected one process, got %+v", resp.Processes)
	}
	process := resp.Processes[0]
	if process.OwnerSessionId != "session-1" || process.GetOwnerRunId() != "run-1" || process.GetOwnerStepId() != "step-1" {
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
	if got.Process == nil || got.Process.GetOwnerRunId() != "run-1" || got.Process.GetOwnerStepId() != "step-1" {
		t.Fatalf("unexpected process payload: %+v", got.Process)
	}
	if got.Process.LogPath != process.LogPath || got.Process.RecentOutput == "" {
		t.Fatalf("expected log path and output preview from get, got %+v", got.Process)
	}
}

type processViewFixture struct {
	manager *shelltool.Manager
	tool    tools.Handler
	service *ProcessViewService
}

func newProcessViewFixture(t *testing.T) processViewFixture {
	t.Helper()
	manager, err := shelltool.NewManager(shelltool.WithMinimumExecToBgTime(250 * time.Millisecond))
	if err != nil {
		t.Fatalf("new manager: %v", err)
	}
	t.Cleanup(func() { _ = manager.Close() })

	workspace := t.TempDir()
	tool := shelltool.NewExecCommandToolWithPostprocessor(workspace, 16_000, 200_000, manager, "session-1", postprocessfixture.NewRunner(t, postprocess.Settings{Mode: config.ShellPostprocessingModeBuiltin}))
	return processViewFixture{
		manager: manager,
		tool:    tool,
		service: NewProcessViewService(manager, allProjectSessionMembership{}),
	}
}

func (f processViewFixture) startCommand(t *testing.T, id string, command string, runID string, stepID string) tools.Result {
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
		RunID:  runID,
		StepID: stepID,
	})
	if err != nil {
		t.Fatalf("tool call: %v", err)
	}
	return result
}

func TestServiceListProcessesFiltersByOwnerRunID(t *testing.T) {
	fixture := newProcessViewFixture(t)
	for _, runID := range []string{"run-a", "run-b"} {
		fixture.startCommand(t, runID, "sleep 1", runID, runID+"-step")
	}

	waitForProcessCount(t, fixture.manager, 2)

	ownerRunID := "run-b"
	resp, err := fixture.service.ListProcesses(context.Background(), &processpb.ListRequest{
		ProjectId:  processViewTestProjectID,
		OwnerRunId: &ownerRunID,
	})
	if err != nil {
		t.Fatalf("ListProcesses: %v", err)
	}
	if len(resp.Processes) != 1 || resp.Processes[0].GetOwnerRunId() != "run-b" {
		t.Fatalf("unexpected filtered processes: %+v", resp.Processes)
	}
}

func TestServiceGetInlineOutputReturnsManagerPreview(t *testing.T) {
	fixture := newProcessViewFixture(t)
	result := fixture.startCommand(t, "call-inline", "printf 'inline-preview\n'; sleep 1", "run-1", "step-1")
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
	result := fixture.startCommand(t, "call-kill", "sleep 30", "run-1", "step-1")
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

type allProjectSessionMembership struct{}

func (allProjectSessionMembership) ListProjectSessionIDs(
	_ context.Context,
	_ string,
) ([]string, error) {
	return []string{"session-1"}, nil
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
