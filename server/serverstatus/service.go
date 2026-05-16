package serverstatus

import (
	"context"

	"builder/server/auth"
	"builder/shared/buildinfo"
	"builder/shared/config"
	"builder/shared/protocol"
	"builder/shared/serverapi"
)

type Service struct {
	authManager *auth.Manager
	endpoint    string
}

func NewService(authManager *auth.Manager, cfg config.App) *Service {
	return &Service{authManager: authManager, endpoint: config.ServerRPCURL(cfg)}
}

func (s *Service) GetServerReadiness(ctx context.Context, _ serverapi.ServerReadinessRequest) (serverapi.ServerReadinessResponse, error) {
	authReady := false
	authRequired := true
	if s != nil && s.authManager != nil {
		state, err := s.authManager.Load(ctx)
		if err != nil {
			return serverapi.ServerReadinessResponse{}, err
		}
		authReady = auth.EvaluateStartupGate(state).Ready
	}
	ready := authReady
	response := serverapi.ServerReadinessResponse{
		Ready:           ready,
		ServerVersion:   buildinfo.Version,
		ProtocolVersion: protocol.Version,
		AuthReady:       authReady,
		AuthRequired:    authRequired,
		Endpoint:        "",
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

func (s *Service) GetServerCapabilities(context.Context, serverapi.ServerCapabilitiesRequest) (serverapi.ServerCapabilitiesResponse, error) {
	return serverapi.ServerCapabilitiesResponse{
		ServerVersion:   buildinfo.Version,
		ProtocolVersion: protocol.Version,
		Capabilities:    requiredMVPCapabilities(),
	}, nil
}

func requiredMVPCapabilities() []serverapi.ServerCapability {
	ids := []string{
		"gui.home",
		"project.key.create",
		"project.workspace.list",
		"workflow.board.selected",
		"workflow.board.groups",
		"workflow.live_updates",
		"workflow.task.source_workspace",
		"workflow.task.create",
		"workflow.task.edit_backlog",
		"workflow.task.start",
		"workflow.task.interrupt",
		"workflow.task.resume",
		"workflow.task.cancel",
		"workflow.attention.list",
		"workflow.task.activity",
		"workflow.task.comments",
		"workflow.task.teleport",
	}
	out := make([]serverapi.ServerCapability, 0, len(ids))
	for _, id := range ids {
		out = append(out, serverapi.ServerCapability{ID: id, Available: true, RequiredForMVP: true})
	}
	return out
}
