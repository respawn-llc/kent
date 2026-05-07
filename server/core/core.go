package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"builder/server/approvalview"
	"builder/server/askview"
	"builder/server/auth"
	"builder/server/authbootstrap"
	"builder/server/authstatus"
	serverbootstrap "builder/server/bootstrap"
	"builder/server/generated"
	"builder/server/launch"
	"builder/server/metadata"
	"builder/server/primaryrun"
	"builder/server/processoutput"
	"builder/server/processview"
	"builder/server/projectview"
	"builder/server/promptactivity"
	"builder/server/promptcontrol"
	"builder/server/registry"
	"builder/server/rootlock"
	"builder/server/runprompt"
	"builder/server/runtime"
	"builder/server/runtimecontrol"
	"builder/server/runtimewire"
	"builder/server/session"
	"builder/server/sessionactivity"
	"builder/server/sessionlaunch"
	"builder/server/sessionlifecycle"
	"builder/server/sessionruntime"
	"builder/server/sessionview"
	"builder/server/storagemigration"
	askquestion "builder/server/tools/askquestion"
	shelltool "builder/server/tools/shell"
	"builder/server/updatestatus"
	"builder/server/worktree"
	"builder/shared/buildinfo"
	"builder/shared/client"
	"builder/shared/clientui"
	"builder/shared/config"
	"builder/shared/protocol"
	"builder/shared/serverapi"
)

type Core struct {
	cfg              config.App
	containerDir     string
	oauthOpts        serverbootstrap.AuthSupport
	fastModeState    *runtime.FastModeState
	background       *shelltool.Manager
	backgroundRouter *runtimewire.BackgroundEventRouter
	rootLock         *rootlock.Lease
	metadataStore    *metadata.Store
	runtimeRegistry  *registry.RuntimeRegistry
	sessionStores    *registry.SessionStoreRegistry
	sessionLaunchMu  sync.Mutex
	sessionLaunchMap map[string]client.SessionLaunchClient
	runPromptMu      sync.Mutex
	runPromptMap     map[string]client.RunPromptClient
	projectID        string
	projectViews     client.ProjectViewClient
	authBootstrap    client.AuthBootstrapClient
	authStatus       client.AuthStatusClient
	askViews         client.AskViewClient
	approvalViews    client.ApprovalViewClient
	processControls  client.ProcessControlClient
	processOutput    client.ProcessOutputClient
	processViews     client.ProcessViewClient
	promptControl    client.PromptControlClient
	promptActivity   client.PromptActivityClient
	runtimeControls  client.RuntimeControlClient
	sessionLaunch    client.SessionLaunchClient
	sessionRuntime   client.SessionRuntimeClient
	sessionViews     client.SessionViewClient
	sessionLifecycle client.SessionLifecycleClient
	sessionActivity  client.SessionActivityClient
	worktrees        client.WorktreeClient
	runPrompt        client.RunPromptClient
	updateStatus     *updatestatus.Service
}

type unregisteredSessionLaunchClient struct{}

func (unregisteredSessionLaunchClient) PlanSession(context.Context, serverapi.SessionPlanRequest) (serverapi.SessionPlanResponse, error) {
	return serverapi.SessionPlanResponse{}, serverapi.ErrWorkspaceNotRegistered
}

type unregisteredRunPromptClient struct{}

func (unregisteredRunPromptClient) RunPrompt(context.Context, serverapi.RunPromptRequest, serverapi.RunPromptProgressSink) (serverapi.RunPromptResponse, error) {
	return serverapi.RunPromptResponse{}, serverapi.ErrWorkspaceNotRegistered
}

func New(cfg config.App, authSupport serverbootstrap.AuthSupport, runtimeSupport serverbootstrap.RuntimeSupport) (*Core, error) {
	return NewWithContext(context.Background(), cfg, authSupport, runtimeSupport)
}

