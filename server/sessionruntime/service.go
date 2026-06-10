package sessionruntime

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"builder/server/auth"
	"builder/server/metadata"
	"builder/server/primaryrun"
	"builder/server/registry"
	"builder/server/runprompt"
	"builder/server/runtime"
	"builder/server/runtimeview"
	"builder/server/runtimewire"
	"builder/server/session"
	askquestion "builder/server/tools/askquestion"
	shelltool "builder/server/tools/shell"
	"builder/shared/clientui"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/servicecontract"
	"builder/shared/toolspec"
	"builder/shared/transcript"
	"builder/shared/transcriptdiag"

	"github.com/google/uuid"
)

type Service struct {
	persistenceRoot          string
	metadataStore            *metadata.Store
	authManager              *auth.Manager
	fastModeState            *runtime.FastModeState
	background               *shelltool.Manager
	backgroundRouter         *runtimewire.BackgroundEventRouter
	runtimes                 *registry.RuntimeRegistry
	sessionStores            *registry.SessionStoreRegistry
	storeOptions             []session.StoreOption
	recoveredWarning         string
	recoveredWarningProvider func() (string, bool, error)

	mu      sync.Mutex
	handles map[string]*runtimeHandle

	idleUnloadDelay        time.Duration
	runFinishedUnloadDelay time.Duration
	idleTimers             map[string]*runtimeIdleTimer
}

type runtimeHandle struct {
	controllerRequestID string
	controllerLeaseID   string
	ownerRefs           int
	ownerIDs            map[string]struct{}
	activationErr       error
	closing             bool
	takeover            *runtimeTakeover
	ready               chan struct{}
	closed              chan struct{}
	closedOnce          sync.Once
	rebind              func(string) error
	close               func()
}

type runtimeIdleTimer struct {
	generation uint64
	timer      *time.Timer
}

const (
	defaultRuntimeIdleUnloadDelay        = 5 * time.Second
	defaultRunFinishedIdleUnloadDelay    = 3 * time.Minute
	bestEffortRuntimeLeaseReleaseTimeout = 2 * time.Second
)

type runtimeTakeover struct {
	requestID string
	leaseID   string
	err       error
	ready     chan struct{}
	readyOnce sync.Once
}

type activationClaim int

const (
	activationClaimOwner activationClaim = iota
	activationClaimReuse
	activationClaimClosing
	activationClaimTakeoverReuse
	activationClaimTakeover
)

func NewService(persistenceRoot string, metadataStore *metadata.Store, authManager *auth.Manager, fastModeState *runtime.FastModeState, background *shelltool.Manager, backgroundRouter *runtimewire.BackgroundEventRouter, runtimes *registry.RuntimeRegistry, sessionStores *registry.SessionStoreRegistry, storeOptions ...session.StoreOption) *Service {
	svc := &Service{
		persistenceRoot:        strings.TrimSpace(persistenceRoot),
		metadataStore:          metadataStore,
		authManager:            authManager,
		fastModeState:          fastModeState,
		background:             background,
		backgroundRouter:       backgroundRouter,
		runtimes:               runtimes,
		sessionStores:          sessionStores,
		storeOptions:           append([]session.StoreOption(nil), storeOptions...),
		handles:                make(map[string]*runtimeHandle),
		idleUnloadDelay:        defaultRuntimeIdleUnloadDelay,
		runFinishedUnloadDelay: defaultRunFinishedIdleUnloadDelay,
		idleTimers:             make(map[string]*runtimeIdleTimer),
	}
	if runtimes != nil {
		runtimes.SetInterestObserver(svc.runtimeInterestChanged)
	}
	return svc
}

func (s *Service) WithGeneratedRecoveredWarning(warning string) *Service {
	if s == nil {
		return nil
	}
	s.recoveredWarning = strings.TrimSpace(warning)
	return s
}

func (s *Service) WithGeneratedRecoveredWarningProvider(provider func() (string, bool, error)) *Service {
	if s == nil {
		return nil
	}
	s.recoveredWarningProvider = provider
	return s
}

type recoveredWarningEntry struct {
	Visibility transcript.EntryVisibility `json:"visibility,omitempty"`
	Role       string                     `json:"role"`
	Text       string                     `json:"text"`
}

func (s *Service) appendRecoveredWarningIfNeeded(store *session.Store) error {
	warning, ok, _ := s.generatedRecoveredWarning()
	if !ok || warning == "" || store == nil {
		return nil
	}
	if store.Meta().GeneratedRecoveredWarningIssued {
		return nil
	}
	_, _, appendErr := store.AppendEvent("", "local_entry", recoveredWarningEntry{
		Visibility: transcript.EntryVisibilityAll,
		Role:       "warning",
		Text:       warning,
	})
	if appendErr != nil {
		return appendErr
	}
	return store.MarkGeneratedRecoveredWarningIssued()
}

func (s *Service) generatedRecoveredWarning() (string, bool, error) {
	if s == nil {
		return "", false, nil
	}
	if s.recoveredWarningProvider != nil {
		warning, ok, err := s.recoveredWarningProvider()
		return strings.TrimSpace(warning), ok, err
	}
	warning := strings.TrimSpace(s.recoveredWarning)
	return warning, warning != "", nil
}

