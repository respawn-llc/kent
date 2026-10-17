package runprompt

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"core/server/launch"
	"core/server/metadata"
	"core/server/runlog"
	"core/server/runtime"
	"core/server/runtimecontrol"
	"core/server/runtimewire"
	"core/server/session"
	"core/server/sessionlaunch"
	"core/server/sessionruntime"
	askquestion "core/server/tools"
	"core/shared/apicontract"
	"core/shared/clientui"
	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runpromptpb "core/shared/protoapi/gen/kent/api/run_prompt"
	"core/shared/runtimeids"
	"core/shared/serverapi"
	"core/shared/textutil"
)

var ErrHeadlessGoalSession = errors.New("headless runs cannot continue sessions with goals; clear the goal first")

type promptHistoryStore interface {
	RecordPromptHistoryEntry(ctx context.Context, entry metadata.PromptHistoryEntry) (metadata.PromptHistoryRecord, error)
}

type LiveRunObserver interface {
	ObserveLiveRun(
		context.Context,
		context.CancelFunc,
		runtimeids.SessionID,
		*runtime.LiveRunWaitHandle,
	) (*promptpb.LiveWatchSuccess, error)
}

type HeadlessBootstrap struct {
	Metadata         *metadata.Store
	SessionLaunch    *sessionlaunch.Service
	PromptHistory    promptHistoryStore
	RuntimeAuthority *sessionruntime.Authority
	LiveRunObserver  LiveRunObserver
	// ManagedWorktreeBaseDir is the server-owned managed Worktree namespace.
	ManagedWorktreeBaseDir string
}

func NewInProcessRunPromptClient(boot HeadlessBootstrap) apicontract.RunPromptService {
	launcher := &headlessPromptLauncher{boot: boot}
	launcher.steering = runtimecontrol.NewService(boot.RuntimeAuthority, boot.Metadata).WithPromptHistoryStore(boot.PromptHistory)
	return &inProcessRunPromptService{launcher: launcher}
}

type headlessPromptLauncher struct {
	boot     HeadlessBootstrap
	steering *runtimecontrol.Service
}

func (l *headlessPromptLauncher) prepareHeadlessPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (runCompletion, error) {
	if l.boot.SessionLaunch == nil {
		return nil, errors.New("headless session launch service is required")
	}
	selectedSessionID, openingExisting := req.Intent.SessionID()
	if openingExisting {
		return l.prepareContinuation(ctx, req, progress, selectedSessionID)
	}
	launchReq := sessionlaunch.PlanRequest{
		Mode:            launch.ModeHeadless,
		Intent:          req.Intent,
		CallerSessionID: req.CallerSessionID,
		Overrides:       req.Overrides,
	}
	result, err := l.boot.SessionLaunch.PlanLaunchSession(ctx, launchReq)
	if err != nil {
		return nil, err
	}
	plan := result.Plan
	if plan.Goal != nil {
		return nil, fmt.Errorf("%w", ErrHeadlessGoalSession)
	}
	agentSteer, err := l.steering.AgentSteerForCaller(ctx, req.CallerSessionID, req.Prompt)
	if err != nil {
		return nil, err
	}
	runtimePlan, err := l.prepareRuntime(ctx, plan, progress, agentSteer)
	if err != nil {
		return nil, err
	}
	var sessionStarted *runpromptpb.SessionStarted
	if req.Intent.Kind() == serverapi.SessionLaunchIntentCreateNew {
		sessionStarted = &runpromptpb.SessionStarted{SessionId: runtimePlan.sessionID, ShortId: protoapi.SessionShortIDToProto(result.Identity.ShortID)}
	}
	return &headlessPromptRuntime{
		identity:          result.Identity,
		plan:              runtimePlan,
		warnings:          result.Warnings,
		selectionWarnings: result.RunSelectionWarnings,
		progress:          progress,
		sessionStarted:    sessionStarted,
		liveRunObserver:   l.boot.LiveRunObserver,
		ctx:               ctx, prompt: req.Prompt, history: l.boot.PromptHistory,
	}, nil
}

