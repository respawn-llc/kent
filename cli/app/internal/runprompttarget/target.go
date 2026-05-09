package runprompttarget

import (
	"context"
	"strings"

	"builder/cli/app/internal/remoteattach"
	"builder/cli/app/internal/targetstartup"
	"builder/shared/client"
	"builder/shared/config"
)

type Target struct {
	Client    client.RunPromptClient
	Auth      client.AuthBootstrapClient
	ProjectID func() string
}

type ValidateRequest struct {
	Target          Target
	Config          config.App
	EnsureAuthReady func(context.Context, client.AuthBootstrapClient) error
}

func Remote(remote *client.Remote, cfg config.App) targetstartup.Target[Target] {
	return RemoteWithClose(remote, cfg, remote.Close)
}

func RemoteWithClose(remote *client.Remote, _ config.App, closeFn func() error) targetstartup.Target[Target] {
	return targetstartup.Target[Target]{
		Value: Target{
			Client:    remote,
			Auth:      remote,
			ProjectID: remote.ProjectID,
		},
		Close: closeFn,
	}
}

func Embedded(runPrompt client.RunPromptClient, projectID func() string, closeFn func() error) targetstartup.Target[Target] {
	return targetstartup.Target[Target]{
		Value: Target{
			Client:    runPrompt,
			ProjectID: projectID,
		},
		Close: closeFn,
	}
}

func Validate(ctx context.Context, req ValidateRequest) error {
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
