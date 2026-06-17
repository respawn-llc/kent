package remoteattach

import (
	"context"
	"errors"
	"strings"

	"core/shared/client"
	"core/shared/config"
)

type WorkspaceRootDialer func(context.Context, config.App, string, string) (*client.Remote, error)
type WorkspaceIDDialer func(context.Context, config.App, string, string) (*client.Remote, error)

type ProjectWorkspaceBindingRequest struct {
	Current           *client.Remote
	Config            config.App
	ProjectID         string
	WorkspaceID       string
	OwnsServer        bool
	OwnedClose        func() error
	DialWorkspaceRoot WorkspaceRootDialer
	DialWorkspaceID   WorkspaceIDDialer
}

type ProjectWorkspaceBinding struct {
	Remote  *client.Remote
	CloseFn func() error
}

func BindProjectWorkspace(ctx context.Context, req ProjectWorkspaceBindingRequest) (ProjectWorkspaceBinding, error) {
	if req.Current == nil {
		return ProjectWorkspaceBinding{}, errors.New("remote server is required")
	}
	projectID := strings.TrimSpace(req.ProjectID)
	if projectID == "" {
		return ProjectWorkspaceBinding{}, errors.New("project id is required")
	}
	workspaceID := strings.TrimSpace(req.WorkspaceID)
	nextRemote, err := dialRemote(ctx, req, projectID, workspaceID)
	if err != nil {
		return ProjectWorkspaceBinding{}, err
	}
	_ = req.Current.Close()
	var closeFn func() error
	if req.OwnsServer && req.OwnedClose != nil {
		closeFn = func() error {
			return errors.Join(nextRemote.Close(), req.OwnedClose())
		}
	}
	return ProjectWorkspaceBinding{Remote: nextRemote, CloseFn: closeFn}, nil
}

func dialRemote(ctx context.Context, req ProjectWorkspaceBindingRequest, projectID string, workspaceID string) (*client.Remote, error) {
	if workspaceID != "" {
		dial := req.DialWorkspaceID
		if dial == nil {
			dial = client.DialConfiguredRemoteForProjectWorkspaceID
		}
		return dial(ctx, req.Config, projectID, workspaceID)
	}
	dial := req.DialWorkspaceRoot
	if dial == nil {
		dial = client.DialConfiguredRemoteForProjectWorkspace
	}
	return dial(ctx, req.Config, projectID, req.Config.WorkspaceRoot)
}
