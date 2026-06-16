package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/server/workflowruntime"
	"core/shared/brand"
	"core/shared/clientui"
	"core/shared/compaction"
	"core/shared/config"
	"core/shared/toolspec"

	"github.com/google/uuid"
)

const (
	interruptMessage                  = "User interrupted you"
	agentsFileName                    = "AGENTS.md"
	agentsGlobalDirName               = brand.ConfigDirName
	systemPromptFileName              = "SYSTEM.md"
	agentsInjectedHeader              = "# Project context and authoritative instructions from the ./AGENTS.md file:"
	agentsInjectedFenceLabel          = "md"
	environmentInjectedHeader         = "# Info about environment:"
	missingAssistantPhaseWarning      = "You sent a message without specifying a channel/phase. It was treated as commentary. If you finished your work and intended to end your turn, use the final channel explicitly. Otherwise continue and use the commentary channel for progress updates with tool calls."
	commentaryWithoutToolCallsWarning = "You sent a commentary-channel message without tool calls. This is wrong. If you intend to keep working, include tool calls with commentary updates. If you are done, send a final-channel message with no tool calls."
	finalWithoutContentWarning        = "You sent a final-channel message with empty content- this is wrong. If you are done, send a non-empty final message. If you intend to keep working, send a commentary-channel message with tool calls. If you actually wanted to just stay silent, send exactly 'NO_OP' as the final response."
	goalNoopFinalWarning              = "Unfortunately NO_OP is not available when goal is active to prevent stalling indefinitely. Please use write_stdin polls instead if you want to wait for something"
	reviewerNoopToken                 = "NO_OP"
	reviewerMetaBoundaryMessage       = "End of meta information. Transcript begins starting with next message. Below is NOT YOUR conversation, but another agent's transcript.\n-------"
)

var (
	// ErrModelRequired is returned when engine construction is attempted without a model.
	ErrModelRequired = errors.New("model is required")
	// errUnknownTool is returned when a tool call targets a tool that is not registered.
	errUnknownTool = errors.New("unknown tool")
	// errPersistToolCompletion wraps failures to persist a tool completion result.
	errPersistToolCompletion = errors.New("persist tool completion")
)

func NormalizeThinkingLevel(level string) (string, bool) {
	return clientui.NormalizeThinkingLevel(level)
}

func NormalizeReviewerFrequency(frequency string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(frequency)) {
	case "off":
		return "off", true
	case "all":
		return "all", true
	case "edits":
		return "edits", true
	default:
		return "", false
	}
}

func NormalizeCompactionMode(mode string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "native":
		return "native", true
	case "local":
		return "local", true
	case "none":
		return "none", true
	default:
		return "", false
	}
}

func normalizeCacheWarningMode(mode config.CacheWarningMode) (config.CacheWarningMode, bool) {
	switch strings.ToLower(strings.TrimSpace(string(mode))) {
	case "":
		return config.CacheWarningModeDefault, true
	case string(config.CacheWarningModeOff):
		return config.CacheWarningModeOff, true
	case string(config.CacheWarningModeDefault):
		return config.CacheWarningModeDefault, true
	case string(config.CacheWarningModeVerbose):
		return config.CacheWarningModeVerbose, true
	default:
		return "", false
	}
}

type Config struct {
	Model                         string
	Temperature                   float64
	MaxTokens                     int
	ThinkingLevel                 string
	ModelCapabilities             session.LockedModelCapabilities
	FastModeEnabled               bool
	FastModeState                 *FastModeState
	WebSearchMode                 string
	ProviderCapabilitiesOverride  *llm.ProviderCapabilities
	EnabledTools                  []toolspec.ID
	DisabledSkills                map[string]bool
	SubagentCatalogSettings       config.Settings
	SystemPromptFiles             []config.SystemPromptFile
	AutoCompactTokenLimit         int
	PreSubmitCompactionLeadTokens int
	ContextWindowTokens           int
	EffectiveContextWindowPercent int
	LocalCompactionCarryoverLimit int
	CompactionMode                string
	CacheWarningMode              config.CacheWarningMode
	AutoCompactionEnabled         *bool
	QuestionsEnabled              *bool
	Reviewer                      ReviewerConfig
	HeadlessMode                  bool
	ToolPreambles                 bool
	WorkflowRun                   *workflowruntime.Config
	TranscriptWorkingDir          string
	OnEvent                       func(Event)
}