func (l *headlessPromptLauncher) prepareContinuation(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink, id runtimeids.SessionID) (runCompletion, error) {
	if l.boot.RuntimeAuthority == nil {
		return nil, errors.New("headless run prompt requires a session runtime authority")
	}
	steer, err := l.steering.PrepareSessionSteer(ctx, runtimeids.SessionIdentity{ID: id}, req.CallerSessionID, req.Prompt)
	if err != nil {
		return nil, err
	}
	for {
		var preparedResource *runtimeids.SessionResourceRef
		if execution, active := l.boot.RuntimeAuthority.SessionExecution(id); active {
			ref, ok := execution.Scope().Resource()
			if !ok {
				return nil, errors.New("Agent execution resource is required")
			}
			preparedResource = &ref
		}
		launchReq := sessionlaunch.PlanRequest{
			Mode: launch.ModeHeadless, Intent: req.Intent,
			CallerSessionID: req.CallerSessionID, Overrides: req.Overrides,
		}
		if preparedResource != nil {
			active, err := l.boot.SessionLaunch.PrepareActiveRun(ctx, id, req.CallerSessionID, req.Overrides.ThinkingLevel)
			if err != nil {
				return nil, err
			}
			steer.Identity = active.Identity
			prepared, changed, err := l.prepareActiveContinuation(ctx, active.Descriptor, *preparedResource, steer, progress, ignoredActiveRunSettings(req.Overrides))
			if err != nil {
				return nil, err
			}
			if changed {
				continue
			}
			return prepared, nil
		}
		if strings.TrimSpace(launchReq.Overrides.ThinkingLevel) != "" {
			var err error
			launchReq, err = l.boot.SessionLaunch.SaveRunSelection(ctx, launchReq)
			if err != nil {
				return nil, err
			}
		}
		result, err := l.boot.SessionLaunch.PlanLaunchSession(ctx, launchReq)
		if err != nil {
			return nil, err
		}
		if result.Plan.Goal != nil {
			return nil, ErrHeadlessGoalSession
		}
		steer.Identity = result.Identity
		prepared, changed, err := l.prepareContinuationRuntime(ctx, result.Plan, progress, steer.AgentSteer)
		if changed || err != nil {
			if err != nil {
				return nil, err
			}
			continue
		}
		return &headlessPromptRuntime{
			identity: result.Identity, plan: prepared, warnings: result.Warnings,
			selectionWarnings: result.RunSelectionWarnings, progress: progress,
			liveRunObserver: l.boot.LiveRunObserver,
			ctx:             ctx, prompt: req.Prompt, history: l.boot.PromptHistory,
		}, nil
	}
}

func (l *headlessPromptLauncher) prepareActiveContinuation(ctx context.Context, descriptor session.SessionDescriptor, resource runtimeids.SessionResourceRef, steer runtimecontrol.PreparedLiveSteer, progress serverapi.RunPromptProgressSink, ignored []runpromptpb.IgnoredRunSetting) (*runObservation, bool, error) {
	scope := newObservationScope(ctx)
	prepared := &runObservation{identity: steer.Identity, scope: scope, observer: l.boot.LiveRunObserver, response: runResponse(steer.Identity)}
	_, changed, err := l.boot.RuntimeAuthority.ContinueAgentExecution(ctx, sessionruntime.ActiveContinuation{
		Descriptor: descriptor, Resource: resource,
		Join: func(joinCtx context.Context, engine *runtime.Engine, accept runtime.CommandAcceptance) (bool, error) {
			stopProgress, err := l.boot.RuntimeAuthority.ObserveRuntimeEvents(resource, func(event runtime.Event) { PublishRunPromptProgress(progress, event) })
			if err != nil {
				return false, err
			}
			_, observer, accepted, err := l.steering.SubmitLiveSteer(joinCtx, runtimeids.NewQueueItemID().String(), steer, engine, scope.observerCtx, func(commit func() (bool, error)) (bool, error) {
				return accept(func() (bool, error) {
					accepted, err := commit()
					if accepted && progress != nil {
						progress.PublishRunPromptProgress(&runpromptpb.ProgressEvent{Payload: &runpromptpb.ProgressEvent_ActiveSteerAccepted{ActiveSteerAccepted: &runpromptpb.ActiveSteerAccepted{IgnoredSettings: ignored}}})
					}
					return accepted, err
				})
			})
			if accepted {
				prepared.stopProgress = stopProgress
				prepared.handle = observer
				prepared.response.SessionName = engine.SessionName()
			} else {
				stopProgress()
			}
			return accepted, err
		},
	})
	if err != nil || changed {
		scope.close()
		return nil, changed, err
	}
	return prepared, false, nil
}

