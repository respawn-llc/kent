package app

import (
	"context"
	"core/cli/app/internal/remoteattach"
	"core/shared/apicontract"
	"core/shared/client"
	"core/shared/config"
	authpb "core/shared/protoapi/gen/kent/api/auth"
	worktreepb "core/shared/protoapi/gen/kent/api/worktree"
	"errors"

	"core/shared/protocol"
	"core/shared/theme"
)

type remoteAppServer struct {
	remote       *client.Remote
	identity     protocol.ServerIdentity
	connection   config.Connection
	local        config.LocalPreferences
	presentation startupPresentation
	retarget     *sessionWorkspaceRetargetContext
}

type startupPresentation struct {
	Theme string
}

func newRemoteAppServerWithAuth(remote *client.Remote, connection config.Connection, local config.LocalPreferences) *remoteAppServer {
	if remote == nil {
		return nil
	}
	server := &remoteAppServer{
		remote:       remote,
		identity:     remote.Identity(),
		connection:   connection,
		local:        local,
		presentation: startupPresentation{Theme: theme.Resolve(local.Theme)},
	}
	if binding, present := remote.ProjectBinding(); present {
		server.retarget = sessionWorkspaceRetargetContextFromBinding(binding, server.presentation.Theme)
	}
	return server
}

func (s *remoteAppServer) PresentationTheme() string {
	return s.presentation.Theme
}

func (s *remoteAppServer) PromptCommandCatalogClient(_ context.Context, sessionID string, _ *worktreepb.SessionExecutionTarget) (apicontract.PromptCommandCatalogService, error) {
	if s == nil {
		return nil, errors.New("remote server is required")
	}
	return s.remote.PromptCommandCatalogClientForSession(sessionID)
}

func (s *remoteAppServer) Close() error {
	if s == nil {
		return nil
	}
	if s.remote == nil {
		return nil
	}
	return s.remote.Close()
}

func (s *remoteAppServer) Connection() config.Connection {
	return s.connection
}

func (s *remoteAppServer) ClientSettings() config.ClientSettings {
	if s == nil {
		return config.ClientSettings{}
	}
	return s.local.Client
}

func (s *remoteAppServer) LocalPreferences() config.LocalPreferences { return s.local }

func (s *remoteAppServer) ProjectBinding() (client.ProjectAttachment, bool) {
	return s.remote.ProjectBinding()
}

func (s *remoteAppServer) ChatSettingsClient() apicontract.ChatSettingsService { return s.remote }

func (s *remoteAppServer) workspaceRetargetContext() *sessionWorkspaceRetargetContext {
	if s == nil || s.retarget == nil {
		return nil
	}
	copied := *s.retarget
	return &copied
}

func (s *remoteAppServer) BindProjectWorkspace(ctx context.Context, projectID string, workspaceID string) (interactiveSessionServer, error) {
	if s == nil {
		return nil, errors.New("remote server is required")
	}
	bound, err := remoteattach.BindProjectWorkspace(ctx, s.remote, s.connection, projectID, workspaceID, config.ExplicitPersistenceRootID(s.connection))
	if err != nil {
		return nil, err
	}
	binding, present := bound.ProjectBinding()
	if !present {
		closeErr := bound.Close()
		s.remote = nil
		return nil, errors.Join(errors.New("remote project attachment binding is required"), closeErr)
	}
	retargetContext := sessionWorkspaceRetargetContextFromBinding(binding, s.presentation.Theme)
	next := newRemoteAppServerWithAuth(bound, s.connection, s.local)
	next.presentation = s.presentation
	next.retarget = retargetContext
	s.remote = nil
	return next, nil
}

func (s *remoteAppServer) ReattachSession(ctx context.Context, sessionID string) error {
	if s == nil || s.remote == nil {
		return errors.New("remote server is required")
	}
	bound, err := remoteattach.BindSession(
		ctx,
		s.remote,
		s.connection,
		sessionID,
		config.ExplicitPersistenceRootID(s.connection),
	)
	if err != nil {
		return err
	}
	binding, present := bound.ProjectBinding()
	if !present {
		closeErr := bound.Close()
		s.remote = nil
		return errors.Join(errors.New("remote Session attachment binding is required"), closeErr)
	}
	s.remote = bound
	s.retarget = sessionWorkspaceRetargetContextFromBinding(binding, s.presentation.Theme)
	return nil
}

func (s *remoteAppServer) AuthStatusClient() apicontract.AuthStatusService {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) ProjectID() string {
	if s == nil || s.remote == nil {
		return ""
	}
	return s.remote.ProjectID()
}

func (s *remoteAppServer) RuntimeAttachmentClients() runtimeAttachmentClients {
	if s == nil {
		return runtimeAttachmentClients{}
	}
	return runtimeAttachmentClients{
		ProcessControls:   s.remote,
		ProcessViews:      s.remote,
		PromptControl:     s.remote,
		RuntimeControls:   s.remote,
		GoalSet:           s.remote,
		ChatSettings:      s.remote,
		SessionTranscript: s.remote,
		SessionRuntime:    s.remote,
		SessionViews:      s.remote,
		Worktrees:         s.remote,
	}
}

func (s *remoteAppServer) ProjectViewClient() apicontract.ProjectViewService {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) ServerStatusClient() apicontract.ServerStatusService {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) SessionLaunchClient() apicontract.SessionLaunchService {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) SessionLifecycleClient() apicontract.SessionLifecycleService {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) SessionViewClient() apicontract.SessionViewService {
	if s == nil {
		return nil
	}
	return s.remote
}

func (s *remoteAppServer) Reauthenticate(ctx context.Context) error {
	if s == nil || s.remote == nil {
		return errors.New("remote server is required")
	}
	catalog, err := runStartupOperation(ctx, s.PresentationTheme(), "Provider connections", "Loading connections...", func() (*authpb.ConnectionCatalog, error) {
		return s.remote.GetConnections(ctx, &authpb.GetConnectionsRequest{})
	})
	if err == nil {
		err = s.manageConnections(ctx, catalog)
	}
	if errors.Is(err, ErrAuthCanceledByUser) {
		return nil
	}
	return err
}

func (s *remoteAppServer) EnsureAuthReady(ctx context.Context, connectionID config.ConnectionID, interactor authInteractor, interactiveAuth bool) error {
	if s == nil || s.remote == nil {
		return errors.New("remote server is required")
	}
	return ensureRemoteAuthReady(ctx, s.remote, connectionID, s.PresentationTheme(), interactor, interactiveAuth)
}