func (s *Service) ActivateSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeActivateRequest) (serverapi.SessionRuntimeActivateResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	sessionID := strings.TrimSpace(req.SessionID)
	requestID := strings.TrimSpace(req.ClientRequestID)
	ownerID := strings.TrimSpace(req.OwnerID)
	var handle *runtimeHandle
	var takeover *runtimeTakeover
	var claim activationClaim
	var err error
	for {
		if s.confirmExternalSessionRuntimeActive(ctx, sessionID) {
			return serverapi.SessionRuntimeActivateResponse{ReadOnly: true}, nil
		}
		handle, takeover, claim, err = s.claimActivation(sessionID, requestID, ownerID)
		if err != nil {
			return serverapi.SessionRuntimeActivateResponse{}, err
		}
		if claim != activationClaimClosing {
			break
		}
		if err := waitForRuntimeHandleClosed(ctx, handle); err != nil {
			return serverapi.SessionRuntimeActivateResponse{}, err
		}
	}
	if claim == activationClaimReuse {
		if err := waitForRuntimeHandleReady(ctx, handle); err != nil {
			return serverapi.SessionRuntimeActivateResponse{}, err
		}
		s.addRuntimeHandleOwnerRef(sessionID, handle, ownerID)
		return activationResponseForHandle(handle)
	}
	if claim == activationClaimTakeoverReuse {
		if err := waitForRuntimeTakeoverReady(ctx, takeover); err != nil {
			return serverapi.SessionRuntimeActivateResponse{}, err
		}
		return activationResponseForTakeover(takeover)
	}
	if claim == activationClaimTakeover {
		return s.takeOverActivation(ctx, sessionID, requestID, ownerID, handle, takeover)
	}
	var leaseID string
	var cleanup func()
	defer func() {
		if err == nil {
			return
		}
		if cleanup != nil {
			cleanup()
		}
		if strings.TrimSpace(leaseID) != "" {
			s.releaseRuntimeLeaseBestEffort(sessionID, leaseID)
		}
		s.failActivation(sessionID, handle, err)
	}()
	store, err := s.resolveStore(ctx, sessionID)
	if err != nil {
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	if err := store.EnsureDurable(); err != nil {
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	if err := s.appendRecoveredWarningIfNeeded(store); err != nil {
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	lease, err := s.createRuntimeLease(ctx, sessionID, requestID)
	if err != nil {
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	leaseID = lease.LeaseID
	target, err := s.resolveExecutionTarget(ctx, sessionID)
	if err != nil {
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	logger, err := runprompt.NewRunLogger(store.Dir(), nil)
	if err != nil {
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	logger.Logf("app.interactive.start session_id=%s workspace=%s workdir=%s model=%s", sessionID, target.WorkspaceRoot, target.EffectiveWorkdir, req.ActiveSettings.Model)
	logger.Logf("config.settings path=%s created=%t", req.Source.SettingsPath, req.Source.CreatedDefaultConfig)
	for _, line := range configSourceLines(req.Source.Sources) {
		logger.Logf("config.source %s", line)
	}
	enabledTools, err := parseToolIDs(req.EnabledToolIDs)
	if err != nil {
		_ = logger.Close()
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	wiring, err := runtimewire.NewRuntimeWiringWithBackground(store, req.ActiveSettings, enabledTools, target.EffectiveWorkdir, s.authManager, logger, s.background, runtimewire.RuntimeWiringOptions{
		FastMode: s.fastModeState,
		Sources:  req.Source.Sources,
		OnEvent: func(evt runtime.Event) {
			logger.Logf("%s", runprompt.FormatRuntimeEvent(evt))
			if transcriptdiag.EnabledForProcess(req.ActiveSettings.Debug) {
				projected := runtimeview.EventFromRuntime(evt)
				logger.Logf("%s", runprompt.FormatTranscriptProjectionDiagnostic(sessionID, projected))
				logger.Logf("%s", runprompt.FormatTranscriptPublishDiagnostic(sessionID, projected))
			}
			if s.runtimes != nil {
				s.runtimes.PublishRuntimeEvent(sessionID, evt)
			}
		},
	})
	if err != nil {
		_ = logger.Close()
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	if wiring.AskBroker != nil && s.runtimes != nil {
		wiring.AskBroker.SetAskHandler(func(req askquestion.Request) (askquestion.Response, error) {
			return s.runtimes.AwaitPromptResponse(context.Background(), sessionID, req)
		})
	}
	var runtimeRegistry runtimewire.RuntimeRegistry
	if s.runtimes != nil {
		runtimeRegistry = s.runtimes
	}
	var backgroundRouter runtimewire.BackgroundRouter
	if s.backgroundRouter != nil {
		backgroundRouter = s.backgroundRouter
	}
	registration := runtimewire.RegisterSessionRuntime(sessionID, wiring.Engine, runtimeRegistry, backgroundRouter)
	cleanup = func() {
		registration.Close()
		_ = wiring.Close()
		_ = logger.Close()
	}
	handle.rebind = nil
	var localRebind func(string) error
	if wiring.LocalTools != nil {
		localRebind = wiring.LocalTools.Rebind
	}
	handle.rebind = runtimeRebindFunc(localRebind, wiring.Engine)
	s.completeActivation(handle, leaseID, cleanup)
	s.cancelScheduledIdleUnload(sessionID)
	cleanup = nil
	return serverapi.SessionRuntimeActivateResponse{LeaseID: leaseID}, nil
}

func (s *Service) externalSessionRuntimeActive(sessionID string) bool {
	if s == nil || s.runtimes == nil || !s.runtimes.IsSessionRuntimeActive(sessionID) {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.handles[strings.TrimSpace(sessionID)] == nil
}

func (s *Service) confirmExternalSessionRuntimeActive(ctx context.Context, sessionID string) bool {
	if !s.externalSessionRuntimeActive(sessionID) || s.runtimes == nil {
		return false
	}
	engine, err := s.runtimes.ResolveRuntime(ctx, sessionID)
	if err != nil || engine == nil {
		return false
	}
	return s.externalSessionRuntimeActive(sessionID)
}

func runtimeRebindFunc(localRebind func(string) error, engine *runtime.Engine) func(string) error {
	return func(workdir string) error {
		if localRebind != nil {
			if err := localRebind(workdir); err != nil {
				return err
			}
		}
		if engine != nil {
			engine.SetTranscriptWorkingDir(workdir)
		}
		return nil
	}
}

func (s *Service) ReleaseSessionRuntime(ctx context.Context, req serverapi.SessionRuntimeReleaseRequest) (serverapi.SessionRuntimeReleaseResponse, error) {
	if err := req.Validate(); err != nil {
		return serverapi.SessionRuntimeReleaseResponse{}, err
	}
	sessionID := strings.TrimSpace(req.SessionID)
	leaseID := strings.TrimSpace(req.LeaseID)
	leaseErr := error(nil)
	if _, err := s.validateRuntimeLease(ctx, sessionID, leaseID); err != nil {
		leaseErr = err
	}
	s.mu.Lock()
	handle := s.handles[sessionID]
	if handle == nil {
		s.mu.Unlock()
		_, err := s.releaseRuntimeLease(ctx, sessionID, leaseID)
		return serverapi.SessionRuntimeReleaseResponse{Released: err == nil}, err
	}
	s.mu.Unlock()
	if err := waitForRuntimeHandleReady(ctx, handle); err != nil {
		if leaseErr == nil {
			s.closeReleasedRuntimeHandle(sessionID, handle)
		}
		return serverapi.SessionRuntimeReleaseResponse{}, err
	}
	s.mu.Lock()
	current := s.handles[sessionID]
	if current == nil || current != handle || strings.TrimSpace(current.controllerLeaseID) != leaseID {
		s.mu.Unlock()
		return serverapi.SessionRuntimeReleaseResponse{}, errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is invalid or expired", strings.TrimSpace(sessionID)))
	}
	if leaseErr != nil {
		s.mu.Unlock()
		return serverapi.SessionRuntimeReleaseResponse{}, leaseErr
	}
	var primaryLease primaryrun.Lease
	if req.OnlyIfIdle {
		if req.DropOwner && current.ownerRefs > 1 {
			current.ownerRefs--
			s.mu.Unlock()
			return serverapi.SessionRuntimeReleaseResponse{}, nil
		}
		s.mu.Unlock()
		lease, err := s.acquirePrimaryRunLease(sessionID)
		if errors.Is(err, primaryrun.ErrActivePrimaryRun) {
			if req.DropOwner {
				s.markRuntimeHandleOrphaned(sessionID, handle, leaseID, req.OwnerID)
			}
			return serverapi.SessionRuntimeReleaseResponse{Active: true}, nil
		}
		if err != nil {
			return serverapi.SessionRuntimeReleaseResponse{}, err
		}
		primaryLease = lease
		active, err := s.runtimeHasActiveRun(ctx, sessionID)
		if err != nil {
			if primaryLease != nil {
				primaryLease.Release()
			}
			return serverapi.SessionRuntimeReleaseResponse{}, err
		}
		if active {
			if primaryLease != nil {
				primaryLease.Release()
			}
			if req.DropOwner {
				s.markRuntimeHandleOrphaned(sessionID, handle, leaseID, req.OwnerID)
			}
			return serverapi.SessionRuntimeReleaseResponse{Active: true}, nil
		}
		if s.runtimeHasSubscribers(sessionID) {
			if primaryLease != nil {
				primaryLease.Release()
			}
			if req.DropOwner {
				s.markRuntimeHandleOrphaned(sessionID, handle, leaseID, req.OwnerID)
			}
			return serverapi.SessionRuntimeReleaseResponse{}, nil
		}
		s.mu.Lock()
		current = s.handles[sessionID]
		if current == nil || current != handle || strings.TrimSpace(current.controllerLeaseID) != leaseID {
			s.mu.Unlock()
			if primaryLease != nil {
				primaryLease.Release()
			}
			return serverapi.SessionRuntimeReleaseResponse{}, errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is invalid or expired", strings.TrimSpace(sessionID)))
		}
	}
	current.closing = true
	closeFn := current.close
	takeover := current.takeover
	s.mu.Unlock()
	defer func() {
		if primaryLease != nil {
			primaryLease.Release()
		}
	}()
	finishRuntimeTakeover(takeover, "", errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is invalid or expired", strings.TrimSpace(sessionID))))
	if closeFn != nil {
		closeFn()
	}
	s.mu.Lock()
	if s.handles[sessionID] == current {
		delete(s.handles, sessionID)
		signalRuntimeHandleClosed(current)
	}
	s.mu.Unlock()
	s.clearScheduledIdleUnload(sessionID)
	_, err := s.releaseRuntimeLease(ctx, sessionID, leaseID)
	return serverapi.SessionRuntimeReleaseResponse{Released: err == nil}, err
}

func (s *Service) acquirePrimaryRunLease(sessionID string) (primaryrun.Lease, error) {
	if s == nil || s.runtimes == nil {
		return primaryrun.LeaseFunc(func() {}), nil
	}
	return s.runtimes.AcquirePrimaryRun(strings.TrimSpace(sessionID))
}

func (s *Service) runtimeHasActiveRun(ctx context.Context, sessionID string) (bool, error) {
	if s == nil || s.runtimes == nil {
		return false, nil
	}
	engine, err := s.runtimes.ResolveRuntime(ctx, strings.TrimSpace(sessionID))
	if err != nil || engine == nil {
		return false, err
	}
	return engine.ActiveRun() != nil, nil
}

func (s *Service) markRuntimeHandleOrphaned(sessionID string, handle *runtimeHandle, leaseID string, ownerID string) {
	if s == nil || handle == nil {
		return
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	trimmedLeaseID := strings.TrimSpace(leaseID)
	s.mu.Lock()
	current := s.handles[trimmedSessionID]
	if current == handle && strings.TrimSpace(current.controllerLeaseID) == trimmedLeaseID {
		trimmedOwnerID := strings.TrimSpace(ownerID)
		if trimmedOwnerID != "" && len(current.ownerIDs) > 0 {
			if _, ok := current.ownerIDs[trimmedOwnerID]; ok {
				delete(current.ownerIDs, trimmedOwnerID)
				if current.ownerRefs > 0 {
					current.ownerRefs--
				}
			}
		} else if current.ownerRefs > 0 {
			current.ownerRefs--
		}
	}
	s.mu.Unlock()
	s.scheduleIdleUnload(trimmedSessionID, s.defaultIdleUnloadDelay())
}

func (s *Service) addRuntimeHandleOwnerRef(sessionID string, handle *runtimeHandle, ownerID string) {
	if s == nil || handle == nil {
		return
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return
	}
	s.mu.Lock()
	if current := s.handles[trimmedSessionID]; current == handle {
		if trimmedOwnerID := strings.TrimSpace(ownerID); trimmedOwnerID != "" {
			if current.ownerIDs == nil {
				current.ownerIDs = make(map[string]struct{})
			}
			if _, exists := current.ownerIDs[trimmedOwnerID]; !exists {
				current.ownerIDs[trimmedOwnerID] = struct{}{}
				current.ownerRefs++
			}
		} else {
			current.ownerRefs++
		}
	}
	s.mu.Unlock()
	s.cancelScheduledIdleUnload(trimmedSessionID)
}

func (s *Service) runtimeInterestChanged(sessionID string, reason registry.RuntimeInterestReason) {
	delay := s.defaultIdleUnloadDelay()
	if reason == registry.RuntimeInterestRunFinished {
		delay = s.runFinishedIdleUnloadDelay()
	}
	s.scheduleIdleUnload(sessionID, delay)
}

func (s *Service) cancelScheduledIdleUnload(sessionID string) {
	if s == nil {
		return
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return
	}
	s.mu.Lock()
	state := s.idleTimers[trimmedSessionID]
	if state != nil {
		state.generation++
		if state.timer != nil {
			state.timer.Stop()
		}
	}
	s.mu.Unlock()
}

func (s *Service) clearScheduledIdleUnload(sessionID string) {
	if s == nil {
		return
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return
	}
	s.mu.Lock()
	state := s.idleTimers[trimmedSessionID]
	if state != nil && state.timer != nil {
		state.timer.Stop()
	}
	delete(s.idleTimers, trimmedSessionID)
	s.mu.Unlock()
}

func (s *Service) defaultIdleUnloadDelay() time.Duration {
	if s == nil || s.idleUnloadDelay <= 0 {
		return defaultRuntimeIdleUnloadDelay
	}
	return s.idleUnloadDelay
}

func (s *Service) runFinishedIdleUnloadDelay() time.Duration {
	if s == nil || s.runFinishedUnloadDelay <= 0 {
		return defaultRunFinishedIdleUnloadDelay
	}
	return s.runFinishedUnloadDelay
}

func (s *Service) scheduleIdleUnload(sessionID string, delay time.Duration) {
	if s == nil {
		return
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" || delay <= 0 {
		return
	}
	s.mu.Lock()
	if s.idleTimers == nil {
		s.idleTimers = make(map[string]*runtimeIdleTimer)
	}
	state := s.idleTimers[trimmedSessionID]
	if state == nil {
		state = &runtimeIdleTimer{}
		s.idleTimers[trimmedSessionID] = state
	}
	state.generation++
	generation := state.generation
	if state.timer != nil {
		state.timer.Stop()
	}
	state.timer = time.AfterFunc(delay, func() {
		s.runScheduledIdleUnload(trimmedSessionID, generation)
	})
	s.mu.Unlock()
}

func (s *Service) runScheduledIdleUnload(sessionID string, generation uint64) {
	if s == nil {
		return
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	if trimmedSessionID == "" {
		return
	}
	s.mu.Lock()
	state := s.idleTimers[trimmedSessionID]
	if state == nil || state.generation != generation {
		s.mu.Unlock()
		return
	}
	handle := s.handles[trimmedSessionID]
	if handle == nil || handle.closing || handle.ownerRefs > 0 {
		s.mu.Unlock()
		return
	}
	leaseID := strings.TrimSpace(handle.controllerLeaseID)
	s.mu.Unlock()
	if leaseID == "" || s.runtimeHasSubscribers(trimmedSessionID) {
		return
	}
	if active, err := s.runtimeHasActiveRun(context.Background(), trimmedSessionID); err != nil || active {
		return
	}
	_, _ = s.ReleaseSessionRuntime(context.Background(), serverapi.SessionRuntimeReleaseRequest{
		ClientRequestID: uuid.NewString(),
		SessionID:       trimmedSessionID,
		LeaseID:         leaseID,
		OnlyIfIdle:      true,
		DropOwner:       true,
	})
}

func (s *Service) runtimeHasSubscribers(sessionID string) bool {
	if s == nil || s.runtimes == nil {
		return false
	}
	return s.runtimes.HasRuntimeSubscribers(strings.TrimSpace(sessionID))
}

func (s *Service) closeReleasedRuntimeHandle(sessionID string, handle *runtimeHandle) {
	if handle == nil {
		return
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	s.mu.Lock()
	current := s.handles[trimmedSessionID]
	if current == nil || current != handle {
		s.mu.Unlock()
		return
	}
	current.closing = true
	closeFn := current.close
	takeover := current.takeover
	s.mu.Unlock()
	finishRuntimeTakeover(takeover, "", errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is invalid or expired", strings.TrimSpace(trimmedSessionID))))
	if closeFn != nil {
		closeFn()
	}
	s.mu.Lock()
	if s.handles[trimmedSessionID] == current {
		delete(s.handles, trimmedSessionID)
		signalRuntimeHandleClosed(current)
	}
	s.mu.Unlock()
	s.clearScheduledIdleUnload(trimmedSessionID)
}

func (s *Service) RequireControllerLease(ctx context.Context, sessionID string, leaseID string) error {
	trimmedSessionID := strings.TrimSpace(sessionID)
	trimmedLeaseID := strings.TrimSpace(leaseID)
	if trimmedLeaseID == "" {
		return errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is required", trimmedSessionID))
	}
	s.mu.Lock()
	handle := s.handles[trimmedSessionID]
	s.mu.Unlock()
	if handle == nil {
		return errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is invalid or expired", strings.TrimSpace(trimmedSessionID)))
	}
	if err := waitForRuntimeHandleReady(ctx, handle); err != nil {
		return err
	}
	s.mu.Lock()
	current := s.handles[trimmedSessionID]
	if current == nil || current != handle {
		s.mu.Unlock()
		return errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is invalid or expired", strings.TrimSpace(trimmedSessionID)))
	}
	activationErr := current.activationErr
	controllerLeaseID := strings.TrimSpace(current.controllerLeaseID)
	s.mu.Unlock()
	if activationErr != nil {
		return errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is invalid or expired", strings.TrimSpace(trimmedSessionID)))
	}
	if controllerLeaseID != trimmedLeaseID {
		return errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is invalid or expired", strings.TrimSpace(trimmedSessionID)))
	}
	if s.metadataStore != nil {
		if _, err := s.validateRuntimeLease(ctx, trimmedSessionID, trimmedLeaseID); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) RebindLocalTools(ctx context.Context, sessionID string, leaseID string, workspaceRoot string) error {
	trimmedRoot := strings.TrimSpace(workspaceRoot)
	trimmedLeaseID := strings.TrimSpace(leaseID)
	if trimmedRoot == "" {
		return errors.New("workspace root is required")
	}
	if err := s.RequireControllerLease(ctx, sessionID, leaseID); err != nil {
		return err
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	s.mu.Lock()
	handle := s.handles[trimmedSessionID]
	s.mu.Unlock()
	if handle == nil {
		return errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is invalid or expired", strings.TrimSpace(trimmedSessionID)))
	}
	if err := waitForRuntimeHandleReady(ctx, handle); err != nil {
		return err
	}
	s.mu.Lock()
	current := s.handles[trimmedSessionID]
	if err := s.ensureCurrentControllerLeaseLocked(trimmedSessionID, trimmedLeaseID, handle); err != nil {
		s.mu.Unlock()
		return err
	}
	rebind := current.rebind
	s.mu.Unlock()
	if rebind == nil {
		return nil
	}
	return rebind(trimmedRoot)
}

func (s *Service) RecordWorktreeTransition(ctx context.Context, sessionID string, leaseID string, state session.WorktreeReminderState) error {
	if err := s.RequireControllerLease(ctx, sessionID, leaseID); err != nil {
		return err
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	trimmedLeaseID := strings.TrimSpace(leaseID)
	store, err := s.resolveStore(ctx, trimmedSessionID)
	if err != nil {
		return err
	}
	normalized, err := normalizeWorktreeReminderState(state)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if err := s.ensureCurrentControllerLeaseLocked(trimmedSessionID, trimmedLeaseID, nil); err != nil {
		s.mu.Unlock()
		return err
	}
	s.mu.Unlock()
	return store.SetWorktreeReminderState(&normalized)
}

func (s *Service) ensureCurrentControllerLeaseLocked(sessionID string, leaseID string, handle *runtimeHandle) error {
	trimmedSessionID := strings.TrimSpace(sessionID)
	trimmedLeaseID := strings.TrimSpace(leaseID)
	current := s.handles[trimmedSessionID]
	if current == nil {
		return errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is invalid or expired", strings.TrimSpace(trimmedSessionID)))
	}
	if handle != nil && current != handle {
		return errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is invalid or expired", strings.TrimSpace(trimmedSessionID)))
	}
	if strings.TrimSpace(current.controllerLeaseID) != trimmedLeaseID {
		return errors.Join(serverapi.ErrInvalidControllerLease, fmt.Errorf("controller lease for session %q is invalid or expired", strings.TrimSpace(trimmedSessionID)))
	}
	return nil
}

func (s *Service) SyncExecutionTarget(ctx context.Context, sessionID string, target clientui.SessionExecutionTarget, reminder *session.WorktreeReminderState) error {
	trimmedSessionID := strings.TrimSpace(sessionID)
	trimmedWorkdir := strings.TrimSpace(target.EffectiveWorkdir)
	if trimmedSessionID == "" {
		return errors.New("session id is required")
	}
	if trimmedWorkdir == "" {
		return errors.New("execution target effective workdir is required")
	}
	var normalizedReminder *session.WorktreeReminderState
	if reminder != nil {
		normalized, err := normalizeWorktreeReminderState(*reminder)
		if err != nil {
			return err
		}
		normalizedReminder = &normalized
	}
	handle, err := s.activeRuntimeHandle(ctx, trimmedSessionID)
	if err != nil {
		return err
	}
	if handle == nil || handle.rebind == nil {
		return s.persistWorktreeReminderState(ctx, trimmedSessionID, normalizedReminder)
	}
	if err := handle.rebind(trimmedWorkdir); err != nil {
		return err
	}
	return s.persistWorktreeReminderState(ctx, trimmedSessionID, normalizedReminder)
}

func (s *Service) persistWorktreeReminderState(ctx context.Context, sessionID string, reminder *session.WorktreeReminderState) error {
	if reminder == nil {
		return nil
	}
	store, err := s.resolveStore(ctx, strings.TrimSpace(sessionID))
	if err != nil {
		return err
	}
	return store.SetWorktreeReminderState(reminder)
}

func normalizeWorktreeReminderState(state session.WorktreeReminderState) (session.WorktreeReminderState, error) {
	state.Mode = session.WorktreeReminderMode(strings.TrimSpace(string(state.Mode)))
	switch state.Mode {
	case session.WorktreeReminderModeEnter, session.WorktreeReminderModeExit:
	default:
		return session.WorktreeReminderState{}, errors.New("worktree reminder mode is required")
	}
	state.Branch = strings.TrimSpace(state.Branch)
	state.WorktreePath = strings.TrimSpace(state.WorktreePath)
	state.WorkspaceRoot = strings.TrimSpace(state.WorkspaceRoot)
	state.EffectiveCwd = strings.TrimSpace(state.EffectiveCwd)
	if state.WorkspaceRoot == "" {
		return session.WorktreeReminderState{}, errors.New("worktree reminder workspace root is required")
	}
	if state.EffectiveCwd == "" {
		return session.WorktreeReminderState{}, errors.New("worktree reminder effective cwd is required")
	}
	if state.Mode == session.WorktreeReminderModeEnter && state.WorktreePath == "" {
		return session.WorktreeReminderState{}, errors.New("worktree reminder worktree path is required for enter mode")
	}
	state.HasIssuedInGeneration = false
	state.IssuedCompactionCount = 0
	return state, nil
}

func (s *Service) resolveStore(ctx context.Context, sessionID string) (*session.Store, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.sessionStores != nil {
		store, err := s.sessionStores.ResolveStore(ctx, sessionID)
		if err != nil {
			return nil, err
		}
		if store != nil {
			return store, nil
		}
	}
	store, err := session.OpenByID(s.persistenceRoot, sessionID, s.storeOptions...)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.sessionStores != nil {
		s.sessionStores.RegisterStore(store)
	}
	return store, nil
}

func (s *Service) activeRuntimeHandle(ctx context.Context, sessionID string) (*runtimeHandle, error) {
	trimmedSessionID := strings.TrimSpace(sessionID)
	s.mu.Lock()
	handle := s.handles[trimmedSessionID]
	s.mu.Unlock()
	if handle == nil {
		return nil, nil
	}
	if err := waitForRuntimeHandleReady(ctx, handle); err != nil {
		return nil, err
	}
	s.mu.Lock()
	current := s.handles[trimmedSessionID]
	s.mu.Unlock()
	if current == nil || current != handle {
		return nil, nil
	}
	if current.activationErr != nil {
		return nil, current.activationErr
	}
	return current, nil
}

// Phase 2 temporarily allows many attached readers, but exactly one controlling
// client per session. A second activation must fail explicitly instead of
// joining the active runtime.
func (s *Service) claimActivation(sessionID string, requestID string, ownerID string) (*runtimeHandle, *runtimeTakeover, activationClaim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current := s.handles[sessionID]; current != nil {
		if current.closing {
			return current, nil, activationClaimClosing, nil
		}
		if current.controllerRequestID == requestID {
			return current, nil, activationClaimReuse, nil
		}
		if current.takeover != nil {
			if current.takeover.requestID == requestID {
				return current, current.takeover, activationClaimTakeoverReuse, nil
			}
			return nil, nil, activationClaimOwner, errors.Join(serverapi.ErrSessionAlreadyControlled, fmt.Errorf("session %q is already controlled by another client", sessionID))
		}
		if runtimeHandleReady(current) && current.activationErr == nil {
			takeover := &runtimeTakeover{
				requestID: requestID,
				ready:     make(chan struct{}),
			}
			current.takeover = takeover
			return current, takeover, activationClaimTakeover, nil
		}
		return nil, nil, activationClaimOwner, errors.Join(serverapi.ErrSessionAlreadyControlled, fmt.Errorf("session %q is already controlled by another client", sessionID))
	}
	handle := newRuntimeHandle(requestID, ownerID)
	s.handles[sessionID] = handle
	return handle, nil, activationClaimOwner, nil
}

func newRuntimeHandle(requestID string, ownerID string) *runtimeHandle {
	handle := &runtimeHandle{
		controllerRequestID: strings.TrimSpace(requestID),
		ownerRefs:           1,
		ready:               make(chan struct{}),
		closed:              make(chan struct{}),
	}
	if trimmedOwnerID := strings.TrimSpace(ownerID); trimmedOwnerID != "" {
		handle.ownerIDs = map[string]struct{}{trimmedOwnerID: {}}
	}
	return handle
}

func (s *Service) takeOverActivation(ctx context.Context, sessionID string, requestID string, ownerID string, handle *runtimeHandle, takeover *runtimeTakeover) (serverapi.SessionRuntimeActivateResponse, error) {
	if err := waitForRuntimeHandleReady(ctx, handle); err != nil {
		s.failTakeover(sessionID, handle, takeover, err)
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	if _, err := activationResponseForHandle(handle); err != nil {
		s.failTakeover(sessionID, handle, takeover, err)
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	lease, err := s.createRuntimeLease(ctx, sessionID, requestID)
	if err != nil {
		s.failTakeover(sessionID, handle, takeover, err)
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	leaseID := strings.TrimSpace(lease.LeaseID)
	ok, completeErr := s.completeTakeover(ctx, sessionID, handle, takeover, requestID, leaseID, ownerID)
	if completeErr != nil {
		if strings.TrimSpace(leaseID) != "" {
			s.releaseRuntimeLeaseBestEffort(sessionID, leaseID)
		}
		return serverapi.SessionRuntimeActivateResponse{}, completeErr
	}
	if !ok {
		if strings.TrimSpace(leaseID) != "" {
			s.releaseRuntimeLeaseBestEffort(sessionID, leaseID)
		}
		err := errors.Join(serverapi.ErrSessionAlreadyControlled, fmt.Errorf("session %q is already controlled by another client", sessionID))
		finishRuntimeTakeover(takeover, "", err)
		return serverapi.SessionRuntimeActivateResponse{}, err
	}
	return serverapi.SessionRuntimeActivateResponse{LeaseID: leaseID}, nil
}

func runtimeHandleReady(handle *runtimeHandle) bool {
	if handle == nil || handle.ready == nil {
		return true
	}
	select {
	case <-handle.ready:
		return true
	default:
		return false
	}
}

func waitForRuntimeTakeoverReady(ctx context.Context, takeover *runtimeTakeover) error {
	if takeover == nil || takeover.ready == nil {
		return nil
	}
	select {
	case <-takeover.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) completeActivation(handle *runtimeHandle, leaseID string, closeFn func()) {
	if handle == nil {
		return
	}
	handle.takeover = nil
	handle.controllerLeaseID = strings.TrimSpace(leaseID)
	handle.close = closeFn
	if handle.ownerRefs <= 0 {
		handle.ownerRefs = 1
	}
	close(handle.ready)
}

func (s *Service) completeTakeover(ctx context.Context, sessionID string, handle *runtimeHandle, takeover *runtimeTakeover, requestID string, leaseID string, ownerID string) (bool, error) {
	if handle == nil || takeover == nil {
		return false, nil
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	trimmedLeaseID := strings.TrimSpace(leaseID)
	s.mu.Lock()
	current := s.handles[trimmedSessionID]
	if current == nil || current != handle || current.takeover != takeover {
		s.mu.Unlock()
		return false, nil
	}
	previousLeaseID := strings.TrimSpace(current.controllerLeaseID)
	s.mu.Unlock()
	if previousLeaseID != "" {
		if _, err := s.releaseRuntimeLease(ctx, trimmedSessionID, previousLeaseID); err != nil {
			s.mu.Lock()
			if s.handles[trimmedSessionID] == current && current.takeover == takeover {
				current.takeover = nil
			}
			s.mu.Unlock()
			finishRuntimeTakeover(takeover, "", err)
			return false, err
		}
	}
	s.mu.Lock()
	current = s.handles[trimmedSessionID]
	if current == nil || current != handle || current.takeover != takeover {
		s.mu.Unlock()
		return false, nil
	}
	current.controllerRequestID = strings.TrimSpace(requestID)
	current.controllerLeaseID = trimmedLeaseID
	current.ownerRefs = 1
	current.ownerIDs = nil
	if trimmedOwnerID := strings.TrimSpace(ownerID); trimmedOwnerID != "" {
		current.ownerIDs = map[string]struct{}{trimmedOwnerID: {}}
	}
	current.takeover = nil
	s.mu.Unlock()
	s.cancelScheduledIdleUnload(trimmedSessionID)
	finishRuntimeTakeover(takeover, trimmedLeaseID, nil)
	return true, nil
}

func (s *Service) failTakeover(sessionID string, handle *runtimeHandle, takeover *runtimeTakeover, err error) {
	if handle == nil || takeover == nil {
		return
	}
	trimmedSessionID := strings.TrimSpace(sessionID)
	s.mu.Lock()
	current := s.handles[trimmedSessionID]
	if current != nil && current == handle && current.takeover == takeover {
		current.takeover = nil
	}
	s.mu.Unlock()
	finishRuntimeTakeover(takeover, "", err)
}

func (s *Service) failActivation(sessionID string, handle *runtimeHandle, err error) {
	if handle == nil {
		return
	}
	handle.activationErr = err
	close(handle.ready)
	s.mu.Lock()
	defer s.mu.Unlock()
	current := s.handles[strings.TrimSpace(sessionID)]
	if current == nil || current != handle {
		return
	}
	finishRuntimeTakeover(current.takeover, "", err)
	delete(s.handles, strings.TrimSpace(sessionID))
}

func finishRuntimeTakeover(takeover *runtimeTakeover, leaseID string, err error) {
	if takeover == nil {
		return
	}
	takeover.readyOnce.Do(func() {
		takeover.leaseID = strings.TrimSpace(leaseID)
		takeover.err = err
		if takeover.ready != nil {
			close(takeover.ready)
		}
	})
}

func (s *Service) resolveExecutionTarget(ctx context.Context, sessionID string) (clientui.SessionExecutionTarget, error) {
	if s == nil || s.metadataStore == nil {
		return clientui.SessionExecutionTarget{}, fmt.Errorf("metadata store is required")
	}
	return s.metadataStore.ResolveSessionExecutionTarget(ctx, sessionID)
}

func (s *Service) createRuntimeLease(ctx context.Context, sessionID string, requestID string) (metadata.RuntimeLeaseRecord, error) {
	if s == nil || s.metadataStore == nil {
		return metadata.RuntimeLeaseRecord{}, fmt.Errorf("metadata store is required")
	}
	return s.metadataStore.CreateRuntimeLease(ctx, sessionID)
}

func (s *Service) validateRuntimeLease(ctx context.Context, sessionID string, leaseID string) (metadata.RuntimeLeaseRecord, error) {
	if s == nil || s.metadataStore == nil {
		return metadata.RuntimeLeaseRecord{}, fmt.Errorf("metadata store is required")
	}
	record, err := s.metadataStore.ValidateRuntimeLease(ctx, sessionID, leaseID)
	if err != nil {
		if errors.Is(err, metadata.ErrInvalidRuntimeLease) {
			return metadata.RuntimeLeaseRecord{}, errors.Join(serverapi.ErrInvalidControllerLease, err)
		}
		return metadata.RuntimeLeaseRecord{}, err
	}
	return record, nil
}

func (s *Service) releaseRuntimeLease(ctx context.Context, sessionID string, leaseID string) (metadata.RuntimeLeaseRecord, error) {
	if s == nil || s.metadataStore == nil {
		return metadata.RuntimeLeaseRecord{}, fmt.Errorf("metadata store is required")
	}
	record, err := s.metadataStore.ReleaseRuntimeLease(ctx, sessionID, leaseID)
	if err != nil {
		if errors.Is(err, metadata.ErrInvalidRuntimeLease) {
			return metadata.RuntimeLeaseRecord{}, errors.Join(serverapi.ErrInvalidControllerLease, err)
		}
		return metadata.RuntimeLeaseRecord{}, err
	}
	return record, nil
}

func (s *Service) releaseRuntimeLeaseBestEffort(sessionID string, leaseID string) {
	ctx, cancel := context.WithTimeout(context.Background(), bestEffortRuntimeLeaseReleaseTimeout)
	defer cancel()
	_, _ = s.releaseRuntimeLease(ctx, sessionID, leaseID)
}

func waitForRuntimeHandleReady(ctx context.Context, handle *runtimeHandle) error {
	if handle == nil || handle.ready == nil {
		return nil
	}
	select {
	case <-handle.ready:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func waitForRuntimeHandleClosed(ctx context.Context, handle *runtimeHandle) error {
	if handle == nil || handle.closed == nil {
		return nil
	}
	select {
	case <-handle.closed:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func signalRuntimeHandleClosed(handle *runtimeHandle) {
	if handle == nil || handle.closed == nil {
		return
	}
	handle.closedOnce.Do(func() {
		close(handle.closed)
	})
}

func activationResponseForHandle(handle *runtimeHandle) (serverapi.SessionRuntimeActivateResponse, error) {
	if handle == nil {
		return serverapi.SessionRuntimeActivateResponse{}, fmt.Errorf("activate session runtime: missing runtime handle")
	}
	if handle.activationErr != nil {
		return serverapi.SessionRuntimeActivateResponse{}, handle.activationErr
	}
	leaseID := strings.TrimSpace(handle.controllerLeaseID)
	if leaseID == "" {
		return serverapi.SessionRuntimeActivateResponse{}, fmt.Errorf("activate session runtime: controller lease is unavailable")
	}
	return serverapi.SessionRuntimeActivateResponse{LeaseID: leaseID}, nil
}

func activationResponseForTakeover(takeover *runtimeTakeover) (serverapi.SessionRuntimeActivateResponse, error) {
	if takeover == nil {
		return serverapi.SessionRuntimeActivateResponse{}, fmt.Errorf("activate session runtime: missing takeover state")
	}
	if takeover.err != nil {
		return serverapi.SessionRuntimeActivateResponse{}, takeover.err
	}
	leaseID := strings.TrimSpace(takeover.leaseID)
	if leaseID == "" {
		return serverapi.SessionRuntimeActivateResponse{}, fmt.Errorf("activate session runtime: takeover lease is unavailable")
	}
	return serverapi.SessionRuntimeActivateResponse{LeaseID: leaseID}, nil
}

func parseToolIDs(raw []string) ([]toolspec.ID, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	ids := make([]toolspec.ID, 0, len(raw))
	for _, item := range raw {
		id, ok := toolspec.ParseID(item)
		if !ok {
			return nil, fmt.Errorf("unknown tool id %q", item)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

func configSourceLines(src map[string]string) []string {
	keys := make([]string, 0, len(src))
	for key := range src {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, fmt.Sprintf("%s=%s", key, strings.TrimSpace(src[key])))
	}
	return lines
}

func NewActivateRequest(clientRequestID string, sessionID string, settings config.Settings, enabledToolIDs []string, source config.SourceReport) serverapi.SessionRuntimeActivateRequest {
	id := strings.TrimSpace(clientRequestID)
	if id == "" {
		id = uuid.NewString()
	}
	return serverapi.SessionRuntimeActivateRequest{
		ClientRequestID: id,
		SessionID:       strings.TrimSpace(sessionID),
		ActiveSettings:  settings,
		EnabledToolIDs:  append([]string(nil), enabledToolIDs...),
		Source:          source,
	}
}

var _ servicecontract.SessionRuntimeService = (*Service)(nil)