func ignoredActiveRunSettings(overrides serverapi.RunPromptOverrides) []runpromptpb.IgnoredRunSetting {
	var ignored []runpromptpb.IgnoredRunSetting
	if overrides.AgentRole != nil {
		ignored = append(ignored, runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_AGENT)
	}
	if strings.TrimSpace(overrides.Model) != "" {
		ignored = append(ignored, runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_MODEL)
	}
	if strings.TrimSpace(overrides.Tools) != "" {
		ignored = append(ignored, runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_TOOLS)
	}
	if strings.TrimSpace(overrides.Theme) != "" {
		ignored = append(ignored, runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_THEME)
	}
	if overrides.ModelTimeoutSeconds > 0 {
		ignored = append(ignored, runpromptpb.IgnoredRunSetting_IGNORED_RUN_SETTING_MODEL_TIMEOUT)
	}
	return ignored
}

type headlessRuntimePlan struct {
	handle     sessionruntime.ExecutionHandle
	sessionID  string
	submission chan headlessPromptSubmission
	activeRun  chan activeRunCapture
	onActive   func()
	agentSteer *runtime.AgentSteer
}

type headlessPromptSubmission struct {
	prompt string
	steer  *runtime.AgentSteer
	scope  *observationScope
}

type activeRunCapture struct {
	observerHandle *runtime.LiveRunWaitHandle
	sessionName    *string
	err            error
}

func (p *headlessRuntimePlan) PromptHistoryText(fallback string) string {
	if p != nil && p.agentSteer != nil && p.agentSteer.Message().Content != nil {
		return *p.agentSteer.Message().Content
	}
	return fallback
}

func (p *headlessRuntimePlan) CloseWithFailure(failed bool) error {
	if p == nil || p.handle == nil {
		return nil
	}
	var stopErr error
	if failed {
		stopErr = p.handle.Stop(context.Background())
	}
	return errors.Join(stopErr, p.handle.Close(context.Background()))
}

func (l *headlessPromptLauncher) prepareRuntime(ctx context.Context, plan launch.SessionPlan, progress serverapi.RunPromptProgressSink, agentSteer *runtime.AgentSteer) (*headlessRuntimePlan, error) {
	prepared, err := l.runtimeLaunchRequest(plan, progress, agentSteer)
	if err != nil {
		return nil, err
	}
	handle, err := l.boot.RuntimeAuthority.StartAgentExecution(ctx, prepared.request)
	if err != nil {
		return nil, err
	}
	prepared.plan.handle = handle
	return prepared.plan, nil
}

type preparedRuntimeLaunch struct {
	request sessionruntime.AgentExecutionRequest
	plan    *headlessRuntimePlan
}

func (l *headlessPromptLauncher) prepareContinuationRuntime(ctx context.Context, plan launch.SessionPlan, progress serverapi.RunPromptProgressSink, agentSteer *runtime.AgentSteer) (*headlessRuntimePlan, bool, error) {
	prepared, err := l.runtimeLaunchRequest(plan, progress, agentSteer)
	if err != nil {
		return nil, false, err
	}
	handle, changed, err := l.boot.RuntimeAuthority.ContinueAgentExecution(ctx, sessionruntime.IdleContinuation{Request: prepared.request})
	if err != nil || changed {
		return nil, changed, err
	}
	prepared.plan.handle = handle
	return prepared.plan, false, nil
}

