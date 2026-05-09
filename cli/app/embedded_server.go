package app

import (
	"context"
	"errors"
	"io"
	"strings"

	"builder/cli/app/internal/embeddedbinding"
	"builder/cli/app/internal/embeddedstartup"
	"builder/cli/app/internal/statuscollect"
	"builder/shared/client"
	"builder/shared/config"
)

type appServerCore interface {
	Close() error
	OwnsServer() bool
	Config() config.App
}

type embeddedAppServer struct {
	inner              *embeddedstartup.Server
	boundProjectID     string
	boundSessionLaunch client.SessionLaunchClient
	boundRunPrompt     client.RunPromptClient
}

func newEmbeddedAppServer(inner *embeddedstartup.Server) *embeddedAppServer {
	if inner == nil {
		return nil
	}
	return &embeddedAppServer{inner: inner}
}

func (s *embeddedAppServer) Close() error {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.Close()
}

func (s *embeddedAppServer) OwnsServer() bool {
	return s != nil && s.inner != nil
}

func (s *embeddedAppServer) Config() config.App {
	if s == nil || s.inner == nil {
		return config.App{}
	}
	return s.inner.Config()
}

func (s *embeddedAppServer) BindProjectWorkspace(ctx context.Context, projectID string, workspaceID string) (interactiveSessionServer, error) {
	if s == nil {
		_, err := embeddedbinding.BindProjectWorkspace(ctx, embeddedbinding.Request{ProjectID: projectID, WorkspaceID: workspaceID})
		return nil, err
	}
	bound, err := embeddedbinding.BindProjectWorkspace(ctx, embeddedbinding.Request{
		Server:      s.inner,
		ProjectID:   projectID,
		WorkspaceID: workspaceID,
	})
	if err != nil {
		return nil, err
	}
	return &embeddedAppServer{
		inner:              s.inner,
		boundProjectID:     bound.ProjectID,
		boundSessionLaunch: bound.SessionLaunch,
		boundRunPrompt:     bound.RunPrompt,
	}, nil
}

func (s *embeddedAppServer) AuthStateResolver() statuscollect.AuthStateResolver {
	if s == nil || s.inner == nil {
		return nil
	}
	return statuscollect.NormalizeAuthStateResolver(s.inner.AuthManager())
}

func (s *embeddedAppServer) AuthStatePath() string {
	if s == nil || s.inner == nil || s.inner.AuthManager() == nil {
		return ""
	}
	return config.GlobalAuthConfigPath(s.Config())
}

func (s *embeddedAppServer) AuthStatusClient() client.AuthStatusClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.AuthStatusClient()
}

func (s *embeddedAppServer) ProjectID() string {
	if s == nil {
		return ""
	}
	if trimmed := strings.TrimSpace(s.boundProjectID); trimmed != "" {
		return trimmed
	}
	if s.inner == nil {
		return ""
	}
	return s.inner.ProjectID()
}

func (s *embeddedAppServer) ProjectViewClient() client.ProjectViewClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.ProjectViewClient()
}

func (s *embeddedAppServer) AskViewClient() client.AskViewClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.AskViewClient()
}

func (s *embeddedAppServer) ApprovalViewClient() client.ApprovalViewClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.ApprovalViewClient()
}

func (s *embeddedAppServer) PromptControlClient() client.PromptControlClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.PromptControlClient()
}

func (s *embeddedAppServer) PromptActivityClient() client.PromptActivityClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.PromptActivityClient()
}

func (s *embeddedAppServer) RunPromptClient() client.RunPromptClient {
	if s == nil {
		return nil
	}
	if s.boundRunPrompt != nil {
		return s.boundRunPrompt
	}
	if s.inner == nil {
		return nil
	}
	return s.inner.RunPromptClient()
}

func (s *embeddedAppServer) SessionLaunchClient() client.SessionLaunchClient {
	if s == nil {
		return nil
	}
	if s.boundSessionLaunch != nil {
		return s.boundSessionLaunch
	}
	if s.inner == nil {
		return nil
	}
	return s.inner.SessionLaunchClient()
}

func (s *embeddedAppServer) SessionViewClient() client.SessionViewClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.SessionViewClient()
}

func (s *embeddedAppServer) WorktreeClient() client.WorktreeClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.WorktreeClient()
}

func (s *embeddedAppServer) SessionActivityClient() client.SessionActivityClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.SessionActivityClient()
}

func (s *embeddedAppServer) SessionRuntimeClient() client.SessionRuntimeClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.SessionRuntimeClient()
}

func (s *embeddedAppServer) SessionLifecycleClient() client.SessionLifecycleClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.SessionLifecycleClient()
}

func (s *embeddedAppServer) ProcessViewClient() client.ProcessViewClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.ProcessViewClient()
}

func (s *embeddedAppServer) ProcessControlClient() client.ProcessControlClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.ProcessControlClient()
}

func (s *embeddedAppServer) ProcessOutputClient() client.ProcessOutputClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.ProcessOutputClient()
}

func (s *embeddedAppServer) RuntimeControlClient() client.RuntimeControlClient {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.RuntimeControlClient()
}

func (s *embeddedAppServer) ContainerDir() string {
	if s == nil || s.inner == nil {
		return ""
	}
	return s.inner.ContainerDir()
}

func (s *embeddedAppServer) PrepareRuntime(ctx context.Context, plan sessionLaunchPlan, diagnosticWriter io.Writer, startLogLine string) (*runtimeLaunchPlan, error) {
	if s == nil || s.inner == nil {
		return nil, errors.New("embedded server is required")
	}
	return prepareSharedRuntime(ctx, s, plan, diagnosticWriter, startLogLine)
}

func (s *embeddedAppServer) Reauthenticate(ctx context.Context, interactor authInteractor) error {
	if s == nil || s.inner == nil {
		return errors.New("embedded server is required")
	}
	cfg := s.inner.Config()
	return ensureRemoteAuthReady(ctx, s.inner.AuthBootstrapClient(), cfg.Settings, interactor)
}