func NewWithContext(ctx context.Context, cfg config.App, authSupport serverbootstrap.AuthSupport, runtimeSupport serverbootstrap.RuntimeSupport) (*Core, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rootLease, err := rootlock.Acquire(cfg.PersistenceRoot)
	if err != nil {
		return nil, err
	}
	generatedSupport, err := serverbootstrap.BuildGeneratedSupport(ctx)
	if err != nil {
		_ = rootLease.Close()
		return nil, err
	}
	runtimeSupport.Generated = generatedSupport
	if err := storagemigration.EnsureProjectV1(context.Background(), cfg.PersistenceRoot, nil); err != nil {
		_ = rootLease.Close()
		return nil, err
	}
	containerDir := ""
	if strings.TrimSpace(cfg.WorkspaceRoot) != "" {
		_, resolvedContainerDir, err := config.ResolveWorkspaceContainer(cfg)
		if err != nil {
			_ = rootLease.Close()
			return nil, err
		}
		containerDir = resolvedContainerDir
	}
	metadataStore, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		_ = rootLease.Close()
		return nil, err
	}
	if authSupport.AuthManager == nil {
		_ = rootLease.Close()
		_ = metadataStore.Close()
		return nil, fmt.Errorf("auth manager is required")
	}
	if runtimeSupport.Background == nil {
		_ = rootLease.Close()
		_ = metadataStore.Close()
		return nil, fmt.Errorf("background manager is required")
	}
	storeOptions := metadataStore.AuthoritativeSessionStoreOptions()
	runtimeRegistry := registry.NewRuntimeRegistry()
	sessionStoreRegistry := registry.NewSessionStoreRegistry()
	projectService, err := projectview.NewMetadataService(metadataStore, "", "")
	if err != nil {
		_ = rootLease.Close()
		_ = metadataStore.Close()
		return nil, err
	}
	askService := askview.NewService(runtimeRegistry)
	approvalService := approvalview.NewService(runtimeRegistry)
	processService := processview.NewService(runtimeSupport.Background)
	processOutputService := processoutput.NewService(runtimeSupport.Background, runtimeSupport.Background)
	sessionRuntimeService := sessionruntime.NewService(cfg.PersistenceRoot, metadataStore, authSupport.AuthManager, runtimeSupport.FastModeState, runtimeSupport.Background, runtimeSupport.BackgroundRouter, runtimeRegistry, sessionStoreRegistry, storeOptions...).
		WithGeneratedRecoveredWarningProvider(func() (string, bool, error) {
			nonEmpty, err := generated.RecoveredRootNonEmpty()
			if err != nil {
				return "", false, err
			}
			if !nonEmpty {
				return "", false, nil
			}
			return generated.RecoveredWarning(), true, nil
		})
	promptControlService := promptcontrol.NewService(runtimeRegistry).WithControllerLeaseVerifier(sessionRuntimeService)
	promptActivityService := promptactivity.NewService(runtimeRegistry)
	runtimeControlService := runtimecontrol.NewService(runtimeRegistry, runtimeRegistry).WithControllerLeaseVerifier(sessionRuntimeService)
	worktreeService := worktree.NewService(metadataStore, nil, runtimeRegistry, sessionRuntimeService, runtimeSupport.Background, runtimeControlService, worktree.ServiceOptions{BaseDir: cfg.Settings.Worktrees.BaseDir, SetupScript: cfg.Settings.Worktrees.SetupScript})
	projectViews := client.NewLoopbackProjectViewClient(projectService)
	authBootstrapService := authbootstrap.NewService(authSupport.AuthManager, authSupport.OAuthOptions, protocol.AllowedPreAuthMethods())
	authStatusService := authstatus.NewService(authSupport.AuthManager)
	updateStatusService := updatestatus.NewService(buildinfo.Version)
	sessionViewService := sessionview.NewService(registry.NewGlobalPersistenceSessionResolver(cfg.PersistenceRoot, storeOptions...), runtimeRegistry, metadataStore).WithCacheWarningMode(cfg.Settings.CacheWarningMode).WithUpdateStatusProvider(updateStatusService)
	sessionLifecycleService := sessionlifecycle.NewGlobalService(cfg.PersistenceRoot, sessionStoreRegistry, authSupport.AuthManager, storeOptions...).WithControllerLeaseVerifier(sessionRuntimeService)
	sessionActivityService := sessionactivity.NewService(runtimeRegistry)
	core := &Core{
		cfg:              cfg,
		containerDir:     containerDir,
		oauthOpts:        authSupport,
		fastModeState:    runtimeSupport.FastModeState,
		background:       runtimeSupport.Background,
		backgroundRouter: runtimeSupport.BackgroundRouter,
		rootLock:         rootLease,
		metadataStore:    metadataStore,
		runtimeRegistry:  runtimeRegistry,
		sessionStores:    sessionStoreRegistry,
		sessionLaunchMap: make(map[string]client.SessionLaunchClient),
		runPromptMap:     make(map[string]client.RunPromptClient),
		projectViews:     projectViews,
		authBootstrap:    client.NewLoopbackAuthBootstrapClient(authBootstrapService),
		authStatus:       client.NewLoopbackAuthStatusClient(authStatusService),
		askViews:         client.NewLoopbackAskViewClient(askService),
		approvalViews:    client.NewLoopbackApprovalViewClient(approvalService),
		processControls:  client.NewLoopbackProcessControlClient(processService),
		processOutput:    client.NewLoopbackProcessOutputClient(processOutputService),
		processViews:     client.NewLoopbackProcessViewClient(processService),
		promptControl:    client.NewLoopbackPromptControlClient(promptControlService),
		promptActivity:   client.NewLoopbackPromptActivityClient(promptActivityService),
		runtimeControls:  client.NewLoopbackRuntimeControlClient(runtimeControlService),
		sessionLaunch:    unregisteredSessionLaunchClient{},
		sessionRuntime:   client.NewLoopbackSessionRuntimeClient(sessionRuntimeService),
		sessionViews:     client.NewLoopbackSessionViewClient(sessionViewService),
		sessionLifecycle: client.NewLoopbackSessionLifecycleClient(sessionLifecycleService),
		sessionActivity:  client.NewLoopbackSessionActivityClient(sessionActivityService),
		worktrees:        client.NewLoopbackWorktreeClient(worktreeService),
		runPrompt:        unregisteredRunPromptClient{},
		updateStatus:     updateStatusService,
	}
	if strings.TrimSpace(cfg.WorkspaceRoot) != "" {
		binding, err := metadataStore.EnsureWorkspaceBinding(context.Background(), cfg.WorkspaceRoot)
		if err != nil && !errors.Is(err, serverapi.ErrWorkspaceNotRegistered) {
			_ = rootLease.Close()
			_ = metadataStore.Close()
			return nil, err
		}
		if err == nil {
			core.projectID = binding.ProjectID
			core.sessionLaunch, err = core.SessionLaunchClientForProjectWorkspace(context.Background(), binding.ProjectID, cfg.WorkspaceRoot)
			if err != nil {
				_ = rootLease.Close()
				_ = metadataStore.Close()
				return nil, err
			}
			core.runPrompt, err = core.RunPromptClientForProjectWorkspace(context.Background(), binding.ProjectID, cfg.WorkspaceRoot)
			if err != nil {
				_ = rootLease.Close()
				_ = metadataStore.Close()
				return nil, err
			}
		}
	}
	updateStatusService.Start()
	return core, nil
}