func (l *headlessPromptLauncher) runtimeLaunchRequest(plan launch.SessionPlan, progress serverapi.RunPromptProgressSink, agentSteer *runtime.AgentSteer) (*preparedRuntimeLaunch, error) {
	if l.boot.RuntimeAuthority == nil {
		return nil, errors.New("headless run prompt requires a session runtime authority")
	}
	sessionID := plan.Descriptor.SessionID()
	executionTarget := clientui.NormalizeSessionExecutionTarget(plan.ExecutionTarget)
	workdir := executionTarget.EffectiveWorkdir
	if strings.TrimSpace(workdir) == "" {
		return nil, fmt.Errorf("headless session %q execution target effective workdir is required", sessionID)
	}
	if strings.TrimSpace(executionTarget.WorkspaceRoot) == "" {
		return nil, fmt.Errorf("headless session %q execution target workspace root is required", sessionID)
	}
	executionRoot := executionTarget.WorkspaceRoot
	var currentWorktreeRoot *string
	if executionTarget.Worktree != nil && strings.TrimSpace(executionTarget.Worktree.Root) != "" {
		root := executionTarget.Worktree.Root
		currentWorktreeRoot = &root
		executionRoot = root
	}
	filesystemContext, err := runtimewire.NewFilesystemContext(workdir, executionRoot, plan.ProjectID)
	if err != nil {
		return nil, err
	}
	var managedWorktreePathContext *askquestion.ManagedWorktreePathContext
	if strings.TrimSpace(l.boot.ManagedWorktreeBaseDir) != "" {
		managedWorktreePathContext, err = askquestion.NewManagedWorktreePathContext(l.boot.ManagedWorktreeBaseDir, currentWorktreeRoot, plan.ManagedWorktreeRoots)
		if err != nil {
			return nil, err
		}
	}
	startLogLines := []string{
		fmt.Sprintf("app.run_prompt.start session_id=%s workspace=%s workdir=%s model=%s", sessionID, executionTarget.WorkspaceRoot, workdir, plan.ActiveSettings.Model),
		fmt.Sprintf("config.settings files=%+v created=%t", plan.Source.Files, plan.Source.CreatedDefaultConfig),
	}
	for _, line := range runlog.FormatConfigSourceLines(plan.Source.Sources) {
		startLogLines = append(startLogLines, "config.source "+line)
	}
	runtimePlan, err := sessionruntime.NewAgentRuntimePlan(sessionruntime.AgentRuntimePlanOptions{
		ConnectionScope:       plan.ConnectionScope,
		MainWorkspaceRoot:     executionTarget.WorkspaceRoot,
		Settings:              plan.ActiveSettings,
		EnabledTools:          plan.EnabledTools,
		FilesystemContext:     askquestion.FilesystemContext{Access: filesystemContext.Access, ManagedWorktree: managedWorktreePathContext},
		Sources:               plan.Source.Sources,
		Headless:              true,
		QuestionsEnabled:      textutil.Value(plan.QuestionsEnabled),
		AutoCompactionEnabled: textutil.Value(plan.AutoCompactionEnabled),
		AgentSelection:        plan.ActivationAgentSelection,
		ExplicitToolSelection: plan.ExplicitToolSelection,
		StartLogLines:         startLogLines,
		OnLoggingFailure: func(message string) {
			if progress != nil {
				progress.PublishRunPromptProgress(&runpromptpb.ProgressEvent{
					Payload: &runpromptpb.ProgressEvent_RunLoggingFailed{RunLoggingFailed: runPromptFailure(message)},
				})
			}
		},
		OnEvent: func(evt runtime.Event) {
			PublishRunPromptProgress(progress, evt)
		},
	})
	if err != nil {
		return nil, err
	}
	prepared := &headlessRuntimePlan{
		sessionID:  sessionID.String(),
		submission: make(chan headlessPromptSubmission),
		activeRun:  make(chan activeRunCapture, 1),
		agentSteer: agentSteer,
	}
	request := sessionruntime.AgentExecutionRequest{
		Descriptor: plan.Descriptor,
		Runtime:    &runtimePlan,
		Resource:   sessionruntime.ReplaceAgentResource{},
		Runner: func(runCtx context.Context, _ sessionruntime.ExecutionScope, bridge sessionruntime.AgentRuntimeBridge) error {
			var submission headlessPromptSubmission
			select {
			case submission = <-prepared.submission:
			case <-runCtx.Done():
				return context.Cause(runCtx)
			}
			return bridge.WithEngine(runCtx, func(engineCtx context.Context, engine *runtime.Engine) error {
				var waitHandle *runtime.LiveRunWaitHandle
				var waitStartErr error
				submit := func() error {
					onActive := func() {
						waitHandle, waitStartErr = engine.CaptureActiveRunResult(engineCtx)
						observerHandle, observerErr := engine.CaptureActiveRunResult(submission.scope.observerCtx)
						prepared.activeRun <- activeRunCapture{
							observerHandle: observerHandle,
							sessionName:    engine.SessionName(),
							err:            errors.Join(waitStartErr, observerErr),
						}
						if waitStartErr == nil && prepared.onActive != nil {
							prepared.onActive()
						}
					}
					if submission.steer != nil {
						_, err := engine.SubmitAgentSteerWithHooks(engineCtx, *submission.steer, onActive, nil)
						return err
					}
					_, err := engine.SubmitUserMessageWithHooks(engineCtx, submission.prompt, onActive, nil)
					return err
				}
				submitErr := submit()
				if waitHandle != nil {
					_, waitErr := waitHandle.Wait()
					if waitErr != nil && submitErr == nil && !errors.Is(waitErr, runtime.ErrLiveRunNoFinalAnswer) {
						submitErr = waitErr
					}
				} else if waitStartErr != nil && submitErr == nil {
					submitErr = waitStartErr
				}
				return submitErr
			})
		},
	}
	return &preparedRuntimeLaunch{request: request, plan: prepared}, nil
}

