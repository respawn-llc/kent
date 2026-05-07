package app

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	"builder/server/auth"
	serverembedded "builder/server/embedded"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/transcriptdiag"
	"github.com/google/uuid"
)

const runtimeReleaseTimeout = 3 * time.Second

type embeddedServer interface {
	Close() error
	OwnsServer() bool
	Config() config.App
	BindProject(ctx context.Context, projectID string) (embeddedServer, error)
	BindProjectWorkspace(ctx context.Context, projectID string, workspaceID string) (embeddedServer, error)
	AuthManager() *auth.Manager
	AuthStatusClient() client.AuthStatusClient
	ProjectID() string
	ApprovalViewClient() client.ApprovalViewClient
	AskViewClient() client.AskViewClient
	PromptControlClient() client.PromptControlClient
	PromptActivityClient() client.PromptActivityClient
	ProjectViewClient() client.ProjectViewClient
	RunPromptClient() client.RunPromptClient
	ProcessControlClient() client.ProcessControlClient
	ProcessOutputClient() client.ProcessOutputClient
	ProcessViewClient() client.ProcessViewClient
	RuntimeControlClient() client.RuntimeControlClient
	SessionActivityClient() client.SessionActivityClient
	SessionLaunchClient() client.SessionLaunchClient
	SessionLifecycleClient() client.SessionLifecycleClient
	SessionRuntimeClient() client.SessionRuntimeClient
	SessionViewClient() client.SessionViewClient
	WorktreeClient() client.WorktreeClient
	PrepareRuntime(ctx context.Context, plan sessionLaunchPlan, diagnosticWriter io.Writer, startLogLine string) (*runtimeLaunchPlan, error)
	Reauthenticate(ctx context.Context, interactor authInteractor) error
}

type embeddedAppServer struct {
	inner              *serverembedded.Server
	boundProjectID     string
	boundSessionLaunch client.SessionLaunchClient
	boundRunPrompt     client.RunPromptClient
}