type projectContext struct {
	config         config.App
	projectID      string
	projectRoot    string
	projectSession string
}

func (s *Core) ProjectExists(ctx context.Context, projectID string) error {
	if s == nil || s.metadataStore == nil {
		return errors.New("metadata store is required")
	}
	_, err := s.metadataStore.GetProjectOverview(ctx, strings.TrimSpace(projectID))
	return err
}

func (s *Core) SessionBelongsToProject(ctx context.Context, sessionID string, projectID string) error {
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return fmt.Errorf("session id is required")
	}
	trimmedProjectID := strings.TrimSpace(projectID)
	if trimmedProjectID == "" {
		return fmt.Errorf("project id is required")
	}
	if s == nil || s.metadataStore == nil {
		return errors.New("metadata store is required")
	}
	belongs, err := s.metadataStore.SessionBelongsToProject(ctx, trimmedSessionID, trimmedProjectID)
	if err != nil {
		return err
	}
	if !belongs {
		return fmt.Errorf("session %q not available", trimmedSessionID)
	}
	return nil
}

func (s *Core) SessionLaunchClientForProject(ctx context.Context, projectID string) (client.SessionLaunchClient, error) {
	return s.SessionLaunchClientForProjectWorkspace(ctx, projectID, s.cfg.WorkspaceRoot)
}