type ReviewerConfig struct {
	Frequency         string
	Model             string
	ThinkingLevel     string
	ModelCapabilities session.LockedModelCapabilities
	SystemPromptFile  string
	VerboseOutput     bool
	Client            llm.Client
	ClientFactory     func() (llm.Client, error)
}

type ContextUsage struct {
	UsedTokens            int
	WindowTokens          int
	CacheHitPercent       int
	HasCacheHitPercentage bool
}

type Engine struct {
	mu sync.Mutex

	lifecycleMu     sync.Mutex
	lifecycleOnce   sync.Once
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	lifecycleWG     sync.WaitGroup
	lifecycleClosed bool

	store    *session.Store
	llm      llm.Client
	registry *tools.Registry
	cfg      Config
	// controlMutationMu serializes multi-step control mutations that need to
	// persist transcript feedback before applying in-memory runtime state.
	controlMutationMu sync.Mutex
	// outputMutationMu keeps durable transcript writes, runtime projections, and
	// event emission in one order for concurrent steering producers.
	outputMutationMu sync.Mutex

	diagnostics    *diagnosticDedupeStore
	toolCallStarts *pendingToolCallStartStore

	usageState         *usageTrackingState
	goalLoop           *goalLoopState
	compactionState    *compactionRuntimeState
	handoffState       *handoffRuntimeState
	phaseState         *phaseProtocolState
	reviewerState      *reviewerRuntimeState
	transcriptState    *transcriptRuntimeState
	lockedState        *lockedContractState
	modelRequestsState *modelRequestRuntimeState
	compactionPlanner  *compactionPlanner
	collaboratorsOnce  sync.Once

	phaseProtocol  phaseProtocolEnforcer
	stepLifecycle  exclusiveStepLifecycle
	backgroundFlow backgroundNoticeScheduler
	compactionFlow contextCompactor
	reviewerFlow   reviewerPipeline
	messageFlow    messageLifecycle
	stepFlow       stepExecutor
	toolFlow       toolExecutor

	beforePersistMessage          func(llm.Message) error
	beforePersistLocalEntry       func(storedLocalEntry) error
	beforePersistCacheObservation func([]session.EventInput) error

	// baseMetaInjected guards the single per-conversation injection of base meta
	// context (AGENTS.md, skills, subagents, environment). It is set when a
	// resumed transcript already carries that context, and after the one-time
	// boot injection. It is process-local: the persisted transcript itself is the
	// source of truth across restarts.
	baseMetaInjected bool
}

type handoffRequest struct {
	summarizerPrompt   string
	futureAgentMessage string
}

