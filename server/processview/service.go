package processview

import (
	"context"
	"fmt"

	shelltool "core/server/tools/shell"
	processpb "core/shared/protoapi/gen/kent/api/process"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ProcessSource interface {
	List() []shelltool.Snapshot
	Snapshot(id string) (shelltool.Snapshot, error)
	Kill(id string) error
	InlineOutput(id string, maxChars int) (string, string, error)
}

type ProjectSessionMembership interface {
	ListProjectSessionIDs(ctx context.Context, projectID string) ([]string, error)
}

type ProcessViewService struct {
	processes  ProcessSource
	membership ProjectSessionMembership
}

func NewProcessViewService(processes ProcessSource, membership ProjectSessionMembership) *ProcessViewService {
	return &ProcessViewService{processes: processes, membership: membership}
}

func (s *ProcessViewService) ListProcesses(ctx context.Context, req *processpb.ListRequest) (*processpb.ListSuccess, error) {
	if s == nil || s.processes == nil {
		return nil, fmt.Errorf("process source is required")
	}
	if s.membership == nil {
		return nil, fmt.Errorf("project session membership is required")
	}
	projectSessionIDs, err := s.membership.ListProjectSessionIDs(ctx, req.ProjectId)
	if err != nil {
		return nil, err
	}
	projectSessions := make(map[string]struct{}, len(projectSessionIDs))
	for _, sessionID := range projectSessionIDs {
		projectSessions[sessionID] = struct{}{}
	}
	snapshots := s.processes.List()
	processes := make([]*processpb.BackgroundProcess, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if _, matchesProject := projectSessions[snapshot.OwnerSessionID]; !matchesProject {
			continue
		}
		if req.OwnerSessionId != nil && snapshot.OwnerSessionID != *req.OwnerSessionId {
			continue
		}
		if req.OwnerRunId != nil && snapshot.OwnerRunID != *req.OwnerRunId {
			continue
		}
		process, err := ProcessFromSnapshot(snapshot)
		if err != nil {
			return nil, err
		}
		processes = append(processes, process)
	}
	return &processpb.ListSuccess{Processes: processes}, nil
}

func (s *ProcessViewService) GetProcess(_ context.Context, req *processpb.GetRequest) (*processpb.GetSuccess, error) {
	if s == nil || s.processes == nil {
		return nil, fmt.Errorf("process source is required")
	}
	snapshot, err := s.processes.Snapshot(req.ProcessId)
	if err != nil {
		return nil, err
	}
	process, err := ProcessFromSnapshot(snapshot)
	if err != nil {
		return nil, err
	}
	return &processpb.GetSuccess{Process: process}, nil
}

func (s *ProcessViewService) KillProcess(ctx context.Context, req *processpb.KillRequest) (*emptypb.Empty, error) {
	if s == nil || s.processes == nil {
		return nil, fmt.Errorf("process source is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.processes.Kill(req.ProcessId); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (s *ProcessViewService) GetInlineOutput(_ context.Context, req *processpb.InlineOutputRequest) (*processpb.InlineOutputSuccess, error) {
	if s == nil || s.processes == nil {
		return nil, fmt.Errorf("process source is required")
	}
	output, logPath, err := s.processes.InlineOutput(req.ProcessId, int(req.MaxChars))
	if err != nil {
		return nil, err
	}
	return &processpb.InlineOutputSuccess{Output: output, LogPath: logPath}, nil
}