func (s *Core) SessionLaunchClientForProjectWorkspaceID(ctx context.Context, projectID string, workspaceID string) (client.SessionLaunchClient, error) {
	projectCtx, err := s.resolveProjectContext(ctx, projectID, workspaceID, "")
	if err != nil {
		return nil, err
	}
	return s.sessionLaunchClientForProjectContext(projectCtx), nil
}

func (s *Core) SessionLaunchClientForProjectWorkspace(ctx context.Context, projectID string, workspaceRoot string) (client.SessionLaunchClient, error) {
	projectCtx, err := s.resolveProjectContext(ctx, projectID, "", workspaceRoot)
	if err != nil {
		return nil, err
	}
	return s.sessionLaunchClientForProjectContext(projectCtx), nil
}

func (s *Core) RunPromptClientForProject(ctx context.Context, projectID string) (client.RunPromptClient, error) {
	return s.RunPromptClientForProjectWorkspace(ctx, projectID, s.cfg.WorkspaceRoot)
}

func (s *Core) RunPromptClientForProjectWorkspaceID(ctx context.Context, projectID string, workspaceID string) (client.RunPromptClient, error) {
	projectCtx, err := s.resolveProjectContext(ctx, projectID, workspaceID, "")
	if err != nil {
		return nil, err
	}
	return s.runPromptClientForProjectContext(projectCtx), nil
}

func (s *Core) RunPromptClientForProjectWorkspace(ctx context.Context, projectID string, workspaceRoot string) (client.RunPromptClient, error) {
	projectCtx, err := s.resolveProjectContext(ctx, projectID, "", workspaceRoot)
	if err != nil {
		return nil, err
	}
	return s.runPromptClientForProjectContext(projectCtx), nil
}