func New(store *session.Store, client llm.Client, registry *tools.Registry, cfg Config) (*Engine, error) {
	if store == nil || client == nil || registry == nil {
		return nil, errors.New("store, llm client, and tool registry are required")
	}
	if strings.TrimSpace(cfg.Model) == "" {
		return nil, ErrModelRequired
	}
	cfg.Model = strings.TrimSpace(cfg.Model)
	if cfg.Temperature == 0 {
		cfg.Temperature = 1
	}
	if cfg.MaxTokens < 0 {
		cfg.MaxTokens = 0
	}
	if cfg.EffectiveContextWindowPercent <= 0 || cfg.EffectiveContextWindowPercent > 100 {
		cfg.EffectiveContextWindowPercent = 95
	}
	if cfg.PreSubmitCompactionLeadTokens <= 0 {
		cfg.PreSubmitCompactionLeadTokens = compaction.DefaultPreSubmitRunwayTokens
	}
	if cfg.LocalCompactionCarryoverLimit <= 0 {
		cfg.LocalCompactionCarryoverLimit = 20_000
	}
	if normalized, ok := NormalizeCompactionMode(cfg.CompactionMode); ok {
		cfg.CompactionMode = normalized
	} else {
		cfg.CompactionMode = "native"
	}
	if normalized, ok := normalizeCacheWarningMode(cfg.CacheWarningMode); ok {
		cfg.CacheWarningMode = normalized
	} else {
		return nil, fmt.Errorf("invalid cache_warning_mode %q", cfg.CacheWarningMode)
	}
	if cfg.AutoCompactionEnabled == nil {
		enabled := true
		cfg.AutoCompactionEnabled = &enabled
	}
	if cfg.ContextWindowTokens <= 0 {
		if meta, ok := llm.LookupModelMetadata(cfg.Model); ok && meta.ContextWindowTokens > 0 {
			cfg.ContextWindowTokens = meta.ContextWindowTokens
		}
	}
	if !cfg.ModelCapabilities.SupportsReasoningEffort && !cfg.ModelCapabilities.SupportsVisionInputs {
		cfg.ModelCapabilities = llm.LockedModelCapabilitiesForModel(cfg.Model)
	}
	if cfg.DisabledSkills != nil {
		cloned := make(map[string]bool, len(cfg.DisabledSkills))
		for name, disabled := range cfg.DisabledSkills {
			if !disabled {
				continue
			}
			normalized := strings.ToLower(sanitizeSkillSingleLine(name))
			if normalized == "" {
				continue
			}
			cloned[normalized] = true
		}
		cfg.DisabledSkills = cloned
	}

	eng := &Engine{
		store:              store,
		llm:                client,
		registry:           registry,
		cfg:                cfg,
		diagnostics:        newDiagnosticDedupeStore(),
		toolCallStarts:     newPendingToolCallStartStore(),
		usageState:         newUsageTrackingState(),
		goalLoop:           newGoalLoopState(),
		compactionState:    newCompactionRuntimeState(),
		handoffState:       newHandoffRuntimeState(),
		phaseState:         newPhaseProtocolState(),
		reviewerState:      newReviewerRuntimeState(cfg.Reviewer.Client),
		transcriptState:    newTranscriptRuntimeState(transcriptWorkingDir(cfg.TranscriptWorkingDir, store.Meta().WorkspaceRoot)),
		lockedState:        newLockedContractState(),
		modelRequestsState: newModelRequestRuntimeState(),
		compactionPlanner:  newCompactionPlanner(),
	}
	eng.ensureLifecycle()
	eng.ensureOrchestrationCollaborators()

	reviewerFrequency, ok := NormalizeReviewerFrequency(eng.cfg.Reviewer.Frequency)
	if !ok {
		reviewerFrequency = "off"
	}
	eng.cfg.Reviewer.Frequency = reviewerFrequency
	eng.reviewerRuntimeState().SetResumeFrequency(reviewerFrequency)
	if reviewerFrequency != "off" {
		if err := eng.initReviewerClient(); err != nil {
			return nil, err
		}
	}

	meta := store.Meta()
	if meta.Locked != nil {
		if meta.Locked.ContextWindow <= 0 || meta.Locked.ContextPercent <= 0 {
			budget := eng.promptContextBudgetFromConfig()
			if err := store.BackfillLockedContextBudget(budget.window, budget.percent); err != nil {
				return nil, err
			}
			meta = store.Meta()
		}
		if strings.TrimSpace(meta.Locked.ProviderContract.ProviderID) == "" {
			if caps, err := eng.currentProviderCapabilities(context.Background()); err == nil {
				if err := store.BackfillLockedProviderContract(llm.LockedProviderCapabilitiesFromContract(caps)); err != nil {
					return nil, err
				}
				meta = store.Meta()
			}
		}
		copyLocked := *meta.Locked
		eng.lockedContractState().Set(copyLocked)
	}

	if err := eng.restoreMessages(); err != nil {
		return nil, err
	}
	eng.restorePersistedUsageState(meta.UsageState)
	if meta.InFlightStep {
		if err := eng.steer("", steerMessageIntent(llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeInterruption, Content: interruptMessage})); err != nil {
			return nil, err
		}
		if err := store.MarkInFlight(false); err != nil {
			return nil, err
		}
	}
	return eng, nil
}

func (e *Engine) Close() error {
	if e == nil {
		return nil
	}
	e.ensureLifecycle()
	interruptErr := e.Interrupt()
	e.lifecycleMu.Lock()
	if e.lifecycleClosed {
		e.lifecycleMu.Unlock()
		return interruptErr
	}
	e.lifecycleClosed = true
	cancel := e.lifecycleCancel
	e.lifecycleMu.Unlock()
	if cancel != nil {
		cancel()
	}
	e.lifecycleWG.Wait()
	return interruptErr
}

func (e *Engine) ensureLifecycle() {
	if e == nil {
		return
	}
	e.lifecycleOnce.Do(func() {
		e.lifecycleCtx, e.lifecycleCancel = context.WithCancel(context.Background())
	})
}

