package workflowrunner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"text/template"

	"builder/prompts"
	"builder/server/auth"
	"builder/server/launch"
	"builder/server/llm"
	"builder/server/metadata"
	"builder/server/runprompt"
	"builder/server/runtime"
	"builder/server/runtimeview"
	"builder/server/runtimewire"
	"builder/server/session"
	"builder/server/sessionpath"
	askquestion "builder/server/tools/askquestion"
	shelltool "builder/server/tools/shell"
	"builder/server/workflow"
	"builder/server/workflowruntime"
	"builder/server/workflowscheduler"
	"builder/server/workflowstore"
	"builder/shared/config"
	"builder/shared/serverapi"
	"builder/shared/toolspec"
	"builder/shared/transcriptdiag"
)

const (
	ReasonRuntimeCanceled = "workflow_runtime_canceled"
	ReasonRuntimeFailed   = "workflow_runtime_failed"
)

type RuntimeStore interface {
	GetRunStartContext(context.Context, workflow.RunID) (workflowstore.RunStartContext, error)
	AttachRunSession(context.Context, workflow.RunID, int64, string) error
	SetRunWaitingAsk(context.Context, workflow.RunID, int64, string) error
	ClearRunWaitingAsk(context.Context, workflow.RunID, int64, string) error
	CompleteRun(context.Context, workflowstore.CompleteRunRequest) (workflowstore.CompleteRunResult, error)
	RecordProtocolViolation(context.Context, workflowstore.RecordProtocolViolationRequest) (workflowstore.RecordProtocolViolationResult, error)
	InterruptRun(context.Context, workflow.RunID, string, string) error
	InterruptRunGeneration(context.Context, workflow.RunID, int64, string, string) error
}

type TaskWorktreeEnsurer interface {
	EnsureTaskWorktree(ctx context.Context, taskID string) error
}

type RuntimeEventRegistry interface {
	runtimewire.RuntimeRegistry
	PublishRuntimeEvent(sessionID string, evt runtime.Event)
	AwaitPromptResponse(ctx context.Context, sessionID string, req askquestion.Request) (askquestion.Response, error)
}

type Starter struct {
	cfg              config.App
	metadata         *metadata.Store
	store            RuntimeStore
	authManager      *auth.Manager
	background       *shelltool.Manager
	backgroundRouter runtimewire.BackgroundRouter
	runtimes         RuntimeEventRegistry
	storeOptions     []session.StoreOption
	clientFactory    func(workflowscheduler.StartRunRequest) llm.Client
	worktrees        TaskWorktreeEnsurer
	finished         func(workflow.RunID, int64)

	mu     sync.Mutex
	cancel map[workflow.RunID]context.CancelFunc
	task   map[workflow.RunID]workflow.TaskID
	closed bool
	wg     sync.WaitGroup
}

type StarterOptions struct {
	ClientFactory func(workflowscheduler.StartRunRequest) llm.Client
	Worktrees     TaskWorktreeEnsurer
}

func NewStarter(cfg config.App, metadataStore *metadata.Store, store RuntimeStore, authManager *auth.Manager, background *shelltool.Manager, backgroundRouter runtimewire.BackgroundRouter, runtimes RuntimeEventRegistry, opts StarterOptions) (*Starter, error) {
	if strings.TrimSpace(cfg.PersistenceRoot) == "" {
		return nil, errors.New("workflow runtime persistence root is required")
	}
	if metadataStore == nil {
		return nil, errors.New("workflow runtime metadata store is required")
	}
	if store == nil {
		return nil, errors.New("workflow runtime store is required")
	}
	return &Starter{
		cfg:              cfg,
		metadata:         metadataStore,
		store:            store,
		authManager:      authManager,
		background:       background,
		backgroundRouter: backgroundRouter,
		runtimes:         runtimes,
		storeOptions:     metadataStore.AuthoritativeSessionStoreOptions(),
		clientFactory:    opts.ClientFactory,
		worktrees:        opts.Worktrees,
		cancel:           map[workflow.RunID]context.CancelFunc{},
		task:             map[workflow.RunID]workflow.TaskID{},
	}, nil
}

func (s *Starter) SetRuntimeFinished(fn func(workflow.RunID, int64)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finished = fn
}

