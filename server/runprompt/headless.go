package runprompt

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"core/server/launch"
	"core/server/metadata"
	"core/server/runlog"
	"core/server/runtime"
	"core/server/runtimewire"
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

	"github.com/google/uuid"
)

var ErrHeadlessGoalSession = errors.New("headless runs cannot continue sessions with goals; clear the goal first")

var ErrSessionRunning = errors.New("selected session has an active run")

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
	return &inProcessRunPromptService{launcher: launcher}
}

type headlessPromptLauncher struct {
	boot HeadlessBootstrap
}

func (l *headlessPromptLauncher) prepareHeadlessPrompt(ctx context.Context, req serverapi.RunPromptRequest, progress serverapi.RunPromptProgressSink) (*headlessPromptRuntime, error) {
	if l.boot.SessionLaunch == nil {
		return nil, errors.New("headless session launch service is required")
	}
	selectedSessionID, openingExisting := req.Intent.SessionID()
	launchReq := sessionlaunch.PlanRequest{
		Mode:            launch.ModeHeadless,
		Intent:          req.Intent,
		CallerSessionID: req.CallerSessionID,
		Overrides:       req.Overrides,
	}
	if openingExisting && strings.TrimSpace(req.Overrides.ThinkingLevel) != "" {
		var err error
		launchReq, err = l.boot.SessionLaunch.SaveRunSelection(ctx, launchReq)
		if err != nil {
			return nil, err
		}
	}
	if openingExisting && l.boot.RuntimeAuthority != nil {
		if _, active := l.boot.RuntimeAuthority.SessionExecution(selectedSessionID); active {
			return nil, ErrSessionRunning
		}
	}
	result, err := l.boot.SessionLaunch.PlanLaunchSession(ctx, launchReq)
	if err != nil {
		return nil, err
	}
	plan := result.Plan
	if plan.Goal != nil {
		return nil, fmt.Errorf("%w", ErrHeadlessGoalSession)
	}
	agentSteer, err := l.agentSteerForRunPrompt(ctx, req)
	if err != nil {
		return nil, err
	}
	runtimePlan, err := l.prepareRuntime(ctx, plan, progress, agentSteer)
	if err != nil {
		return nil, err
	}
	var sessionStarted *runpromptpb.SessionStarted
	if req.Intent.Kind() == serverapi.SessionLaunchIntentCreateNew {
		sessionID, err := uuid.Parse(runtimePlan.sessionID)
		if err != nil || sessionID.Version() != 4 {
			runtimePlan.CloseWithFailure(true)
			return nil, fmt.Errorf("new headless session id %q is not a UUIDv4", runtimePlan.sessionID)
		}
		sessionStarted = &runpromptpb.SessionStarted{SessionId: sessionID.String(), ShortId: protoapi.SessionShortIDToProto(result.Identity.ShortID)}
	}
	return &headlessPromptRuntime{
		identity:          result.Identity,
		plan:              runtimePlan,
		warnings:          result.Warnings,
		selectionWarnings: result.RunSelectionWarnings,
		progress:          progress,
		sessionStarted:    sessionStarted,
		liveRunObserver:   l.boot.LiveRunObserver,
	}, nil
}

func (l *headlessPromptLauncher) agentSteerForRunPrompt(ctx context.Context, req serverapi.RunPromptRequest) (*runtime.AgentSteer, error) {
	if req.CallerSessionID == nil {
		return nil, nil
	}
	sourceID, err := runtimeids.ParseSessionID(*req.CallerSessionID)
	if err != nil {
		return nil, err
	}
	source, err := l.boot.Metadata.SessionIdentity(ctx, sourceID)
	if err != nil {
		return nil, err
	}
	steer := runtime.NewAgentSteer(source, req.Prompt)
	return &steer, nil
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
	prompt         string
	steer          *runtime.AgentSteer
	observerCtx    context.Context
	observerCancel context.CancelFunc
}

