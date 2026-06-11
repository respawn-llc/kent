package core

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"builder/server/approvalview"
	"builder/server/askview"
	"builder/server/authbootstrap"
	"builder/server/authstatus"
	serverbootstrap "builder/server/bootstrap"
	"builder/server/generated"
	"builder/server/metadata"
	"builder/server/processoutput"
	"builder/server/processview"
	"builder/server/projectview"
	"builder/server/promptactivity"
	"builder/server/promptcontrol"
	"builder/server/registry"
	"builder/server/rootlock"
	"builder/server/runtime"
	"builder/server/runtimecontrol"
	"builder/server/serverstatus"
	"builder/server/sessionactivity"
	"builder/server/sessionlifecycle"
	"builder/server/sessionruntime"
	"builder/server/sessionview"
	"builder/server/sleepguard"
	"builder/server/updatestatus"
	"builder/server/workflow"
	"builder/server/workflowrunner"
	"builder/server/workflowscheduler"
	"builder/server/workflowstore"
	"builder/server/workflowsvc"
	"builder/server/workflowview"
	"builder/server/worktree"
	"builder/shared/buildinfo"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/rpccontract"
	"builder/shared/serverapi"
)

func New(cfg config.App, authSupport serverbootstrap.AuthSupport, runtimeSupport serverbootstrap.RuntimeSupport) (*Core, error) {
	return NewWithContext(context.Background(), cfg, authSupport, runtimeSupport)
}

