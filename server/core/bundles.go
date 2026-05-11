package core

import (
	"sync"

	"builder/server/approvalview"
	"builder/server/askview"
	"builder/server/authbootstrap"
	"builder/server/authstatus"
	serverbootstrap "builder/server/bootstrap"
	"builder/server/metadata"
	"builder/server/processoutput"
	"builder/server/processview"
	"builder/server/promptactivity"
	"builder/server/promptcontrol"
	"builder/server/registry"
	"builder/server/rootlock"
	"builder/server/runtime"
	"builder/server/runtimecontrol"
	"builder/server/runtimewire"
	"builder/server/sessionactivity"
	"builder/server/sessionlaunch"
	"builder/server/sessionlifecycle"
	"builder/server/sessionruntime"
	"builder/server/sessionview"
	shelltool "builder/server/tools/shell"
	"builder/server/updatestatus"
	"builder/server/worktree"
	"builder/shared/client"
	"builder/shared/config"
)

type Bundles struct {
	Auth        *AuthBundle
	cleanup     []lifecycleResource
	Persistence *PersistenceBundle
	Processes   *ProcessBundle
	Projects    *ProjectBundle
	Prompts     *PromptBundle
	Runtime     *RuntimeBundle
	Sessions    *SessionBundle
	Updates     *UpdateBundle
	Worktrees   *WorktreeBundle
}

type AuthBundle struct {
	support       serverbootstrap.AuthSupport
	authBootstrap client.AuthBootstrapClient
	authStatus    client.AuthStatusClient
}

type PersistenceBundle struct {
	rootLock      *rootlock.Lease
	metadataStore *metadata.Store
	sessionStores *registry.SessionStoreRegistry
}

type ProcessBundle struct {
	processControls client.ProcessControlClient
	processOutput   client.ProcessOutputClient
	processViews    client.ProcessViewClient
}

type ProjectBundle struct {
	cfg          config.App
	containerDir string
	projectID    string
	projectViews client.ProjectViewClient
}

type PromptBundle struct {
	askViews       client.AskViewClient
	approvalViews  client.ApprovalViewClient
	promptControl  client.PromptControlClient
	promptActivity client.PromptActivityClient
}

type RuntimeBundle struct {
	fastModeState    *runtime.FastModeState
	background       *shelltool.Manager
	backgroundRouter *runtimewire.BackgroundEventRouter
	runtimeRegistry  *registry.RuntimeRegistry
	runtimeControls  client.RuntimeControlClient
	sessionRuntime   client.SessionRuntimeClient
	sessionActivity  client.SessionActivityClient
}

type SessionBundle struct {
	mu               sync.Mutex
	runPromptMu      sync.Mutex
	sessionLaunchMap map[string]client.SessionLaunchClient
	sessionServices  map[string]*sessionlaunch.Service
	runPromptMap     map[string]client.RunPromptClient
	sessionLaunch    client.SessionLaunchClient
	sessionViews     client.SessionViewClient
	sessionLifecycle client.SessionLifecycleClient
	runPrompt        client.RunPromptClient
}

type UpdateBundle struct {
	updateStatus *updatestatus.Service
}

type WorktreeBundle struct {
	worktrees client.WorktreeClient
}

func (s *Core) safeBundles() *Bundles {
	if s == nil {
		return emptyBundles()
	}
	return s.bundles.withDefaults()
}

func (b *Bundles) withDefaults() *Bundles {
	if b == nil {
		return emptyBundles()
	}
	withDefaults := *b
	if withDefaults.Auth == nil {
		withDefaults.Auth = &AuthBundle{}
	}
	if withDefaults.Persistence == nil {
		withDefaults.Persistence = &PersistenceBundle{}
	}
	if withDefaults.Processes == nil {
		withDefaults.Processes = &ProcessBundle{}
	}
	if withDefaults.Projects == nil {
		withDefaults.Projects = &ProjectBundle{}
	}
	if withDefaults.Prompts == nil {
		withDefaults.Prompts = &PromptBundle{}
	}
	if withDefaults.Runtime == nil {
		withDefaults.Runtime = &RuntimeBundle{}
	}
	if withDefaults.Sessions == nil {
		withDefaults.Sessions = emptySessionBundle()
	}
	if withDefaults.Updates == nil {
		withDefaults.Updates = &UpdateBundle{}
	}
	if withDefaults.Worktrees == nil {
		withDefaults.Worktrees = &WorktreeBundle{}
	}
	return &withDefaults
}

func emptyBundles() *Bundles {
	return (&Bundles{}).withDefaults()
}

func emptySessionBundle() *SessionBundle {
	return &SessionBundle{
		sessionLaunchMap: make(map[string]client.SessionLaunchClient),
		sessionServices:  make(map[string]*sessionlaunch.Service),
		runPromptMap:     make(map[string]client.RunPromptClient),
	}
}

type bundleCompositionInput struct {
	cfg                     config.App
	containerDir            string
	authSupport             serverbootstrap.AuthSupport
	runtimeSupport          serverbootstrap.RuntimeSupport
	rootLease               *rootlock.Lease
	metadataStore           *metadata.Store
	sessionStoreRegistry    *registry.SessionStoreRegistry
	runtimeRegistry         *registry.RuntimeRegistry
	projectViews            client.ProjectViewClient
	authBootstrapService    *authbootstrap.Service
	authStatusService       *authstatus.Service
	askService              *askview.Service
	approvalService         *approvalview.Service
	processService          *processview.Service
	processOutputService    *processoutput.Service
	promptControlService    *promptcontrol.Service
	promptActivityService   *promptactivity.Service
	runtimeControlService   *runtimecontrol.Service
	sessionRuntimeService   *sessionruntime.Service
	sessionViewService      *sessionview.Service
	sessionLifecycleService *sessionlifecycle.Service
	sessionActivityService  *sessionactivity.Service
	updateStatusService     *updatestatus.Service
	worktreeService         *worktree.Service
}

