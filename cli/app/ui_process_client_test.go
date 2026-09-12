package app

import processpb "core/shared/protoapi/gen/kent/api/process"
import (
	"context"
	"errors"
	"testing"
	"time"

	"core/server/processview"
	shelltool "core/server/tools/shell"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
)

type fixedUIProcessClient struct {
	entries []*processpb.BackgroundProcess
}

const uiProcessTestProjectID = "project-1"

func withUIBackgroundManagerForTest(manager *shelltool.Manager) UIOption {
	return func(m *uiModelConstruction) {
		if manager == nil || m.processClientExplicit {
			return
		}
		processes := processview.NewProcessViewService(manager, allUIProjectSessionMembership{})
		m.processClient = newUIProcessClientWithReads(
			uiProcessTestProjectID,
			processes,
			processes)
	}
}

type stubProcessViewService struct {
	listResp *processpb.ListSuccess
	err      error
}

type stubProcessControlService struct {
	inlineResp *processpb.InlineOutputSuccess
	err        error
	killedReq  *processpb.KillRequest
	killed     []string
}

func (s *stubProcessViewService) ListProcesses(context.Context, *processpb.ListRequest) (*processpb.ListSuccess, error) {
	if s.err != nil {
		return &processpb.ListSuccess{}, s.err
	}
	return s.listResp, nil
}

func (s *stubProcessViewService) GetProcess(context.Context, *processpb.GetRequest) (*processpb.GetSuccess, error) {
	if s.err != nil {
		return &processpb.GetSuccess{}, s.err
	}
	return &processpb.GetSuccess{}, nil
}

func (s *stubProcessControlService) KillProcess(_ context.Context, req *processpb.KillRequest) (*emptypb.Empty, error) {
	if s.err != nil {
		return &emptypb.Empty{}, s.err
	}
	s.killedReq = req
	s.killed = append(s.killed, req.ProcessId)
	return &emptypb.Empty{}, nil
}

func (s *stubProcessControlService) GetInlineOutput(context.Context, *processpb.InlineOutputRequest) (*processpb.InlineOutputSuccess, error) {
	if s.err != nil {
		return &processpb.InlineOutputSuccess{}, s.err
	}
	return s.inlineResp, nil
}

func (c fixedUIProcessClient) ListProcesses(context.Context) ([]*processpb.BackgroundProcess, error) {
	out := make([]*processpb.BackgroundProcess, len(c.entries))
	for i, entry := range c.entries {
		out[i] = proto.Clone(entry).(*processpb.BackgroundProcess)
	}
	return out, nil
}

func (fixedUIProcessClient) KillProcess(context.Context, string) error { return nil }

func (fixedUIProcessClient) InlineOutput(context.Context, string, int) (string, string, error) {
	return "", "", nil
}

func TestUIProcessClientProjectsManagerSnapshots(t *testing.T) {
	manager := newFastBackgroundTestManager(t)

	workdir := t.TempDir()
	res, err := manager.Start(context.Background(), shelltool.ExecRequest{
		Command:        []string{"sh", "-c", "printf 'done\n'; sleep 0.05; exit 7"},
		DisplayCommand: "project-test",
		OwnerSessionID: "session-1",
		Workdir:        workdir,
		YieldTime:      fastBackgroundTestYield})
	if err != nil {
		t.Fatalf("start background process: %v", err)
	}
	if !res.Backgrounded {
		t.Fatal("expected background process")
	}

	processes := processview.NewProcessViewService(manager, allUIProjectSessionMembership{})
	client := newUIProcessClientWithReads(
		uiProcessTestProjectID,
		processes,
		processes)
	waitForTestCondition(t, 2*time.Second, "background process to finish", func() bool {
		entries, err := client.ListProcesses(context.Background())
		if err != nil {
			return false
		}
		for _, entry := range entries {
			if entry.Id == res.SessionID {
				return !entry.Running && entry.ExitCode != nil
			}
		}
		return false
	})

	var projectedExitCode *int32
	found := false
	entries, err := client.ListProcesses(context.Background())
	if err != nil {
		t.Fatalf("ListProcesses: %v", err)
	}
	for _, entry := range entries {
		if entry.Id != res.SessionID {
			continue
		}
		found = true
		if entry.Command != "project-test" {
			t.Fatalf("command = %q, want project-test", entry.Command)
		}
		if entry.Workdir != workdir {
			t.Fatalf("workdir = %q, want %q", entry.Workdir, workdir)
		}
		if entry.LogPath == "" {
			t.Fatal("expected projected log path")
		}
		if entry.ExitCode == nil || *entry.ExitCode != 7 {
			t.Fatalf("exit code = %+v, want 7", entry.ExitCode)
		}
		projectedExitCode = entry.ExitCode
		break
	}
	if !found {
		t.Fatalf("expected projected process entry for %s", res.SessionID)
	}

	*projectedExitCode = 0
	entries, err = client.ListProcesses(context.Background())
	if err != nil {
		t.Fatalf("ListProcesses second: %v", err)
	}
	for _, entry := range entries {
		if entry.Id == res.SessionID {
			if entry.ExitCode == nil || *entry.ExitCode != 7 {
				t.Fatalf("expected projected exit code clone to remain 7, got %+v", entry.ExitCode)
			}
			return
		}
	}
	t.Fatalf("expected projected process entry for %s on second read", res.SessionID)
}