func NewWithContext(ctx context.Context, cfg config.App, authSupport serverbootstrap.AuthSupport, runtimeSupport serverbootstrap.RuntimeSupport) (*Core, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	rootLease, err := rootlock.Acquire(cfg.PersistenceRoot)
	if err != nil {
		return nil, fmt.Errorf("persistence bundle: root lock: %w", err)
	}
	generatedSupport, err := serverbootstrap.BuildGeneratedSupport(ctx)
	if err != nil {
		_ = rootLease.Close()
		return nil, fmt.Errorf("persistence bundle: generated support: %w", err)
	}
	runtimeSupport.Generated = generatedSupport
	containerDir := ""
	metadataStore, err := metadata.Open(cfg.PersistenceRoot)
	if err != nil {
		_ = rootLease.Close()
		return nil, fmt.Errorf("persistence bundle: metadata store: %w", err)
	}
	if err := validateAuthBundleSupport(authSupport); err != nil {
		_ = rootLease.Close()
		_ = metadataStore.Close()
		return nil, err
	}
	if err := validateRuntimeBundleSupport(runtimeSupport); err != nil {
		_ = rootLease.Close()
		_ = metadataStore.Close()
		return nil, err
	}
	storeOptions := metadataStore.AuthoritativeSessionStoreOptions()
	runtimeRegistry := registry.NewRuntimeRegistry()
	sleepManager, _ := sleepguard.NewManager(cfg.Settings.PreventSleep, func(sessionID string, err error) {
		if strings.TrimSpace(sessionID) == "" {
			return
		}
		runtimeRegistry.PublishRuntimeEvent(sessionID, runtime.Event{
			Kind:  runtime.EventSleepGuardFailed,
			Error: err.Error(),
		})
	})
	runtimeRegistry.SetSleepObserver(sleepManager.OnRunStateChanged)
	sessionStoreRegistry := registry.NewSessionStoreRegistry()
	projectService, err := projectview.NewMetadataService(metadataStore, "")
	if err != nil {
		_ = rootLease.Close()
		_ = metadataStore.Close()
		return nil, fmt.Errorf("projects bundle: metadata service: %w", err)
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
	authBootstrapService := authbootstrap.NewService(authSupport.AuthManager, authSupport.OAuthOptions, cfg.Settings, rpccontract.AllowedPreAuthMethods())
	authStatusService := authstatus.NewService(authSupport.AuthManager, cfg.Settings)
	serverStatusService := serverstatus.NewService(authSupport.AuthManager, cfg)
	updateStatusService := updatestatus.NewService(buildinfo.Version)
	sessionViewService := sessionview.NewService(registry.NewGlobalPersistenceSessionResolver(cfg.PersistenceRoot, storeOptions...), runtimeRegistry, metadataStore).WithCacheWarningMode(cfg.Settings.CacheWarningMode).WithUpdateStatusProvider(updateStatusService)
	sessionLifecycleService := sessionlifecycle.NewGlobalService(cfg.PersistenceRoot, sessionStoreRegistry, authSupport.AuthManager, storeOptions...).WithControllerLeaseVerifier(sessionRuntimeService)
	sessionActivityService := sessionactivity.NewService(runtimeRegistry)
	var workflowRuntimeStarter *workflowrunner.Starter
	var workflowScheduler *workflowscheduler.Service
	cleanupNewFailure := func() {
		sleepManager.Close()
		if workflowScheduler != nil {
			_ = workflowScheduler.Close()
		}
		if workflowRuntimeStarter != nil {
			_ = workflowRuntimeStarter.Close()
		}
		_ = rootLease.Close()
		_ = metadataStore.Close()
		if runtimeSupport.Background != nil {
			_ = runtimeSupport.Background.Close()
		}
	}
	workflowRoleResolver := configRoleResolver{settings: cfg.Settings}
	workflowStore, err := workflowstore.New(metadataStore, workflowstore.WithRoleResolver(workflowRoleResolver))
	if err != nil {
		cleanupNewFailure()
		return nil, fmt.Errorf("workflow bundle: store: %w", err)
	}
	workflowViewService, err := workflowview.New(metadataStore, workflowview.WithSessionTranscriptProvider(sessionViewService))
	if err != nil {
		cleanupNewFailure()
		return nil, fmt.Errorf("workflow bundle: view: %w", err)
	}
	workflowRuntimeStarter, err = workflowrunner.NewStarter(cfg, metadataStore, workflowStore, authSupport.AuthManager, runtimeSupport.Background, runtimeSupport.BackgroundRouter, runtimeRegistry, workflowrunner.StarterOptions{Worktrees: taskWorktreeEnsurer{service: worktreeService}})
	if err != nil {
		cleanupNewFailure()
		return nil, fmt.Errorf("workflow bundle: runtime starter: %w", err)
	}
	workflowScheduler, err = workflowscheduler.New(workflowStore, workflowRuntimeStarter, workflowscheduler.Config{Concurrency: cfg.Settings.Workflow.Concurrency}, workflowscheduler.WithPendingAskResolver(runtimePendingAskResolver{prompts: runtimeRegistry}))
	if err != nil {
		cleanupNewFailure()
		return nil, fmt.Errorf("workflow bundle: scheduler: %w", err)
	}
	workflowRuntimeStarter.SetRuntimeFinished(workflowScheduler.RuntimeFinished)
	workflowService, err := workflowsvc.New(workflowStore, workflowViewService, workflowRoleResolver, workflowsvc.WithTaskWorktreeEnsurer(taskWorktreeEnsurer{service: worktreeService}), workflowsvc.WithTaskRuntimeCanceler(workflowRuntimeStarter), workflowsvc.WithSchedulerNotifier(workflowScheduler), workflowsvc.WithPromptResponder(runtimeRegistry))
	if err != nil {
		cleanupNewFailure()
		return nil, fmt.Errorf("workflow bundle: service: %w", err)
	}
	core := &Core{bundles: composeBundles(bundleCompositionInput{
		cfg:                     cfg,
		containerDir:            containerDir,
		authSupport:             authSupport,
		runtimeSupport:          runtimeSupport,
		rootLease:               rootLease,
		metadataStore:           metadataStore,
		sessionStoreRegistry:    sessionStoreRegistry,
		runtimeRegistry:         runtimeRegistry,
		projectViews:            projectViews,
		authBootstrapService:    authBootstrapService,
		authStatusService:       authStatusService,
		askService:              askService,
		approvalService:         approvalService,
		processService:          processService,
		processOutputService:    processOutputService,
		promptControlService:    promptControlService,
		promptActivityService:   promptActivityService,
		runtimeControlService:   runtimeControlService,
		serverStatusService:     serverStatusService,
		sessionRuntimeService:   sessionRuntimeService,
		sessionViewService:      sessionViewService,
		sessionLifecycleService: sessionLifecycleService,
		sessionActivityService:  sessionActivityService,
		updateStatusService:     updateStatusService,
		workflowService:         workflowService,
		workflowScheduler:       workflowScheduler,
		workflowRuntimeStarter:  workflowRuntimeStarter,
		worktreeService:         worktreeService,
		sleepManager:            sleepManager,
	})}
	if strings.TrimSpace(cfg.WorkspaceRoot) != "" {
		binding, err := metadataStore.EnsureWorkspaceBinding(context.Background(), cfg.WorkspaceRoot)
		if err != nil && !errors.Is(err, serverapi.ErrWorkspaceNotRegistered) {
			_ = core.Close()
			return nil, fmt.Errorf("projects bundle: workspace binding: %w", err)
		}
		if err == nil {
			core.bundles.Projects.projectID = binding.ProjectID
			core.bundles.Projects.containerDir = config.ProjectSessionsRoot(cfg, binding.ProjectID)
			if err := os.MkdirAll(core.bundles.Projects.containerDir, 0o755); err != nil {
				_ = core.Close()
				return nil, fmt.Errorf("projects bundle: sessions root: %w", err)
			}
			core.bundles.Sessions.sessionLaunch, err = core.SessionLaunchClientForProjectWorkspace(context.Background(), binding.ProjectID, cfg.WorkspaceRoot)
			if err != nil {
				_ = core.Close()
				return nil, fmt.Errorf("sessions bundle: session launch client: %w", err)
			}
			core.bundles.Sessions.runPrompt, err = core.RunPromptClientForProjectWorkspace(context.Background(), binding.ProjectID, cfg.WorkspaceRoot)
			if err != nil {
				_ = core.Close()
				return nil, fmt.Errorf("sessions bundle: run prompt client: %w", err)
			}
		}
	}
	if err := workflowScheduler.Start(context.Background()); err != nil {
		_ = core.Close()
		return nil, fmt.Errorf("workflow bundle: scheduler start: %w", err)
	}
	updateStatusService.Start()
	return core, nil
}

type taskWorktreeEnsurer struct {
	service *worktree.Service
}

func (e taskWorktreeEnsurer) EnsureTaskWorktree(ctx context.Context, taskID string) error {
	if e.service == nil {
		return nil
	}
	_, err := e.service.EnsureTaskWorktree(ctx, worktree.EnsureTaskWorktreeRequest{TaskID: taskID})
	return err
}

type runtimePendingAskResolver struct {
	prompts interface {
		ListPendingPrompts(sessionID string) []registry.PendingPromptSnapshot
	}
}

func (r runtimePendingAskResolver) CanRehydrate(_ context.Context, sessionID string, _ workflow.RunID, askID string) (bool, error) {
	if r.prompts == nil || strings.TrimSpace(sessionID) == "" || strings.TrimSpace(askID) == "" {
		return false, nil
	}
	for _, item := range r.prompts.ListPendingPrompts(sessionID) {
		if item.Request.ID == askID && !item.Request.Approval {
			return true, nil
		}
	}
	return false, nil
}
