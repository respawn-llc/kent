package serverstatus

import (
	"context"

	"core/server/authservice"
	"core/server/workflow"
	"core/shared/apicontract"
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	serverpb "core/shared/protoapi/gen/kent/api/server"
	"core/shared/protocol"

	"google.golang.org/protobuf/types/known/emptypb"
)

type ServerStatusService struct {
	authBootstrap apicontract.AuthBootstrapService
	endpoint      string
	settings      config.Settings
	updates       *UpdateStatusService
}

func NewServerStatusService(authBootstrap apicontract.AuthBootstrapService, cfg config.App, updates *UpdateStatusService) *ServerStatusService {
	return &ServerStatusService{authBootstrap: authBootstrap, endpoint: config.ServerRPCURL(cfg), settings: cfg.Settings, updates: updates}
}

func (s *ServerStatusService) GetReadiness(ctx context.Context, _ *emptypb.Empty) (*serverpb.GetReadinessSuccess, error) {
	authReady := false
	settings := config.Settings{}
	if s != nil {
		settings = s.settings
	}
	authRequired := authservice.StartupAuthRequired(settings)
	if authRequired && s != nil && s.authBootstrap != nil {
		status, err := s.authBootstrap.GetBootstrapStatus(ctx, &authpb.GetBootstrapStatusRequest{ConnectionId: (*string)(settings.Connection)})
		if err != nil {
			return nil, err
		}
		authReady = status.AuthReady
	}
	ready := authReady || !authRequired
	readiness := &serverpb.Readiness{
		Ready:           ready,
		ServerVersion:   config.Version,
		ProtocolVersion: protocol.Version,
		AuthReady:       authReady,
		AuthRequired:    authRequired,
		SubagentRoles:   subagentRoleSummaries(settings),
	}
	if s != nil {
		readiness.Endpoint = s.endpoint
	}
	if !ready {
		readiness.Causes = []*serverpb.ReadinessCause{{
			Code:     "server_not_ready",
			Severity: serverpb.ReadinessSeverity_READINESS_SEVERITY_ERROR,
		}}
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