func (s *Starter) StartWorkflowRun(ctx context.Context, req workflowscheduler.StartRunRequest) error {
	if strings.TrimSpace(string(req.RunID)) == "" {
		return errors.New("workflow run id is required")
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("workflow runtime starter closed")
	}
	s.mu.Unlock()
	if s.worktrees != nil {
		if err := s.worktrees.EnsureTaskWorktree(ctx, string(req.TaskID)); err != nil {
			return err
		}
	}
	input, err := s.store.GetRunStartContext(ctx, req.RunID)
	if err != nil {
		return err
	}
	if strings.TrimSpace(input.WorktreeID) == "" || strings.TrimSpace(input.WorktreeRoot) == "" {
		return fmt.Errorf("workflow task %q has no managed worktree", input.Task.ID)
	}
	if input.Run.Generation != req.Generation {
		return fmt.Errorf("stale workflow run generation: got %d want %d", req.Generation, input.Run.Generation)
	}
	if err := s.validateRole(input.Node.SubagentRole); err != nil {
		return err
	}
	plan, warnings, err := s.planSession(ctx, input)
	if err != nil {
		return err
	}
	if err := plan.Store.SetWorktreeReminderState(&session.WorktreeReminderState{
		Mode:          session.WorktreeReminderModeEnter,
		WorktreePath:  input.WorktreeRoot,
		WorkspaceRoot: input.WorkspaceRoot,
		EffectiveCwd:  input.WorktreeRoot,
	}); err != nil {
		return errors.Join(err, s.cleanupSession(ctx, plan.Store))
	}
	runCtx, cancel := context.WithCancel(context.Background())
	if !s.registerRun(req, cancel) {
		cancel()
		return errors.Join(errors.New("workflow runtime starter closed"), s.cleanupSession(ctx, plan.Store))
	}
	if err := s.metadata.UpdateSessionExecutionTargetByID(ctx, plan.Store.Meta().SessionID, input.WorkspaceID, input.WorktreeID, "."); err != nil {
		cancel()
		s.releaseRegisteredRun(req.RunID)
		return errors.Join(err, s.cleanupSession(ctx, plan.Store))
	}
	if err := s.store.AttachRunSession(ctx, req.RunID, req.Generation, plan.Store.Meta().SessionID); err != nil {
		cancel()
		s.releaseRegisteredRun(req.RunID)
		return errors.Join(err, s.cleanupSession(ctx, plan.Store))
	}
	go s.run(runCtx, req, input, plan, warnings)
	return nil
}

func (s *Starter) registerRun(req workflowscheduler.StartRunRequest, cancel context.CancelFunc) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.cancel[req.RunID] = cancel
	s.task[req.RunID] = req.TaskID
	s.wg.Add(1)
	return true
}

func (s *Starter) releaseRegisteredRun(runID workflow.RunID) {
	s.mu.Lock()
	delete(s.cancel, runID)
	delete(s.task, runID)
	s.mu.Unlock()
	s.wg.Done()
}

func (s *Starter) cleanupSession(ctx context.Context, store *session.Store) error {
	if store == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	cleanupCtx := context.WithoutCancel(ctx)
	sessionID := store.Meta().SessionID
	return errors.Join(store.RemoveDurable(), s.metadata.DeleteSessionRecordByID(cleanupCtx, sessionID))
}