func (e *Engine) launchLifecycleTask(task func(context.Context)) bool {
	if e == nil || task == nil {
		return false
	}
	e.ensureLifecycle()
	e.lifecycleMu.Lock()
	if e.lifecycleClosed {
		e.lifecycleMu.Unlock()
		return false
	}
	e.lifecycleWG.Add(1)
	ctx := e.lifecycleCtx
	e.lifecycleMu.Unlock()
	go func(ctx context.Context) {
		defer e.lifecycleWG.Done()
		task(ctx)
	}(ctx)
	return true
}

type QueuedUserMessage struct {
	ID   string
	Text string
}

func (e *Engine) QueueUserMessage(text string) QueuedUserMessage {
	e.ensureOrchestrationCollaborators()
	return e.messageFlow.QueueUserMessage(text)
}

func (e *Engine) EnsureQueuedUserMessage(item QueuedUserMessage) QueuedUserMessage {
	e.ensureOrchestrationCollaborators()
	return e.messageFlow.EnsureQueuedUserMessage(item)
}

func (e *Engine) DiscardQueuedUserMessage(queueItemID string) bool {
	e.ensureOrchestrationCollaborators()
	return e.messageFlow.DiscardQueuedUserMessage(queueItemID)
}

func (e *Engine) Interrupt() error {
	e.ensureOrchestrationCollaborators()
	if e.goalActive() {
		e.goalLoopState().Suspend()
	}
	return e.stepLifecycle.Interrupt()
}

func (e *Engine) SubmitUserMessage(ctx context.Context, text string) (assistant llm.Message, err error) {
	if text == "" {
		return llm.Message{}, errors.New("empty message")
	}

	e.ensureOrchestrationCollaborators()
	e.goalLoopState().Resume()
	err = e.stepLifecycle.Run(ctx, exclusiveStepOptions{EmitRunState: true, PersistRunLifecycle: true}, func(stepCtx context.Context, stepID string) error {
		hasQueuedInjected := e.messageFlow.HasPendingUserInjections()
		if err := e.ensureMetaContextForRequest(stepCtx, stepID); err != nil {
			return err
		}
		userMessage := llm.Message{Role: llm.RoleUser, Content: text}
		if !hasQueuedInjected {
			intents := []steeringIntent{steerUserMessageWithoutDerivedEventIntent(userMessage)}
			if flushed := flushedUserMessageEvent(userMessage, stepID); flushed != nil {
				intents = append(intents, steerEventIntent(*flushed))
			}
			if err := e.steer(stepID, intents...); err != nil {
				return err
			}
		} else if err := e.steer(stepID, steerUserMessageIntent(userMessage)); err != nil {
			return err
		}
		msg, runErr := e.runStepLoop(stepCtx, stepID)
		assistant = msg
		return runErr
	})
	return assistant, err
}

func (e *Engine) SubmitWorkflowTurn(ctx context.Context) (assistant llm.Message, err error) {
	if !e.workflowRunActive() {
		return llm.Message{}, errors.New("workflow turn requires an active workflow run")
	}

	e.ensureOrchestrationCollaborators()
	err = e.stepLifecycle.Run(ctx, exclusiveStepOptions{EmitRunState: true, PersistRunLifecycle: true}, func(stepCtx context.Context, stepID string) error {
		if err := e.ensureMetaContextForRequest(stepCtx, stepID); err != nil {
			return err
		}
		msg, runErr := e.runStepLoop(stepCtx, stepID)
		assistant = msg
		return runErr
	})
	return assistant, err
}