func composeBundles(in bundleCompositionInput) *Bundles {
	return &Bundles{
		Auth: newAuthBundle(in.authSupport, in.authBootstrapService, in.authStatusService),
		cleanup: []lifecycleResource{
			{name: "persistence root lock", close: in.rootLease.Close},
			{name: "metadata store", close: in.metadataStore.Close},
			{name: "background manager", close: in.runtimeSupport.Background.Close},
		},
		Persistence: newPersistenceBundle(in.rootLease, in.metadataStore, in.sessionStoreRegistry),
		Processes:   newProcessBundle(in.processService, in.processOutputService),
		Projects:    newProjectBundle(in.cfg, in.containerDir, in.projectViews),
		Prompts:     newPromptBundle(in.askService, in.approvalService, in.promptControlService, in.promptActivityService),
		Runtime:     newRuntimeBundle(in.runtimeSupport, in.runtimeRegistry, in.runtimeControlService, in.sessionRuntimeService, in.sessionActivityService),
		Sessions:    newSessionBundle(in.sessionViewService, in.sessionLifecycleService),
		Updates:     &UpdateBundle{updateStatus: in.updateStatusService},
		Worktrees:   &WorktreeBundle{worktrees: client.NewLoopbackWorktreeClient(in.worktreeService)},
	}
}

func newAuthBundle(authSupport serverbootstrap.AuthSupport, bootstrapService *authbootstrap.Service, statusService *authstatus.Service) *AuthBundle {
	return &AuthBundle{
		support:       authSupport,
		authBootstrap: client.NewLoopbackAuthBootstrapClient(bootstrapService),
		authStatus:    client.NewLoopbackAuthStatusClient(statusService),
	}
}

func newPersistenceBundle(rootLease *rootlock.Lease, metadataStore *metadata.Store, sessionStoreRegistry *registry.SessionStoreRegistry) *PersistenceBundle {
	return &PersistenceBundle{
		rootLock:      rootLease,
		metadataStore: metadataStore,
		sessionStores: sessionStoreRegistry,
	}
}

func newProcessBundle(processService *processview.Service, processOutputService *processoutput.Service) *ProcessBundle {
	return &ProcessBundle{
		processControls: client.NewLoopbackProcessControlClient(processService),
		processOutput:   client.NewLoopbackProcessOutputClient(processOutputService),
		processViews:    client.NewLoopbackProcessViewClient(processService),
	}
}

func newProjectBundle(cfg config.App, containerDir string, projectViews client.ProjectViewClient) *ProjectBundle {
	return &ProjectBundle{
		cfg:          cfg,
		containerDir: containerDir,
		projectViews: projectViews,
	}
}

func newPromptBundle(askService *askview.Service, approvalService *approvalview.Service, promptControlService *promptcontrol.Service, promptActivityService *promptactivity.Service) *PromptBundle {
	return &PromptBundle{
		askViews:       client.NewLoopbackAskViewClient(askService),
		approvalViews:  client.NewLoopbackApprovalViewClient(approvalService),
		promptControl:  client.NewLoopbackPromptControlClient(promptControlService),
		promptActivity: client.NewLoopbackPromptActivityClient(promptActivityService),
	}
}

func newRuntimeBundle(runtimeSupport serverbootstrap.RuntimeSupport, runtimeRegistry *registry.RuntimeRegistry, runtimeControlService *runtimecontrol.Service, sessionRuntimeService *sessionruntime.Service, sessionActivityService *sessionactivity.Service) *RuntimeBundle {
	return &RuntimeBundle{
		fastModeState:    runtimeSupport.FastModeState,
		background:       runtimeSupport.Background,
		backgroundRouter: runtimeSupport.BackgroundRouter,
		runtimeRegistry:  runtimeRegistry,
		runtimeControls:  client.NewLoopbackRuntimeControlClient(runtimeControlService),
		sessionRuntime:   client.NewLoopbackSessionRuntimeClient(sessionRuntimeService),
		sessionActivity:  client.NewLoopbackSessionActivityClient(sessionActivityService),
	}
}

func newSessionBundle(sessionViewService *sessionview.Service, sessionLifecycleService *sessionlifecycle.Service) *SessionBundle {
	return &SessionBundle{
		sessionLaunchMap: make(map[string]client.SessionLaunchClient),
		sessionServices:  make(map[string]*sessionlaunch.Service),
		runPromptMap:     make(map[string]client.RunPromptClient),
		sessionLaunch:    unregisteredSessionLaunchClient{},
		sessionViews:     client.NewLoopbackSessionViewClient(sessionViewService),
		sessionLifecycle: client.NewLoopbackSessionLifecycleClient(sessionLifecycleService),
		runPrompt:        unregisteredRunPromptClient{},
	}
}

func validateAuthBundleSupport(authSupport serverbootstrap.AuthSupport) error {
	if authSupport.AuthManager == nil {
		return bundleResourceRequiredError("auth", "auth manager")
	}
	return nil
}

func validateRuntimeBundleSupport(runtimeSupport serverbootstrap.RuntimeSupport) error {
	if runtimeSupport.Background == nil {
		return bundleResourceRequiredError("runtime", "background manager")
	}
	return nil
}