func (s *Starter) Close() error {
	s.mu.Lock()
	s.closed = true
	cancels := make([]context.CancelFunc, 0, len(s.cancel))
	for _, cancel := range s.cancel {
		cancels = append(cancels, cancel)
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	s.wg.Wait()
	return nil
}

func (s *Starter) CancelTaskRuns(ctx context.Context, taskID workflow.TaskID) error {
	s.mu.Lock()
	cancels := []context.CancelFunc{}
	for runID, cancel := range s.cancel {
		if s.task[runID] == taskID && cancel != nil {
			cancels = append(cancels, cancel)
		}
	}
	s.mu.Unlock()
	for _, cancel := range cancels {
		cancel()
	}
	return nil
}

func (s *Starter) CancelRun(ctx context.Context, runID workflow.RunID) error {
	s.mu.Lock()
	cancel := s.cancel[runID]
	s.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	return nil
}

func (s *Starter) planSession(ctx context.Context, input workflowstore.RunStartContext) (launch.SessionPlan, []string, error) {
	cfg := s.cfg
	cfg.WorkspaceRoot = strings.TrimSpace(input.WorkspaceRoot)
	projectID := strings.TrimSpace(input.Task.ProjectID)
	if projectID == "" {
		return launch.SessionPlan{}, nil, errors.New("workflow task project id is required")
	}
	containerDir := config.ProjectSessionsRoot(cfg, projectID)
	planner := launch.Planner{
		Config:       cfg,
		ContainerDir: containerDir,
		StoreOptions: s.storeOptions,
		MetadataStoreOpener: func(string) (launch.MetadataExecutionTargetStore, error) {
			return s.metadata, nil
		},
	}
	if strings.TrimSpace(input.Run.SessionID) != "" {
		plan, err := planner.PlanSession(ctx, launch.SessionRequest{Mode: launch.ModeHeadless, SelectedSessionID: input.Run.SessionID})
		if err != nil {
			return launch.SessionPlan{}, nil, err
		}
		if err := plan.Store.EnsureDurable(); err != nil {
			return launch.SessionPlan{}, nil, err
		}
		return plan, nil, nil
	}
	var plan launch.SessionPlan
	var err error
	switch input.ContextMode {
	case "", workflow.ContextModeNewSession:
		plan, err = planner.PlanSession(ctx, launch.SessionRequest{Mode: launch.ModeHeadless, ForceNewSession: true})
	case workflow.ContextModeContinueSession:
		if strings.TrimSpace(input.SourceSessionID) == "" {
			return launch.SessionPlan{}, nil, errors.New("continue_session requires a source session")
		}
		if strings.TrimSpace(input.SourceNode.SubagentRole) != strings.TrimSpace(input.Node.SubagentRole) {
			return launch.SessionPlan{}, nil, fmt.Errorf("continue_session requires same subagent role: source %q target %q", input.SourceNode.SubagentRole, input.Node.SubagentRole)
		}
		plan, err = planner.PlanSession(ctx, launch.SessionRequest{Mode: launch.ModeHeadless, SelectedSessionID: input.SourceSessionID})
	case workflow.ContextModeCompactAndContinueSession:
		if strings.TrimSpace(input.SourceSessionID) == "" {
			return launch.SessionPlan{}, nil, errors.New("compact_and_continue_session requires a source session")
		}
		if strings.TrimSpace(input.SourceNode.SubagentRole) != strings.TrimSpace(input.Node.SubagentRole) {
			return launch.SessionPlan{}, nil, fmt.Errorf("compact_and_continue_session requires same subagent role: source %q target %q", input.SourceNode.SubagentRole, input.Node.SubagentRole)
		}
		// In-place continuation reuses the source session; the runtime runs a real
		// compaction before the node turn. A fan-out branch instead continues in an
		// isolated full clone of the source so parallel branches never compact or
		// mutate the shared source session concurrently.
		continuationSessionID := input.SourceSessionID
		if input.IsFanoutBranch {
			continuationSessionID, err = s.cloneSourceSessionForFanout(containerDir, input.SourceSessionID)
			if err != nil {
				return launch.SessionPlan{}, nil, err
			}
		}
		plan, err = planner.PlanSession(ctx, launch.SessionRequest{Mode: launch.ModeHeadless, SelectedSessionID: continuationSessionID})
	default:
		return launch.SessionPlan{}, nil, fmt.Errorf("unsupported workflow context mode %q", input.ContextMode)
	}
	if err != nil {
		return launch.SessionPlan{}, nil, err
	}
	warnings := []string{}
	if input.ContextMode == "" || input.ContextMode == workflow.ContextModeNewSession {
		plan, warnings, err = launch.ApplyRunPromptOverrides(plan, serverapi.RunPromptOverrides{AgentRole: input.Node.SubagentRole}, auth.EmptyState())
		if err != nil {
			return launch.SessionPlan{}, nil, err
		}
	}
	if err := plan.Store.EnsureDurable(); err != nil {
		return launch.SessionPlan{}, nil, err
	}
	return plan, warnings, nil
}

// cloneSourceSessionForFanout creates an isolated full clone of the source
// session for a fan-out compact-and-continue branch and returns its session ID,
// so the branch can be compacted/continued without touching the shared source.
func (s *Starter) cloneSourceSessionForFanout(containerDir, sourceSessionID string) (string, error) {
	sourceDir, err := sessionpath.ResolveScopedSessionDir(containerDir, sourceSessionID)
	if err != nil {
		return "", fmt.Errorf("resolve source session dir: %w", err)
	}
	sourceStore, err := session.Open(sourceDir, s.storeOptions...)
	if err != nil {
		return "", fmt.Errorf("open source session: %w", err)
	}
	cloned, err := session.CloneSession(sourceStore, "")
	if err != nil {
		return "", fmt.Errorf("clone source session: %w", err)
	}
	return cloned.Meta().SessionID, nil
}

func (s *Starter) validateRole(role string) error {
	trimmed := strings.TrimSpace(role)
	if workflow.IsDefaultAgentRole(trimmed) {
		return nil
	}
	for _, available := range config.AvailableSubagentRoleNames(s.cfg.Settings, false) {
		if available == trimmed {
			return nil
		}
	}
	return fmt.Errorf("workflow validation failed: [%s]", workflow.CodeAgentRoleMissing)
}

func (s *Starter) run(ctx context.Context, req workflowscheduler.StartRunRequest, input workflowstore.RunStartContext, plan launch.SessionPlan, warnings []string) {
	defer s.wg.Done()
	defer s.finish(req.RunID, req.Generation)
	logger, err := runprompt.NewRunLogger(plan.Store.Dir(), nil)
	if err != nil {
		s.interrupt(context.Background(), req.RunID, req.Generation, ReasonRuntimeFailed, err)
		return
	}
	defer func() { _ = logger.Close() }()
	logger.Logf("workflow.runtime.start run_id=%s task_id=%s session_id=%s node_id=%s worktree=%s model=%s", req.RunID, req.TaskID, plan.Store.Meta().SessionID, req.NodeID, input.WorktreeRoot, plan.ActiveSettings.Model)
	for _, warning := range warnings {
		logger.Logf("workflow.runtime.warning %s", warning)
	}
	client := llm.Client(nil)
	if s.clientFactory != nil {
		client = s.clientFactory(req)
	}
	instructions, instructionsErr := BuildWorkflowTaskInstructions(input)
	if instructionsErr != nil {
		s.interrupt(context.Background(), req.RunID, req.Generation, ReasonRuntimeFailed, instructionsErr)
		return
	}
	wiring, err := runtimewire.NewRuntimeWiringWithBackground(plan.Store, plan.ActiveSettings, workflowRuntimeEnabledTools(plan.EnabledTools), input.WorktreeRoot, s.authManager, logger, s.background, runtimewire.RuntimeWiringOptions{
		Headless: true,
		FastMode: nil,
		Sources:  plan.Source.Sources,
		Client:   client,
		WorkflowRun: &workflowruntime.Config{
			Contract:                     workflowCompletionContract(req, input),
			CompletionMode:               s.cfg.Settings.Workflow.CompletionMode,
			MaxFinalAnswerViolations:     s.cfg.Settings.Workflow.MaxFinalAnswerViolations,
			MaxInvalidCompletionAttempts: s.cfg.Settings.Workflow.MaxInvalidCompletionAttempts,
			Controller:                   workflowruntime.StoreController{Store: s.store},
			Instructions:                 instructions,
		},
		OnEvent: func(evt runtime.Event) {
			logger.Logf("%s", runprompt.FormatRuntimeEvent(evt))
			if transcriptdiag.EnabledForProcess(plan.ActiveSettings.Debug) {
				projected := runtimeview.EventFromRuntime(evt)
				logger.Logf("%s", runprompt.FormatTranscriptProjectionDiagnostic(plan.Store.Meta().SessionID, projected))
				logger.Logf("%s", runprompt.FormatTranscriptPublishDiagnostic(plan.Store.Meta().SessionID, projected))
			}
			if s.runtimes != nil {
				s.runtimes.PublishRuntimeEvent(plan.Store.Meta().SessionID, evt)
			}
		},
	})
	if err != nil {
		s.interrupt(context.Background(), req.RunID, req.Generation, ReasonRuntimeFailed, err)
		return
	}
	defer func() { _ = wiring.Close() }()
	var runtimeRegistry runtimewire.RuntimeRegistry
	if s.runtimes != nil {
		runtimeRegistry = s.runtimes
	}
	if wiring.AskBroker != nil && s.runtimes != nil {
		sessionID := plan.Store.Meta().SessionID
		wiring.AskBroker.SetAskHandler(func(askReq askquestion.Request) (askquestion.Response, error) {
			if err := s.store.SetRunWaitingAsk(context.Background(), req.RunID, req.Generation, askReq.ID); err != nil {
				return askquestion.Response{}, err
			}
			resp, askErr := s.runtimes.AwaitPromptResponse(ctx, sessionID, askReq)
			if clearErr := s.store.ClearRunWaitingAsk(context.Background(), req.RunID, req.Generation, askReq.ID); clearErr != nil && askErr == nil {
				return askquestion.Response{}, clearErr
			}
			return resp, askErr
		})
	} else if wiring.AskBroker != nil {
		wiring.AskBroker.SetAskHandler(func(askquestion.Request) (askquestion.Response, error) {
			return askquestion.Response{}, errors.New("workflow questions require runtime registry")
		})
	}
	registration := runtimewire.RegisterSessionRuntime(plan.Store.Meta().SessionID, wiring.Engine, runtimeRegistry, s.backgroundRouter)
	defer registration.Close()
	if input.ContextMode == workflow.ContextModeCompactAndContinueSession {
		if err := wiring.Engine.CompactContext(ctx, ""); err != nil {
			reason := ReasonRuntimeFailed
			if errors.Is(err, context.Canceled) || ctx.Err() != nil {
				reason = ReasonRuntimeCanceled
			}
			s.interrupt(context.Background(), req.RunID, req.Generation, reason, err)
			return
		}
	}
	if _, err := wiring.Engine.SubmitWorkflowTurn(ctx); err != nil {
		reason := ReasonRuntimeFailed
		if errors.Is(err, context.Canceled) || ctx.Err() != nil {
			reason = ReasonRuntimeCanceled
		}
		s.interrupt(context.Background(), req.RunID, req.Generation, reason, err)
	}
}

func (s *Starter) finish(runID workflow.RunID, generation int64) {
	s.mu.Lock()
	delete(s.cancel, runID)
	delete(s.task, runID)
	finished := s.finished
	s.mu.Unlock()
	if finished != nil {
		finished(runID, generation)
	}
}

func workflowRuntimeEnabledTools(enabled []toolspec.ID) []toolspec.ID {
	out := make([]toolspec.ID, 0, len(enabled))
	for _, id := range enabled {
		out = append(out, id)
	}
	return out
}

func (s *Starter) interrupt(ctx context.Context, runID workflow.RunID, generation int64, reason string, cause error) {
	detail := "{}"
	if cause != nil {
		if raw, err := json.Marshal(map[string]string{"error": cause.Error()}); err == nil {
			detail = string(raw)
		}
	}
	if err := s.store.InterruptRunGeneration(ctx, runID, generation, reason, detail); err != nil {
		return
	}
}

func BuildWorkflowTaskInstructions(input workflowstore.RunStartContext) (workflowruntime.TaskInstructions, error) {
	nodePrompt, err := renderTransitionPrompt(input.PromptTemplate, input)
	if err != nil {
		return workflowruntime.TaskInstructions{}, err
	}
	taskShortID := strings.TrimSpace(input.Task.ShortID)
	if taskShortID == "" {
		taskShortID = string(input.Task.ID)
	}
	workflowShortID := strings.TrimSpace(string(input.Workflow.ID))
	if workflowShortID == "" {
		workflowShortID = string(input.Task.WorkflowID)
	}
	return workflowruntime.TaskInstructions{
		TaskID:          string(input.Task.ID),
		TaskShortID:     taskShortID,
		TaskTitle:       strings.TrimSpace(input.Task.Title),
		TaskBody:        strings.TrimSpace(input.Task.Body),
		WorkflowID:      string(input.Task.WorkflowID),
		WorkflowShortID: workflowShortID,
		NodeID:          string(input.Node.ID),
		NodeKey:         string(input.Node.Key),
		NodeDisplayName: strings.TrimSpace(input.Node.DisplayName),
		ContextMode:     string(input.ContextMode),
		SourceSessionID: strings.TrimSpace(input.SourceSessionID),
		Transitions:     workflowInstructionTransitions(input.TransitionOptions, input.TransitionIDs),
		NodePrompt:      nodePrompt,
	}, nil
}

func workflowTransitions(options []workflowstore.TransitionOption, transitionIDs []string) []prompts.WorkflowTransition {
	capacity := len(options)
	if len(transitionIDs) > capacity {
		capacity = len(transitionIDs)
	}
	out := make([]prompts.WorkflowTransition, 0, capacity)
	if len(options) > 0 {
		for _, option := range options {
			id := strings.TrimSpace(option.ID)
			if id == "" {
				continue
			}
			out = append(out, prompts.WorkflowTransition{ID: id, DisplayName: strings.TrimSpace(option.DisplayName), Description: strings.TrimSpace(option.Description)})
		}
		return out
	}
	for _, id := range transitionIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			out = append(out, prompts.WorkflowTransition{ID: trimmed})
		}
	}
	return out
}