func (e *Engine) SubmitUserShellCommand(ctx context.Context, command string) (result tools.Result, err error) {
	command = strings.TrimSpace(command)
	if command == "" {
		return tools.Result{}, errors.New("empty command")
	}

	e.ensureOrchestrationCollaborators()
	err = e.stepLifecycle.Run(ctx, exclusiveStepOptions{EmitRunState: true, PersistRunLifecycle: true}, func(stepCtx context.Context, stepID string) error {
		if err := e.ensureMetaContextForRequest(stepCtx, stepID); err != nil {
			return err
		}

		call := llm.ToolCall{
			ID:   uuid.NewString(),
			Name: string(toolspec.ToolExecCommand),
			Input: mustJSON(map[string]any{
				"cmd":            command,
				"user_initiated": true,
			}),
		}
		if err := e.steer(stepID, steerMessageWithoutDerivedEventIntent(llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}})); err != nil {
			return err
		}
		if _, ok := e.registry.Get(toolspec.ToolExecCommand); !ok {
			transcriptCall := normalizeToolCallForTranscript(call, e.transcriptWorkingDir())
			_ = e.steerEvent(stepID, Event{Kind: EventToolCallStarted, StepID: stepID, ToolCall: &transcriptCall, CommittedTranscriptChanged: true})
			result = tools.Result{CallID: call.ID, Name: toolspec.ToolExecCommand, IsError: true, Output: mustJSON(map[string]any{"error": "unknown tool"}), Summary: "unknown tool"}
			if err := e.steer(stepID, steerToolCompletionIntent(result)); err != nil {
				return fmt.Errorf("%w (call_id=%s tool=%s): %w", errPersistToolCompletion, call.ID, result.Name, err)
			}
			if appendErr := e.steer(stepID, steerMessageIntent(llm.Message{Role: llm.RoleTool, Content: string(result.Output), ToolCallID: result.CallID, Name: string(result.Name)})); appendErr != nil {
				return appendErr
			}
			return errUnknownTool
		}

		results, execErr := e.executeToolCalls(stepCtx, stepID, []llm.ToolCall{call})
		if len(results) == 0 {
			return errors.New("shell tool execution returned no result")
		}
		result = results[0]
		if appendErr := e.steer(stepID, steerMessageIntent(llm.Message{Role: llm.RoleTool, Content: string(result.Output), ToolCallID: result.CallID, Name: string(result.Name)})); appendErr != nil {
			return errors.Join(execErr, appendErr)
		}
		return execErr
	})
	return result, err
}

func (e *Engine) runStepLoop(ctx context.Context, stepID string) (llm.Message, error) {
	reviewerFrequency := e.ReviewerFrequency()
	reviewerClient := e.reviewerClientSnapshot()
	result, err := e.runStepLoopWithOptions(ctx, stepID, reviewerFrequency, reviewerClient, true, true)
	if result.NoopFinalAnswer {
		return llm.Message{}, err
	}
	return result.Message, err
}

// runStepLoopWithOptions executes a single assistant/tool loop.
// reviewerFrequency/reviewerClient are used as the baseline reviewer policy for
// this run. When refreshReviewerConfigOnResolve is true, the final assistant
// resolution re-reads current runtime reviewer config so busy-time toggles (for
// example from /supervisor) affect the currently running step at completion.
func (e *Engine) runStepLoopWithOptions(ctx context.Context, stepID string, reviewerFrequency string, reviewerClient llm.Client, emitAssistantEvent bool, refreshReviewerConfigOnResolve bool) (stepLoopResult, error) {
	e.ensureOrchestrationCollaborators()
	return e.stepFlow.RunStepLoopWithOptions(ctx, stepID, stepLoopOptions{
		ReviewerFrequency:              reviewerFrequency,
		ReviewerClient:                 reviewerClient,
		EmitAssistantEvent:             emitAssistantEvent,
		RefreshReviewerConfigOnResolve: refreshReviewerConfigOnResolve,
	})
}

func (e *Engine) runReviewerFollowUp(ctx context.Context, stepID string, original llm.Message, originalCommittedStart int, originalCommittedStartSet bool, reviewerClient llm.Client) (reviewerFollowUpResult, error) {
	e.ensureOrchestrationCollaborators()
	return e.reviewerFlow.RunFollowUp(ctx, stepID, original, originalCommittedStart, originalCommittedStartSet, reviewerClient)
}

func (e *Engine) ensureLocked() (session.LockedContract, error) {
	if locked, ok := e.lockedContractState().Snapshot(); ok {
		return locked, nil
	}
	var providerContract llm.ProviderCapabilities
	hasProviderContract := false
	if e.cfg.ProviderCapabilitiesOverride != nil {
		providerContract = *e.cfg.ProviderCapabilitiesOverride
		hasProviderContract = true
	} else if provider, ok := e.llm.(llm.ProviderCapabilitiesClient); ok {
		if caps, err := provider.ProviderCapabilities(context.Background()); err == nil {
			providerContract = caps
			hasProviderContract = true
		}
	}

	contextBudget := e.promptContextBudgetFromConfig()
	lock := session.LockedContract{
		Model:             e.cfg.Model,
		Temperature:       e.cfg.Temperature,
		MaxOutputToken:    e.cfg.MaxTokens,
		ContextWindow:     contextBudget.window,
		ContextPercent:    contextBudget.percent,
		EnabledTools:      toToolNames(e.cfg.EnabledTools),
		ModelCapabilities: e.cfg.ModelCapabilities,
		ToolPreambles: func() *bool {
			enabled := !e.cfg.HeadlessMode && e.cfg.ToolPreambles
			return &enabled
		}(),
	}
	if hasProviderContract {
		lock.ProviderContract = llm.LockedProviderCapabilitiesFromContract(providerContract)
	}
	systemPrompt, err := e.buildSystemPromptSnapshotForRoot(lock, e.systemPromptWorkspaceRootLocked())
	if err != nil {
		return session.LockedContract{}, err
	}
	lock.SystemPrompt = systemPrompt
	lock.HasSystemPrompt = true
	if err := e.store.MarkModelDispatchLocked(lock); err != nil {
		return session.LockedContract{}, err
	}
	e.lockedContractState().Set(lock)
	return lock, nil
}

