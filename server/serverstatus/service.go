package serverstatus

import (
	"context"

	"builder/server/auth"
	"builder/server/workflow"
	"builder/shared/buildinfo"
	"builder/shared/config"
	"builder/shared/protocol"
	"builder/shared/serverapi"
)

type Service struct {
	authManager *auth.Manager
	endpoint    string
	settings    config.Settings
}

func NewService(authManager *auth.Manager, cfg config.App) *Service {
	return &Service{authManager: authManager, endpoint: config.ServerRPCURL(cfg), settings: cfg.Settings}
}

func (s *Service) GetServerReadiness(ctx context.Context, _ serverapi.ServerReadinessRequest) (serverapi.ServerReadinessResponse, error) {
	authReady := false
	authRequired := true
	settings := config.Settings{}
	if s != nil && s.authManager != nil {
		state, err := s.authManager.Load(ctx)
		if err != nil {
			return serverapi.ServerReadinessResponse{}, err
		}
		authReady = auth.EvaluateStartupGate(state).Ready
	}
	if s != nil {
		settings = s.settings
	}
	ready := authReady
	response := serverapi.ServerReadinessResponse{
		Ready:           ready,
		ServerVersion:   buildinfo.Version,
		ServerBuild:     buildinfo.Version,
		ProtocolVersion: protocol.Version,
		AuthReady:       authReady,
		AuthRequired:    authRequired,
		Endpoint:        "",
		SubagentRoles:   subagentRoleSummaries(settings),
	}
	if s != nil {
		response.Endpoint = s.endpoint
	}
	if !ready {
		response.Causes = []serverapi.ServerReadinessCause{{
			Code:       "server_not_ready",
			Severity:   "error",
			Summary:    "Builder server is not ready.",
			NextAction: "Resolve the startup blocker and retry.",
		}}
	}
	return response, nil
}

func subagentRoleSummaries(settings config.Settings) []serverapi.SubagentRoleSummary {
	names := append([]string{workflow.DefaultAgentRole}, config.AvailableSubagentRoleNames(settings, false)...)
	roles := make([]serverapi.SubagentRoleSummary, 0, len(names))
	for _, name := range names {
		roles = append(roles, serverapi.SubagentRoleSummary{Name: name})
	}
	return roles
}