func workflowInstructionTransitions(options []workflowstore.TransitionOption, transitionIDs []string) []workflowruntime.TransitionInstruction {
	transitions := workflowTransitions(options, transitionIDs)
	out := make([]workflowruntime.TransitionInstruction, 0, len(transitions))
	for _, transition := range transitions {
		out = append(out, workflowruntime.TransitionInstruction{ID: transition.ID, DisplayName: transition.DisplayName, Description: transition.Description})
	}
	return out
}

func workflowCompletionContract(req workflowscheduler.StartRunRequest, input workflowstore.RunStartContext) workflowruntime.CompletionContract {
	return workflowruntime.CompletionContract{
		RunID:              req.RunID,
		ExpectedGeneration: req.Generation,
		RequireGeneration:  true,
		Transitions:        workflowCompletionTransitions(input.TransitionOptions, input.TransitionIDs),
	}
}

func workflowCompletionTransitions(options []workflowstore.TransitionOption, transitionIDs []string) []workflowruntime.CompletionTransition {
	out := make([]workflowruntime.CompletionTransition, 0, len(options))
	if len(options) > 0 {
		for _, option := range options {
			id := strings.TrimSpace(option.ID)
			if id == "" {
				continue
			}
			out = append(out, workflowruntime.CompletionTransition{
				ID:          id,
				DisplayName: strings.TrimSpace(option.DisplayName),
				Description: strings.TrimSpace(option.Description),
				Parameters:  append([]workflow.Parameter(nil), option.Parameters...),
			})
		}
		return out
	}
	for _, id := range transitionIDs {
		trimmed := strings.TrimSpace(id)
		if trimmed != "" {
			out = append(out, workflowruntime.CompletionTransition{ID: trimmed})
		}
	}
	return out
}