func newEmbeddedAppServer(inner *serverembedded.Server) *embeddedAppServer {
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

func (s *embeddedAppServer) BindProject(ctx context.Context, projectID string) (embeddedServer, error) {
	return s.BindProjectWorkspace(ctx, projectID, "")
}

func (s *embeddedAppServer) BindProjectWorkspace(ctx context.Context, projectID string, workspaceID string) (embeddedServer, error) {
	if s == nil || s.inner == nil {
		return nil, errors.New("embedded server is required")
	}
	trimmedProjectID := strings.TrimSpace(projectID)
	if trimmedProjectID == "" {
		return nil, errors.New("project id is required")
	}
	trimmedWorkspaceID := strings.TrimSpace(workspaceID)
	var launchClient client.SessionLaunchClient
	var runPromptClient client.RunPromptClient
	var err error
	if trimmedWorkspaceID != "" {
		launchClient, err = s.inner.SessionLaunchClientForProjectWorkspaceID(ctx, trimmedProjectID, trimmedWorkspaceID)
		if err != nil {
			return nil, err
		}
		runPromptClient, err = s.inner.RunPromptClientForProjectWorkspaceID(ctx, trimmedProjectID, trimmedWorkspaceID)
	} else {
		launchClient, err = s.inner.SessionLaunchClientForProjectWorkspace(ctx, trimmedProjectID, s.Config().WorkspaceRoot)
		if err != nil {
			return nil, err
		}
		runPromptClient, err = s.inner.RunPromptClientForProjectWorkspace(ctx, trimmedProjectID, s.Config().WorkspaceRoot)
	}
	if err != nil {
		return nil, err
	}
	return &embeddedAppServer{
		inner:              s.inner,
		boundProjectID:     trimmedProjectID,
		boundSessionLaunch: launchClient,
		boundRunPrompt:     runPromptClient,
	}, nil
}

func (s *embeddedAppServer) AuthManager() *auth.Manager {
	if s == nil || s.inner == nil {
		return nil
	}
	return s.inner.AuthManager()
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

func (s *embeddedAppServer) OAuthOptions() auth.OpenAIOAuthOptions {
	if s == nil || s.inner == nil {
		return auth.OpenAIOAuthOptions{}
	}
	return s.inner.OAuthOptions()
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

func prepareSharedRuntime(ctx context.Context, server embeddedServer, plan sessionLaunchPlan, diagnosticWriter io.Writer, startLogLine string) (*runtimeLaunchPlan, error) {
	if server == nil {
		return nil, errors.New("server is required")
	}
	toolIDs := make([]string, 0, len(plan.EnabledTools))
	for _, id := range plan.EnabledTools {
		toolIDs = append(toolIDs, string(id))
	}
	activateReq := serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: uuid.NewString(),
		SessionID:       plan.SessionID,
		ActiveSettings:  plan.ActiveSettings,
		EnabledToolIDs:  toolIDs,
		Source:          plan.Source,
	}
	activateResp, err := server.SessionRuntimeClient().ActivateSessionRuntime(ctx, activateReq)
	if err != nil {
		return nil, err
	}
	leaseID := strings.TrimSpace(activateResp.LeaseID)
	if leaseID == "" {
		releaseSharedRuntime(server.SessionRuntimeClient(), plan.SessionID, leaseID)
		return nil, errors.New("session runtime activation returned empty controller lease id")
	}
	leaseManager := newControllerLeaseManager(leaseID)
	leaseManager.SetRecoverFunc(func(ctx context.Context) (string, error) {
		resp, err := server.SessionRuntimeClient().ActivateSessionRuntime(ctx, serverapi.SessionRuntimeActivateRequest{
			ClientRequestID: uuid.NewString(),
			SessionID:       plan.SessionID,
			ActiveSettings:  plan.ActiveSettings,
			EnabledToolIDs:  toolIDs,
			Source:          plan.Source,
		})
		if err != nil {
			return "", err
		}
		leaseID := strings.TrimSpace(resp.LeaseID)
		if leaseID == "" {
			return "", errors.New("session runtime activation returned empty controller lease id")
		}
		return leaseID, nil
	})
	sub, err := server.SessionActivityClient().SubscribeSessionActivity(ctx, serverapi.SessionActivitySubscribeRequest{SessionID: plan.SessionID})
	if err != nil {
		releaseSharedRuntime(server.SessionRuntimeClient(), plan.SessionID, leaseID)
		return nil, err
	}
	promptSub, err := server.PromptActivityClient().SubscribePromptActivity(ctx, serverapi.PromptActivitySubscribeRequest{SessionID: plan.SessionID})
	if err != nil {
		_ = sub.Close()
		releaseSharedRuntime(server.SessionRuntimeClient(), plan.SessionID, leaseID)
		return nil, err
	}
	logger := &runLogger{}
	_ = diagnosticWriter
	logger.Logf("%s", startLogLine)
	runtimeClient := newUIRuntimeClientWithReads(plan.SessionID, server.SessionViewClient(), server.RuntimeControlClient()).(*sessionRuntimeClient)
	runtimeClient.SetControllerLeaseManager(leaseManager)
	runtimeClient.SetTranscriptDiagnosticsEnabled(transcriptdiag.EnabledForProcess(plan.ActiveSettings.Debug))
	runtimeEvents, stopRuntimeEvents := startSessionActivityEvents(ctx, sub, func(ctx context.Context, afterSequence uint64) (serverapi.SessionActivitySubscription, error) {
		return server.SessionActivityClient().SubscribeSessionActivity(ctx, serverapi.SessionActivitySubscribeRequest{SessionID: plan.SessionID, AfterSequence: afterSequence})
	}, runtimeClient.transcriptDiagnosticsEnabled, func(line string) {
		logger.Logf("%s", line)
	})
	terminalFocus := newTerminalFocusState()
	turnQueueHook := newBellHooks(defaultTerminalNotifier(plan.ActiveSettings.NotificationMethod), func() string {
		if runtimeClient != nil {
			if sessionName := strings.TrimSpace(runtimeClient.MainView().Session.SessionName); sessionName != "" {
				return sessionName
			}
		}
		return strings.TrimSpace(plan.SessionName)
	}, terminalFocus.FocusedForAttention)
	askEvents, stopAskEvents := startPendingPromptEvents(ctx, promptSub, func(ctx context.Context, afterSequence uint64) (serverapi.PromptActivitySubscription, error) {
		return server.PromptActivityClient().SubscribePromptActivity(ctx, serverapi.PromptActivitySubscribeRequest{SessionID: plan.SessionID, AfterSequence: afterSequence})
	}, server.PromptControlClient(), leaseManager, turnQueueHook.OnAsk)
	wiring := &runtimeWiring{
		runtimeEvents:         runtimeEvents,
		askEvents:             askEvents,
		turnQueueHook:         turnQueueHook,
		terminalFocus:         terminalFocus,
		runtimeClient:         runtimeClient,
		promptControl:         server.PromptControlClient(),
		runtimeControls:       server.RuntimeControlClient(),
		worktrees:             server.WorktreeClient(),
		processControls:       server.ProcessControlClient(),
		processOutput:         server.ProcessOutputClient(),
		processViews:          server.ProcessViewClient(),
		approvalViews:         server.ApprovalViewClient(),
		askViews:              server.AskViewClient(),
		sessionActivity:       server.SessionActivityClient(),
		sessionViews:          server.SessionViewClient(),
		hasOtherSessions:      plan.HasOtherSessions,
		hasOtherSessionsKnown: plan.HasOtherSessionsKnown,
	}
	return &runtimeLaunchPlan{
		Logger:            logger,
		Wiring:            wiring,
		ControllerLeaseID: leaseID,
		controllerLease:   leaseManager,
		close: func() {
			stopAskEvents()
			stopRuntimeEvents()
			releaseSharedRuntime(server.SessionRuntimeClient(), plan.SessionID, leaseManager.Value())
		},
	}, nil
}

func releaseSharedRuntime(client serverapi.SessionRuntimeService, sessionID string, leaseID string) {
	if client == nil {
		return
	}
	if strings.TrimSpace(leaseID) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeReleaseTimeout)
	defer cancel()
	_, _ = client.ReleaseSessionRuntime(ctx, serverapi.SessionRuntimeReleaseRequest{ClientRequestID: uuid.NewString(), SessionID: sessionID, LeaseID: leaseID})
}

func (s *embeddedAppServer) Reauthenticate(ctx context.Context, interactor authInteractor) error {
	if s == nil || s.inner == nil {
		return errors.New("embedded server is required")
	}
	cfg := s.inner.Config()
	return ensureAuthReady(ctx, s.inner.AuthManager(), s.inner.OAuthOptions(), cfg.Settings, interactor)
}

var _ embeddedServer = (*embeddedAppServer)(nil)