func (s *Core) resolveProjectContext(ctx context.Context, projectID string, workspaceID string, workspaceRoot string) (projectContext, error) {
	if s == nil || s.metadataStore == nil {
		return projectContext{}, errors.New("metadata store is required")
	}
	trimmedProjectID := strings.TrimSpace(projectID)
	if trimmedProjectID == "" {
		return projectContext{}, errors.New("project id is required")
	}
	trimmedWorkspaceID := strings.TrimSpace(workspaceID)
	if trimmedWorkspaceID != "" {
		binding, err := s.metadataStore.LookupWorkspaceBindingByID(ctx, trimmedWorkspaceID)
		if err != nil {
			return projectContext{}, err
		}
		if strings.TrimSpace(binding.ProjectID) != trimmedProjectID {
			return projectContext{}, fmt.Errorf("workspace %q is not bound to project %q", binding.CanonicalRoot, trimmedProjectID)
		}
		availability := clientui.ProjectAvailability(binding.WorkspaceStatus)
		switch availability {
		case clientui.ProjectAvailabilityMissing, clientui.ProjectAvailabilityInaccessible:
			return projectContext{}, serverapi.ProjectUnavailableError{
				ProjectID:    trimmedProjectID,
				RootPath:     binding.CanonicalRoot,
				Availability: availability,
			}
		}
		projectCfg, err := s.configForWorkspace(binding.CanonicalRoot)
		if err != nil {
			return projectContext{}, err
		}
		return projectContext{
			config:         projectCfg,
			projectID:      trimmedProjectID,
			projectRoot:    binding.CanonicalRoot,
			projectSession: config.ProjectSessionsRoot(projectCfg, trimmedProjectID),
		}, nil
	}
	trimmedWorkspaceRoot := strings.TrimSpace(workspaceRoot)
	if trimmedWorkspaceRoot != "" {
		binding, err := s.metadataStore.EnsureWorkspaceBinding(ctx, trimmedWorkspaceRoot)
		if err == nil {
			if strings.TrimSpace(binding.ProjectID) != trimmedProjectID {
				return projectContext{}, fmt.Errorf("workspace %q is not bound to project %q", binding.CanonicalRoot, trimmedProjectID)
			}
			projectCfg, err := s.configForWorkspace(binding.CanonicalRoot)
			if err != nil {
				return projectContext{}, err
			}
			return projectContext{
				config:         projectCfg,
				projectID:      trimmedProjectID,
				projectRoot:    binding.CanonicalRoot,
				projectSession: config.ProjectSessionsRoot(projectCfg, trimmedProjectID),
			}, nil
		}
		if !errors.Is(err, serverapi.ErrWorkspaceNotRegistered) {
			return projectContext{}, err
		}
	}
	overview, err := s.metadataStore.GetProjectOverview(ctx, trimmedProjectID)
	if err != nil {
		return projectContext{}, err
	}
	if strings.TrimSpace(overview.Project.RootPath) == "" {
		return projectContext{}, fmt.Errorf("project %q has no root path", trimmedProjectID)
	}
	switch overview.Project.Availability {
	case clientui.ProjectAvailabilityMissing, clientui.ProjectAvailabilityInaccessible:
		return projectContext{}, serverapi.ProjectUnavailableError{
			ProjectID:    trimmedProjectID,
			RootPath:     overview.Project.RootPath,
			Availability: overview.Project.Availability,
		}
	}
	projectCfg, err := s.configForWorkspace(overview.Project.RootPath)
	if err != nil {
		return projectContext{}, err
	}
	return projectContext{
		config:         projectCfg,
		projectID:      trimmedProjectID,
		projectRoot:    overview.Project.RootPath,
		projectSession: config.ProjectSessionsRoot(projectCfg, trimmedProjectID),
	}, nil
}

func (s *Core) configForWorkspace(workspaceRoot string) (config.App, error) {
	if s == nil {
		return config.App{}, errors.New("core is required")
	}
	if strings.TrimSpace(s.cfg.WorkspaceRoot) != "" {
		currentRoot, currentErr := config.CanonicalWorkspaceRoot(s.cfg.WorkspaceRoot)
		requestedRoot, requestedErr := config.CanonicalWorkspaceRoot(workspaceRoot)
		if currentErr == nil && requestedErr == nil && currentRoot == requestedRoot {
			projectCfg := s.cfg
			projectCfg.WorkspaceRoot = requestedRoot
			return projectCfg, nil
		}
	}
	projectCfg, err := config.Load(workspaceRoot, config.LoadOptions{})
	if err != nil {
		return config.App{}, err
	}
	projectCfg.PersistenceRoot = s.cfg.PersistenceRoot
	return projectCfg, nil
}

func (s *Core) sessionLaunchClientForProjectContext(projectCtx projectContext) client.SessionLaunchClient {
	if s == nil {
		return nil
	}
	scopeKey := projectWorkspaceScopeKey(projectCtx)
	s.sessionLaunchMu.Lock()
	defer s.sessionLaunchMu.Unlock()
	if cached := s.sessionLaunchMap[scopeKey]; cached != nil {
		return cached
	}
	service := sessionlaunch.NewService(launch.Planner{
		Config:       projectCtx.config,
		ContainerDir: projectCtx.projectSession,
		ProjectID:    projectCtx.projectID,
		StoreOptions: s.metadataStore.AuthoritativeSessionStoreOptions(),
		ReloadConfig: func() (config.App, error) {
			return s.configForWorkspace(projectCtx.projectRoot)
		},
	}, s.sessionStores)
	client := client.NewLoopbackSessionLaunchClient(service)
	s.sessionLaunchMap[scopeKey] = client
	return client
}