type headlessPromptRuntime struct {
	identity          runtimeids.SessionIdentity
	plan              *headlessRuntimePlan
	warnings          []string
	selectionWarnings []runpromptpb.RunSelectionWarning
	progress          serverapi.RunPromptProgressSink
	sessionStarted    *runpromptpb.SessionStarted
	liveRunObserver   LiveRunObserver
	ctx               context.Context
	prompt            string
	history           promptHistoryStore
}

func (r *headlessPromptRuntime) Complete(timeout time.Duration) (response *runpromptpb.Success, err error) {
	defer func() {
		if err == nil && response != nil && response.GetQuestion() != nil {
			return
		}
		err = errors.Join(err, r.plan.CloseWithFailure(err != nil))
	}()
	if r.history != nil {
		if _, err := r.history.RecordPromptHistoryEntry(r.ctx, metadata.PromptHistoryEntry{
			SessionID: r.plan.sessionID, Text: r.plan.PromptHistoryText(r.prompt),
		}); err != nil {
			return nil, err
		}
	}
	scope := newObservationScope(r.ctx)
	defer scope.close()
	scope.armTimeout(timeout)
	response, err = r.submitUserMessage(scope)
	return response, err
}

func (r *headlessPromptRuntime) submitUserMessage(scope *observationScope) (*runpromptpb.Success, error) {
	if r.plan == nil || r.plan.handle == nil {
		return &runpromptpb.Success{}, errors.Join(serverapi.ErrRuntimeUnavailable, errors.New("headless runtime is unavailable"))
	}
	ctx := scope.ctx
	response := runResponse(r.identity)
	response.Warnings = append([]string(nil), r.warnings...)
	response.SelectionWarnings = append([]runpromptpb.RunSelectionWarning(nil), r.selectionWarnings...)
	r.plan.onActive = r.publishSessionStarted
	var (
		active    activeRunCapture
		hasActive bool
		err       error
	)
	select {
	case r.plan.submission <- headlessPromptSubmission{
		prompt: r.prompt,
		steer:  r.plan.agentSteer,
		scope:  scope,
	}:
	case <-r.plan.handle.Done():
		active, hasActive, err = r.completedBeforeActivation(ctx)
		if err != nil {
			return response, err
		}
	case <-ctx.Done():
		return response, context.Cause(ctx)
	}
	if !hasActive {
		select {
		case active = <-r.plan.activeRun:
			hasActive = true
		case <-r.plan.handle.Done():
			active, hasActive, err = r.completedBeforeActivation(ctx)
			if err != nil {
				return response, err
			}
		case <-ctx.Done():
			return response, context.Cause(ctx)
		}
	}
	response.SessionName = active.sessionName
	if active.err != nil {
		scope.close()
		if active.observerHandle != nil {
			_, _ = active.observerHandle.Wait()
		}
		return response, active.err
	}
	observation := &runObservation{identity: r.identity, handle: active.observerHandle, scope: scope, observer: r.liveRunObserver, response: response}
	response, err = observation.observe()
	if err == nil && response.GetQuestion() == nil {
		_, err = r.plan.handle.Wait(ctx)
	}
	return response, err
}

func (r *headlessPromptRuntime) completedBeforeActivation(ctx context.Context) (activeRunCapture, bool, error) {
	select {
	case active := <-r.plan.activeRun:
		return active, true, nil
	default:
	}
	if _, err := r.plan.handle.Wait(ctx); err != nil {
		return activeRunCapture{}, false, err
	}
	return activeRunCapture{}, false, runtime.ErrNoActiveLiveRun
}

func liveWatchFailureError(failure *promptpb.LiveWatchFailure) error {
	if failure == nil {
		return errors.New("live run failed")
	}
	reason := strings.TrimSpace(failure.Reason)
	if failure.Diagnostic != nil && strings.TrimSpace(*failure.Diagnostic) != "" {
		return fmt.Errorf("%s: %s", reason, strings.TrimSpace(*failure.Diagnostic))
	}
	if reason == "" {
		return errors.New("live run failed")
	}
	return errors.New(reason)
}

func (r *headlessPromptRuntime) publishSessionStarted() {
	if r == nil || r.progress == nil || r.sessionStarted == nil {
		return
	}
	started := r.sessionStarted
	r.sessionStarted = nil
	r.progress.PublishRunPromptProgress(&runpromptpb.ProgressEvent{
		Payload: &runpromptpb.ProgressEvent_SessionStarted{SessionStarted: started},
	})
}