type activeRunCapture struct {
	observerHandle *runtime.LiveRunWaitHandle
	observerCancel context.CancelFunc
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
	handle, err := l.boot.RuntimeAuthority.StartAgentExecution(ctx, sessionruntime.AgentExecutionRequest{
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
						observerHandle, observerErr := engine.CaptureActiveRunResult(submission.observerCtx)
						prepared.activeRun <- activeRunCapture{
							observerHandle: observerHandle,
							observerCancel: submission.observerCancel,
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
	})
	if err != nil {
		if errors.Is(err, sessionruntime.ErrSessionRunActive) ||
			errors.Is(err, sessionruntime.ErrSessionWorkflowActivationActive) {
			return nil, ErrSessionRunning
		}
		return nil, err
	}
	prepared.handle = handle
	return prepared, nil
}

type headlessPromptRuntime struct {
	identity          runtimeids.SessionIdentity
	plan              *headlessRuntimePlan
	warnings          []string
	selectionWarnings []runpromptpb.RunSelectionWarning
	progress          serverapi.RunPromptProgressSink
	sessionStarted    *runpromptpb.SessionStarted
	liveRunObserver   LiveRunObserver
}

func (r *headlessPromptRuntime) submitUserMessage(ctx context.Context, prompt string) (*runpromptpb.Success, error) {
	if r.plan == nil || r.plan.handle == nil {
		return &runpromptpb.Success{}, errors.Join(serverapi.ErrRuntimeUnavailable, errors.New("headless runtime is unavailable"))
	}
	sessionID, err := runtimeids.ParseSessionID(r.plan.sessionID)
	if err != nil {
		return &runpromptpb.Success{}, err
	}
	observerCtx, cancelObserver := context.WithCancel(ctx)
	defer cancelObserver()
	response := &runpromptpb.Success{
		ShortId:   protoapi.SessionShortIDToProto(r.identity.ShortID),
		SessionId: r.plan.sessionID,
		Warnings:  append([]string(nil), r.warnings...),
		SelectionWarnings: append(
			[]runpromptpb.RunSelectionWarning(nil),
			r.selectionWarnings...,
		),
	}
	r.plan.onActive = r.publishSessionStarted
	var (
		active    activeRunCapture
		hasActive bool
	)
	select {
	case r.plan.submission <- headlessPromptSubmission{
		prompt:         prompt,
		steer:          r.plan.agentSteer,
		observerCtx:    observerCtx,
		observerCancel: cancelObserver,
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
		active.observerCancel()
		if active.observerHandle != nil {
			_, _ = active.observerHandle.Wait()
		}
		return response, active.err
	}
	if r.liveRunObserver == nil {
		active.observerCancel()
		if active.observerHandle != nil {
			_, _ = active.observerHandle.Wait()
		}
		return response, errors.New("headless live run observer is required")
	}
	observed, err := r.liveRunObserver.ObserveLiveRun(
		observerCtx,
		active.observerCancel,
		sessionID,
		active.observerHandle,
	)
	if err != nil {
		return response, err
	}
	if question := observed.GetOutcome().GetQuestion(); question != nil {
		response.Outcome = &runpromptpb.Success_Question{Question: question}
		return response, nil
	}
	if _, err := r.plan.handle.Wait(ctx); err != nil {
		return response, err
	}
	switch outcome := observed.GetOutcome().GetOutcome().(type) {
	case *promptpb.LiveWatchOutcome_FinalAnswer:
		if outcome.FinalAnswer == nil || outcome.FinalAnswer.Result == nil {
			return response, errors.New("live run final answer content is required")
		}
		response.Outcome = &runpromptpb.Success_FinalAnswer{FinalAnswer: *outcome.FinalAnswer.Result}
		return response, nil
	case *promptpb.LiveWatchOutcome_NoFinalResult:
		return response, runtime.ErrLiveRunNoFinalAnswer
	case *promptpb.LiveWatchOutcome_ExecutionError:
		return response, liveWatchFailureError(outcome.ExecutionError)
	case *promptpb.LiveWatchOutcome_Interrupted:
		return response, liveWatchFailureError(outcome.Interrupted)
	default:
		return response, errors.New("live run observer returned an unsupported outcome")
	}
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