func (s *Core) runPromptClientForProjectContext(projectCtx projectContext) client.RunPromptClient {
	if s == nil {
		return nil
	}
	scopeKey := projectWorkspaceScopeKey(projectCtx)
	s.runPromptMu.Lock()
	defer s.runPromptMu.Unlock()
	if cached := s.runPromptMap[scopeKey]; cached != nil {
		return cached
	}
	client := runprompt.NewLoopbackRunPromptClient(runprompt.HeadlessBootstrap{
		Config:           projectCtx.config,
		ContainerDir:     projectCtx.projectSession,
		StoreOptions:     s.metadataStore.AuthoritativeSessionStoreOptions(),
		AuthManager:      s.oauthOpts.AuthManager,
		FastModeState:    s.fastModeState,
		Background:       s.background,
		RuntimeRegistry:  s.runtimeRegistry,
		BackgroundRouter: s.backgroundRouter,
	})
	s.runPromptMap[scopeKey] = client
	return client
}

func projectWorkspaceScopeKey(projectCtx projectContext) string {
	return strings.TrimSpace(projectCtx.projectID) + "\n" + strings.TrimSpace(projectCtx.config.WorkspaceRoot)
}

func (s *Core) Close() error {
	if s == nil {
		return nil
	}
	var err error
	if s.background != nil {
		err = s.background.Close()
	}
	if s.metadataStore != nil {
		err = errors.Join(err, s.metadataStore.Close())
	}
	if s.rootLock != nil {
		err = errors.Join(err, s.rootLock.Close())
	}
	return err
}

func (s *Core) Config() config.App {
	if s == nil {
		return config.App{}
	}
	return s.cfg
}

func (s *Core) ContainerDir() string {
	if s == nil {
		return ""
	}
	return s.containerDir
}

func (s *Core) MetadataStore() *metadata.Store {
	if s == nil {
		return nil
	}
	return s.metadataStore
}

func (s *Core) OAuthOptions() auth.OpenAIOAuthOptions {
	if s == nil {
		return auth.OpenAIOAuthOptions{}
	}
	return s.oauthOpts.OAuthOptions
}

func (s *Core) AuthManager() *auth.Manager {
	if s == nil {
		return nil
	}
	return s.oauthOpts.AuthManager
}

func (s *Core) FastModeState() *runtime.FastModeState {
	if s == nil {
		return nil
	}
	return s.fastModeState
}

func (s *Core) Background() *shelltool.Manager {
	if s == nil {
		return nil
	}
	return s.background
}

func (s *Core) BackgroundRouter() *runtimewire.BackgroundEventRouter {
	if s == nil {
		return nil
	}
	return s.backgroundRouter
}

func (s *Core) SessionViewClient() client.SessionViewClient {
	if s == nil {
		return nil
	}
	return s.sessionViews
}

func (s *Core) ProjectID() string {
	if s == nil {
		return ""
	}
	return s.projectID
}

func (s *Core) ProjectViewClient() client.ProjectViewClient {
	if s == nil {
		return nil
	}
	return s.projectViews
}

func (s *Core) AuthBootstrapClient() client.AuthBootstrapClient {
	if s == nil {
		return nil
	}
	return s.authBootstrap
}

func (s *Core) AuthStatusClient() client.AuthStatusClient {
	if s == nil {
		return nil
	}
	return s.authStatus
}

func (s *Core) AskViewClient() client.AskViewClient {
	if s == nil {
		return nil
	}
	return s.askViews
}

func (s *Core) ApprovalViewClient() client.ApprovalViewClient {
	if s == nil {
		return nil
	}
	return s.approvalViews
}