func (e *Engine) generateWithRetry(ctx context.Context, stepID string, req llm.Request, onDelta func(string), onReasoningDelta func(llm.ReasoningSummaryDelta), onAttemptReset func()) (llm.Response, error) {
	return e.generateWithRetryClient(ctx, stepID, e.llm, req, onDelta, onReasoningDelta, onAttemptReset)
}

func (e *Engine) generateWithRetryClient(ctx context.Context, stepID string, client llm.Client, req llm.Request, onDelta func(string), onReasoningDelta func(llm.ReasoningSummaryDelta), onAttemptReset func()) (llm.Response, error) {
	prepared, err := e.modelRequests().RequestCache().Prepare(req)
	if err != nil {
		return llm.Response{}, err
	}
	if err := e.observePromptCacheRequest(stepID, prepared); err != nil {
		return llm.Response{}, err
	}
	delays := generateRetryDelays
	var lastErr error
	for i := 0; i <= len(delays); i++ {
		var (
			resp                    llm.Response
			attemptErr              error
			attemptEmitted          bool
			reasoningEmitted        bool
			attemptOnDelta          func(string)
			attemptOnReasoningDelta func(llm.ReasoningSummaryDelta)
			attemptDone             atomic.Bool
		)
		if onDelta != nil {
			attemptOnDelta = func(delta string) {
				if attemptDone.Load() {
					return
				}
				if delta == "" {
					return
				}
				attemptEmitted = true
				onDelta(delta)
			}
		}
		if onReasoningDelta != nil {
			attemptOnReasoningDelta = func(delta llm.ReasoningSummaryDelta) {
				if attemptDone.Load() {
					return
				}
				if strings.TrimSpace(delta.Text) == "" {
					return
				}
				reasoningEmitted = true
				onReasoningDelta(delta)
			}
		}
		if streamingClient, ok := client.(llm.StreamEventsClient); ok {
			resp, attemptErr = streamingClient.GenerateStreamWithEvents(ctx, req, llm.StreamCallbacks{
				OnAssistantDelta:        attemptOnDelta,
				OnReasoningSummaryDelta: attemptOnReasoningDelta,
			})
		} else if streamingClient, ok := client.(llm.StreamClient); ok {
			resp, attemptErr = streamingClient.GenerateStream(ctx, req, attemptOnDelta)
		} else {
			resp, attemptErr = client.Generate(ctx, req)
			if attemptErr == nil && attemptOnDelta != nil && resp.Assistant.Content != "" {
				attemptOnDelta(resp.Assistant.Content)
			}
		}
		attemptDone.Store(true)
		if attemptErr != nil && ctx.Err() != nil {
			return llm.Response{}, ctx.Err()
		}
		if attemptErr == nil {
			if err := e.observePromptCacheResponse(stepID, prepared, resp.Usage); err != nil {
				return llm.Response{}, err
			}
			return resp, nil
		}
		if llm.IsNonRetriableModelError(attemptErr) || llm.IsContextLengthOverflowError(attemptErr) {
			return llm.Response{}, attemptErr
		}
		if (attemptEmitted || reasoningEmitted) && onAttemptReset != nil {
			onAttemptReset()
		}
		lastErr = attemptErr
		if i == len(delays) {
			break
		}
		if err := waitForRetryDelay(ctx, delays[i]); err != nil {
			return llm.Response{}, err
		}
	}
	return llm.Response{}, fmt.Errorf("model generation failed after retries: %w", lastErr)
}

func (e *Engine) executeToolCalls(ctx context.Context, stepID string, calls []llm.ToolCall) ([]tools.Result, error) {
	e.ensureOrchestrationCollaborators()
	return e.toolFlow.ExecuteToolCalls(ctx, stepID, calls)
}
