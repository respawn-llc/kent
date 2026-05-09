package embeddedbinding

import (
	"context"
	"errors"
	"strings"

	"builder/shared/client"
	"builder/shared/config"
)

type Server interface {
	Config() config.App
	SessionLaunchClientForProjectWorkspace(context.Context, string, string) (client.SessionLaunchClient, error)
	SessionLaunchClientForProjectWorkspaceID(context.Context, string, string) (client.SessionLaunchClient, error)
	RunPromptClientForProjectWorkspace(context.Context, string, string) (client.RunPromptClient, error)
	RunPromptClientForProjectWorkspaceID(context.Context, string, string) (client.RunPromptClient, error)
}

type Request struct {
	Server      Server
	ProjectID   string
	WorkspaceID string
}

type Bound struct {
	ProjectID     string
	SessionLaunch client.SessionLaunchClient
	RunPrompt     client.RunPromptClient
}

func BindProjectWorkspace(ctx context.Context, req Request) (Bound, error) {
	if req.Server == nil {
		return Bound{}, errors.New("embedded server is required")
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return Bound{}, errors.New("project id is required")
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	if workspaceID != "" {
		return bindWorkspaceID(ctx, req.Server, projectID, workspaceID)
	}
	return bindWorkspaceRoot(ctx, req.Server, projectID, req.Server.Config().WorkspaceRoot)
}

func bindWorkspaceRoot(ctx context.Context, server Server, projectID string, workspaceRoot string) (Bound, error) {
	launchClient, err := server.SessionLaunchClientForProjectWorkspace(ctx, projectID, workspaceRoot)
	if err != nil {
		return Bound{}, err
	}
	runPromptClient, err := server.RunPromptClientForProjectWorkspace(ctx, projectID, workspaceRoot)
	if err != nil {
		return Bound{}, err
	}
	return Bound{ProjectID: projectID, SessionLaunch: launchClient, RunPrompt: runPromptClient}, nil
}

func bindWorkspaceID(ctx context.Context, server Server, projectID string, workspaceID string) (Bound, error) {
	launchClient, err := server.SessionLaunchClientForProjectWorkspaceID(ctx, projectID, workspaceID)
	if err != nil {
		return Bound{}, err
	}
	runPromptClient, err := server.RunPromptClientForProjectWorkspaceID(ctx, projectID, workspaceID)
	if err != nil {
		return Bound{}, err
	}
	return Bound{ProjectID: projectID, SessionLaunch: launchClient, RunPrompt: runPromptClient}, nil
}