type nodePromptTemplateData struct {
	TaskId          string
	TaskShortId     string
	TaskTitle       string
	TaskBody        string
	NodeId          string
	NodeKey         string
	NodeDisplayName string
	Params          map[string]promptParameterNamespace
}

const currentParameterValueKey = "\x00current"

type promptParameterNamespace map[string]string

func (n promptParameterNamespace) String() string {
	return n[currentParameterValueKey]
}

func renderTransitionPrompt(templateText string, input workflowstore.RunStartContext) (string, error) {
	prompt := strings.TrimSpace(templateText)
	if prompt == "" {
		return "", nil
	}
	tmpl, err := template.New("workflow transition prompt").Option("missingkey=error").Parse(prompt)
	if err != nil {
		return "", fmt.Errorf("parse workflow transition prompt template: %w", err)
	}
	var b strings.Builder
	if err := tmpl.Execute(&b, nodePromptTemplateData{
		TaskId:          string(input.Task.ID),
		TaskShortId:     strings.TrimSpace(input.Task.ShortID),
		TaskTitle:       strings.TrimSpace(input.Task.Title),
		TaskBody:        strings.TrimSpace(input.Task.Body),
		NodeId:          string(input.Node.ID),
		NodeKey:         string(input.Node.Key),
		NodeDisplayName: strings.TrimSpace(input.Node.DisplayName),
		Params:          promptParameterData(input.ParameterValues, input.PriorParameterValues),
	}); err != nil {
		return "", fmt.Errorf("render workflow transition prompt template: %w", err)
	}
	return b.String(), nil
}

func promptParameterData(current map[string]string, prior map[string]map[string]string) map[string]promptParameterNamespace {
	out := map[string]promptParameterNamespace{}
	for transitionKey, values := range prior {
		key := strings.TrimSpace(transitionKey)
		if key == "" {
			continue
		}
		namespace := out[key]
		if namespace == nil {
			namespace = promptParameterNamespace{}
		}
		for parameterKey, value := range values {
			trimmedParameterKey := strings.TrimSpace(parameterKey)
			if trimmedParameterKey != "" {
				namespace[trimmedParameterKey] = value
			}
		}
		out[key] = namespace
	}
	for parameterKey, value := range current {
		key := strings.TrimSpace(parameterKey)
		if key == "" {
			continue
		}
		namespace := out[key]
		if namespace == nil {
			namespace = promptParameterNamespace{}
		}
		namespace[currentParameterValueKey] = value
		out[key] = namespace
	}
	return out
}

var _ workflowscheduler.RuntimeStarter = (*Starter)(nil)
