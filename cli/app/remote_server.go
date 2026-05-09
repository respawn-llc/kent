package app

import (
	"context"
	"errors"
	"io"
	"strings"

	"builder/cli/app/internal/remotebinding"
	"builder/cli/app/internal/statuscollect"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/protocol"
)

type remoteAppServer struct {
	remote    *client.Remote
	identity  protocol.ServerIdentity
	projectID string
	cfg       config.App
	closeFn   func() error
	owns      bool
}

func newRemoteAppServer(remote *client.Remote, cfg config.App) *remoteAppServer {
	return newRemoteAppServerWithAuth(remote, cfg, nil)
}

func newRemoteAppServerWithClose(remote *client.Remote, cfg config.App, closeFn func() error) *remoteAppServer {
	return newRemoteAppServerWithAuth(remote, cfg, closeFn)
}

func newRemoteAppServerWithAuth(remote *client.Remote, cfg config.App, closeFn func() error) *remoteAppServer {
	if remote == nil {
		return nil
	}
	ownsServer := closeFn != nil
	if closeFn == nil {
		closeFn = remote.Close
	}
	return &remoteAppServer{remote: remote, identity: remote.Identity(), projectID: remote.ProjectID(), cfg: cfg, closeFn: closeFn, owns: ownsServer}
}

func (s *remoteAppServer) Close() error {
	if s == nil {
		return nil
	}
	if s.closeFn != nil {
		return s.closeFn()
	}
	if s.remote == nil {
		return nil
	}
	return s.remote.Close()
}

func (s *remoteAppServer) OwnsServer() bool {
	return s != nil && s.owns
}

func (s *remoteAppServer) Config() config.App {
	if s == nil {
		return config.App{}
	}
	return s.cfg
}

func (s *remoteAppServer) BindProjectWorkspace(ctx context.Context, projectID string, workspaceID string) (interactiveSessionServer, error) {
	if s == nil {
		_, err := remotebinding.BindProjectWorkspace(ctx, remotebinding.Request{ProjectID: projectID, WorkspaceID: workspaceID})
		return nil, err
	}
	bound, err := remotebinding.BindProjectWorkspace(ctx, remotebinding.Request{
		Current:     s.remote,
		Config:      s.cfg,
		ProjectID:   projectID,
		WorkspaceID: workspaceID,
		OwnsServer:  s.owns,
		OwnedClose:  s.closeFn,
	})
	if err != nil {
		return nil, err
	}
	return newRemoteAppServerWithAuth(bound.Remote, s.cfg, bound.CloseFn), nil
}

func (s *remoteAppServer) AuthStateResolver() statuscollect.AuthStateResolver { return nil }
func (s *remoteAppServer) AuthStatePath() string                              { return "" }

func (s *remoteAppServer) AuthStatusClient() client.AuthStatusClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) ProjectID() string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(s.projectID)
}

func (s *remoteAppServer) AskViewClient() client.AskViewClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) ApprovalViewClient() client.ApprovalViewClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) PromptControlClient() client.PromptControlClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) PromptActivityClient() client.PromptActivityClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) ProjectViewClient() client.ProjectViewClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) RunPromptClient() client.RunPromptClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) ProcessControlClient() client.ProcessControlClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) ProcessOutputClient() client.ProcessOutputClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) ProcessViewClient() client.ProcessViewClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) RuntimeControlClient() client.RuntimeControlClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) SessionActivityClient() client.SessionActivityClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) SessionLaunchClient() client.SessionLaunchClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) SessionLifecycleClient() client.SessionLifecycleClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) SessionRuntimeClient() client.SessionRuntimeClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) SessionViewClient() client.SessionViewClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) WorktreeClient() client.WorktreeClient {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) PrepareRuntime(ctx context.Context, plan sessionLaunchPlan, diagnosticWriter io.Writer, startLogLine string) (*runtimeLaunchPlan, error) {
	if s == nil || s.remote == nil {
		return nil, errors.New("remote server is required")
	}
	return prepareSharedRuntime(ctx, s, plan, diagnosticWriter, startLogLine)
}

func (s *remoteAppServer) Reauthenticate(ctx context.Context, interactor authInteractor) error {
	if s == nil || s.remote == nil {
		return errors.New("remote server is required")
	}
	return ensureRemoteAuthReady(ctx, s.remote, s.cfg.Settings, interactor)
}
