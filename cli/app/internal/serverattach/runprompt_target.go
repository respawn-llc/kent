package serverattach

import (
	"context"
	"strings"

	"core/cli/app/internal/remoteattach"
	"core/shared/client"
	"core/shared/config"
)

type RunPromptTarget struct {
	Client    client.RunPromptClient
	Auth      client.AuthBootstrapClient
	ProjectID func() string
}

type RunPromptValidateRequest struct {
	Target          RunPromptTarget
	Config          config.App
	EnsureAuthReady func(context.Context, client.AuthBootstrapClient) error
}

func RunPromptRemoteWithClose(remote *client.Remote, _ config.App, closeFn func() error) Target[RunPromptTarget] {
	return Target[RunPromptTarget]{
		Value: RunPromptTarget{
			Client:    remote,
			Auth:      remote,
			ProjectID: remote.ProjectID,
		},
		Close: closeFn,
	}
}

func RunPromptEmbedded(runPrompt client.RunPromptClient, projectID func() string, closeFn func() error) Target[RunPromptTarget] {
	return Target[RunPromptTarget]{
		Value: RunPromptTarget{
			Client:    runPrompt,
			ProjectID: projectID,
		},
		Close: closeFn,
	}
}

func ValidateRunPromptTarget(ctx context.Context, req RunPromptValidateRequest) error {
	if req.Target.Auth != nil && req.EnsureAuthReady != nil {
		if err := req.EnsureAuthReady(ctx, req.Target.Auth); err != nil {
			return err
		}
	}
	if req.Target.ProjectID == nil || strings.TrimSpace(req.Target.ProjectID()) == "" {
		return remoteattach.HeadlessWorkspaceRegistrationError(req.Config.WorkspaceRoot)
	}
	return nil
}
