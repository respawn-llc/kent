package processview

import (
	"context"
	"fmt"
	"sync"

	shelltool "core/server/tools/shell"
	processpb "core/shared/protoapi/gen/kent/api/process"
	"google.golang.org/protobuf/types/known/emptypb"
)

type ProjectSessionMembership interface {
	ListProjectSessionIDs(ctx context.Context, projectID string) ([]string, error)
}

type ProcessViewService struct {
	processes  *shelltool.Manager
	membership ProjectSessionMembership
	mu         sync.Mutex
	observers  map[*processObservation]struct{}
}

func NewProcessViewService(processes *shelltool.Manager, membership ProjectSessionMembership) *ProcessViewService {
	return &ProcessViewService{processes: processes, membership: membership, observers: make(map[*processObservation]struct{})}
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
	return s.listProcesses(projectSessions, req.OwnerSessionId, req.OwnerStepId)
}

func (s *ProcessViewService) listSessionProcesses(sessionID string) (*processpb.ListSuccess, error) {
	if s == nil || s.processes == nil {
		return nil, fmt.Errorf("process source is required")
	}
	return s.listProcesses(map[string]struct{}{sessionID: {}}, nil, nil)
}

func (s *ProcessViewService) listProcesses(
	sessionIDs map[string]struct{},
	ownerSessionID *string,
	ownerStepID *string,
) (*processpb.ListSuccess, error) {
	snapshots := s.processes.List()
	processes := make([]*processpb.BackgroundProcess, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if !snapshot.Backgrounded {
			continue
		}
		if _, inScope := sessionIDs[snapshot.OwnerSessionID]; !inScope {
			continue
		}
		if ownerSessionID != nil && snapshot.OwnerSessionID != *ownerSessionID {
			continue
		}
		if ownerStepID != nil && snapshot.OwnerStepID != *ownerStepID {
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
