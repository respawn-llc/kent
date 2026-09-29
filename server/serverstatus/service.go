package serverstatus

import (
	"context"

	"core/server/workflow"
	"core/shared/config"
	serverpb "core/shared/protoapi/gen/kent/api/server"
	"core/shared/protocol"

	"google.golang.org/protobuf/types/known/emptypb"
)

type ServerStatusService struct {
	endpoint string
	settings config.Settings
	updates  *UpdateStatusService
}

func NewServerStatusService(cfg config.App, updates *UpdateStatusService) *ServerStatusService {
	return &ServerStatusService{endpoint: config.ServerRPCURL(cfg), settings: cfg.Settings, updates: updates}
}

func (s *ServerStatusService) GetReadiness(ctx context.Context, _ *emptypb.Empty) (*serverpb.GetReadinessSuccess, error) {
	settings := config.Settings{}
	if s != nil {
		settings = s.settings
	}
	readiness := &serverpb.Readiness{
		Ready:           true,
		ServerVersion:   config.Version,
		ProtocolVersion: protocol.Version,
		SubagentRoles:   subagentRoleSummaries(settings),
	}
	if s != nil {
		readiness.Endpoint = s.endpoint
	}
	return &serverpb.GetReadinessSuccess{Readiness: readiness}, nil
}

func (s *ServerStatusService) GetUpdateStatus(ctx context.Context, _ *emptypb.Empty) (*serverpb.GetUpdateStatusSuccess, error) {
	if s == nil || s.updates == nil {
		return nil, ErrUpdateStatusServiceClosed
	}
	result, err := s.updates.status(ctx)
	if err != nil {
		return nil, err
	}
	return &serverpb.GetUpdateStatusSuccess{Status: result.proto()}, nil
}

func subagentRoleSummaries(settings config.Settings) []*serverpb.SubagentRoleSummary {
	names := append([]string{workflow.DefaultAgentRole}, config.AvailableSubagentRoleNames(settings, false)...)
	roles := make([]*serverpb.SubagentRoleSummary, 0, len(names))
	for _, name := range names {
		roles = append(roles, &serverpb.SubagentRoleSummary{Name: name})
	}
	return roles
}