func (s *Core) ProcessViewClient() client.ProcessViewClient {
	if s == nil {
		return nil
	}
	return s.processViews
}

func (s *Core) RuntimeControlClient() client.RuntimeControlClient {
	if s == nil {
		return nil
	}
	return s.runtimeControls
}

func (s *Core) PromptControlClient() client.PromptControlClient {
	if s == nil {
		return nil
	}
	return s.promptControl
}

func (s *Core) PromptActivityClient() client.PromptActivityClient {
	if s == nil {
		return nil
	}
	return s.promptActivity
}

func (s *Core) ProcessControlClient() client.ProcessControlClient {
	if s == nil {
		return nil
	}
	return s.processControls
}

func (s *Core) ProcessOutputClient() client.ProcessOutputClient {
	if s == nil {
		return nil
	}
	return s.processOutput
}

func (s *Core) SessionActivityClient() client.SessionActivityClient {
	if s == nil {
		return nil
	}
	return s.sessionActivity
}

func (s *Core) SessionLaunchClient() client.SessionLaunchClient {
	if s == nil {
		return nil
	}
	return s.sessionLaunch
}

func (s *Core) SessionRuntimeClient() client.SessionRuntimeClient {
	if s == nil {
		return nil
	}
	return s.sessionRuntime
}

func (s *Core) SessionLifecycleClient() client.SessionLifecycleClient {
	if s == nil {
		return nil
	}
	return s.sessionLifecycle
}

func (s *Core) WorktreeClient() client.WorktreeClient {
	if s == nil {
		return nil
	}
	return s.worktrees
}

func (s *Core) RegisterSessionStore(store *session.Store) {
	if s == nil || s.sessionStores == nil {
		return
	}
	s.sessionStores.RegisterStore(store)
}

func (s *Core) ResolveSessionStore(sessionID string) (*session.Store, error) {
	if s == nil || s.sessionStores == nil {
		return nil, nil
	}
	return s.sessionStores.ResolveStore(context.Background(), sessionID)
}

func (s *Core) RegisterRuntime(sessionID string, engine *runtime.Engine) {
	if s == nil || s.runtimeRegistry == nil {
		return
	}
	s.runtimeRegistry.Register(sessionID, engine)
}

func (s *Core) UnregisterRuntime(sessionID string, engine *runtime.Engine) {
	if s == nil || s.runtimeRegistry == nil {
		return
	}
	s.runtimeRegistry.Unregister(sessionID, engine)
}

func (s *Core) PublishRuntimeEvent(sessionID string, evt runtime.Event) {
	if s == nil || s.runtimeRegistry == nil {
		return
	}
	s.runtimeRegistry.PublishRuntimeEvent(sessionID, evt)
}

func (s *Core) BeginPendingPrompt(sessionID string, req askquestion.Request) {
	if s == nil || s.runtimeRegistry == nil {
		return
	}
	s.runtimeRegistry.BeginPendingPrompt(sessionID, req)
}

func (s *Core) CompletePendingPrompt(sessionID string, requestID string) {
	if s == nil || s.runtimeRegistry == nil {
		return
	}
	s.runtimeRegistry.CompletePendingPrompt(sessionID, requestID)
}

func (s *Core) AwaitPromptResponse(ctx context.Context, sessionID string, req askquestion.Request) (askquestion.Response, error) {
	if s == nil || s.runtimeRegistry == nil {
		return askquestion.Response{}, fmt.Errorf("runtime registry is required")
	}
	return s.runtimeRegistry.AwaitPromptResponse(ctx, sessionID, req)
}

func (s *Core) AcquirePrimaryRun(sessionID string) (primaryrun.Lease, error) {
	if s == nil || s.runtimeRegistry == nil {
		return nil, primaryrun.ErrActivePrimaryRun
	}
	return s.runtimeRegistry.AcquirePrimaryRun(sessionID)
}

func (s *Core) RunPromptClient() client.RunPromptClient {
	if s == nil {
		return nil
	}
	return s.runPrompt
}