func TestExplicitUIProcessClientWinsOverBackgroundManagerOptionOrder(t *testing.T) {
	manager := newFastBackgroundTestManager(t)

	explicit := fixedUIProcessClient{entries: []*processpb.BackgroundProcess{{Id: "explicit-process"}}}

	first := newProjectedStaticUIModel(
		withUIBackgroundManagerForTest(manager),
		WithUIProcessClient(explicit))
	if got := first.listProcesses(); len(got) != 1 || got[0].Id != "explicit-process" {
		t.Fatalf("expected explicit process client to win when applied last, got %+v", got)
	}

	second := newProjectedStaticUIModel(
		WithUIProcessClient(explicit),
		withUIBackgroundManagerForTest(manager))
	if got := second.listProcesses(); len(got) != 1 || got[0].Id != "explicit-process" {
		t.Fatalf("expected explicit process client to win when applied first, got %+v", got)
	}
}

func TestUIProcessClientUsesSharedReadsWhenAvailable(t *testing.T) {
	reads := &stubProcessViewService{
		listResp: &processpb.ListSuccess{Processes: []*processpb.BackgroundProcess{{Id: "proc-1", OwnerRunId: proto.String("run-1"), OwnerStepId: proto.String("step-1")}}}}
	processClient := newUIProcessClientWithReads(uiProcessTestProjectID, reads, nil)
	got, err := processClient.ListProcesses(context.Background())
	if err != nil {
		t.Fatalf("ListProcesses: %v", err)
	}
	if len(got) != 1 || got[0].Id != "proc-1" || got[0].GetOwnerRunId() != "run-1" || got[0].GetOwnerStepId() != "step-1" {
		t.Fatalf("unexpected loopback process payload: %+v", got)
	}
}

func TestUIProcessClientDoesNotBypassSharedReadBoundaryOnError(t *testing.T) {
	manager := newFastBackgroundTestManager(t)

	workdir := t.TempDir()
	res, err := manager.Start(context.Background(), shelltool.ExecRequest{
		Command:        []string{"sh", "-c", "printf 'done\n'; sleep 0.05; exit 0"},
		DisplayCommand: "fallback-process",
		OwnerSessionID: "session-1",
		OwnerRunID:     "run-1",
		OwnerStepID:    "step-1",
		Workdir:        workdir,
		YieldTime:      fastBackgroundTestYield})
	if err != nil {
		t.Fatalf("start background process: %v", err)
	}
	if !res.Backgrounded {
		t.Fatal("expected background process")
	}

	processClient := newUIProcessClientWithReads(
		uiProcessTestProjectID,
		&stubProcessViewService{err: errors.New("boom")},
		nil)
	if got, err := processClient.ListProcesses(context.Background()); err == nil || got != nil {
		t.Fatalf("expected shared-read failure to fail closed, got entries=%+v err=%v", got, err)
	}
}

func TestUIProcessClientUsesSharedControlWhenAvailable(t *testing.T) {
	controls := &stubProcessControlService{inlineResp: &processpb.InlineOutputSuccess{Output: "hello", LogPath: "/tmp/proc.log"}}
	processClient := newUIProcessClientWithReads(uiProcessTestProjectID, nil, controls)

	preview, logPath, err := processClient.InlineOutput(context.Background(), "proc-1", 123)
	if err != nil {
		t.Fatalf("InlineOutput: %v", err)
	}
	if preview != "hello" || logPath != "/tmp/proc.log" {
		t.Fatalf("unexpected inline output payload preview=%q logPath=%q", preview, logPath)
	}
	if err := processClient.KillProcess(context.Background(), "proc-1"); err != nil {
		t.Fatalf("KillProcess: %v", err)
	}
	if len(controls.killed) != 1 || controls.killed[0] != "proc-1" {
		t.Fatalf("unexpected killed requests: %+v", controls.killed)
	}
}

func TestUIProcessClientDoesNotBypassSharedControlBoundaryOnError(t *testing.T) {
	manager := newFastBackgroundTestManager(t)

	workdir := t.TempDir()
	res, err := manager.Start(context.Background(), shelltool.ExecRequest{
		Command:        []string{"sh", "-c", "printf 'fallback-control\n'; sleep 1"},
		DisplayCommand: "fallback-control",
		Workdir:        workdir,
		YieldTime:      fastBackgroundTestYield})
	if err != nil {
		t.Fatalf("start background process: %v", err)
	}
	if !res.Backgrounded {
		t.Fatal("expected background process")
	}

	controlErr := errors.New("shared control boundary failure")
	processClient := newUIProcessClientWithReads(
		uiProcessTestProjectID,
		nil,
		&stubProcessControlService{err: controlErr})
	if _, _, err := processClient.InlineOutput(context.Background(), res.SessionID, 12_000); !errors.Is(err, controlErr) {
		t.Fatalf("expected shared control error from InlineOutput, got %v", err)
	}
	if err := processClient.KillProcess(context.Background(), res.SessionID); !errors.Is(err, controlErr) {
		t.Fatalf("expected shared control error from KillProcess, got %v", err)
	}
	for _, entry := range manager.List() {
		if entry.ID == res.SessionID && entry.KillRequested {
			t.Fatalf("expected manager fallback to stay unused, got %+v", entry)
		}
	}
}

type allUIProjectSessionMembership struct{}

func (allUIProjectSessionMembership) ListProjectSessionIDs(
	_ context.Context,
	_ string) ([]string, error) {
	return []string{"session-1"}, nil
}
