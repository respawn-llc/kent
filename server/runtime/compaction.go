package runtime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"core/prompts"
	"core/server/llm"
	"core/shared/transcript"
)

type compactionMode string

const (
	compactionModeAuto    compactionMode = "auto"
	compactionModeHandoff compactionMode = "handoff"
	compactionModeManual  compactionMode = "manual"

	defaultContextWindowTokens         = 200_000
	autoCompactNearLimitMargin         = 8_000
	compactionSoonReminderPercent      = 85
	manualCompactionCarryoverMaxChars  = 4_000
	preciseTokenCountSupportDiagnostic = "precise_token_count_support_failure"
	preciseTokenCountFailureDiagnostic = "precise_token_count_failure"

	additionalCompactionInstructionsHeader = "# Additional user instructions or commentary for this task:"
	manualCompactionCarryoverHeader        = "# Last user message before handoff (work may have been done after it was sent):"
	handoffDisabledByUserMessage           = "User disabled the handoff manually for now. They do not want you to hand off at this time, so please keep working or retry this tool later"
	handoffTooEarlyMessage                 = "trigger_handoff is not enabled yet. Keep working until you receive the reminder that this tool is now enabled, then retry it."
	handoffCompactionToolsDisabledMessage  = "Tools are disabled during handoff. Do NOT attempt to call any tools. Produce only the requested summary."
	handoffCompactionToolCallRetries       = 3
)

var errRemoteCompactionMissingCheckpoint = errors.New("remote compaction output missing checkpoint item")

var (
	// errHandoffDisabledByUser is returned when the user has disabled handoff and the agent requests one.
	errHandoffDisabledByUser = errors.New(handoffDisabledByUserMessage)
	// errHandoffTooEarly is returned when the agent requests a handoff before the trigger_handoff tool is enabled.
	errHandoffTooEarly = errors.New(handoffTooEarlyMessage)
	// errCompactionDisabledModeNone is returned when manual compaction is requested while compaction_mode=none.
	errCompactionDisabledModeNone = errors.New("context compaction is disabled (compaction_mode=none)")
)

type compactionResult struct {
	engine            string
	items             []llm.ResponseItem
	usage             llm.Usage
	trimmedItemsCount int
	overflowRepair    compactionOverflowRepairStats
	provider          string
	summary           string
}

type defaultContextCompactor struct {
	engine *Engine
	steps  exclusiveStepLifecycle
}

func (e *Engine) CompactContext(ctx context.Context, args string) error {
	e.ensureOrchestrationCollaborators()
	return e.compactionFlow.CompactContext(ctx, args)
}

func (e *Engine) CompactContextForPreSubmit(ctx context.Context) error {
	e.ensureOrchestrationCollaborators()
	return e.compactionFlow.CompactContextForPreSubmit(ctx)
}

func (e *Engine) TriggerHandoff(ctx context.Context, stepID string, activeCall llm.ToolCall, summarizerPrompt string, futureAgentMessage string) (string, bool, error) {
	e.ensureOrchestrationCollaborators()
	return e.compactionFlow.TriggerHandoff(ctx, stepID, activeCall, summarizerPrompt, futureAgentMessage)
}

func (c *defaultContextCompactor) CompactContext(ctx context.Context, args string) error {
	return c.compactContext(ctx, compactionModeManual, args, true)
}

func (c *defaultContextCompactor) CompactContextForPreSubmit(ctx context.Context) error {
	return c.compactContext(ctx, compactionModeManual, "", false)
}

func (c *defaultContextCompactor) TriggerHandoff(ctx context.Context, stepID string, activeCall llm.ToolCall, summarizerPrompt string, futureAgentMessage string) (string, bool, error) {
	e := c.engine
	_ = activeCall
	if strings.TrimSpace(stepID) == "" {
		return "", false, errors.New("trigger_handoff requires an active step")
	}
	planningSnapshot := e.compactionPlanningSnapshot()
	planner := e.compactionPlannerState()
	if !planningSnapshot.autoCompactionEnabled {
		return "", false, errHandoffDisabledByUser
	}
	if planner.mode(planningSnapshot.compactionMode) == "none" {
		return "", false, errors.New("User explicitly disabled compaction in configuration.")
	}
	if !e.handoffToolEnabled() {
		return "", false, errHandoffTooEarly
	}
	e.queueHandoffRequest(summarizerPrompt, futureAgentMessage)
	summary := "Handoff scheduled to run now."
	appended := strings.TrimSpace(futureAgentMessage) != ""
	return summary, appended, nil
}

func (c *defaultContextCompactor) compactContext(ctx context.Context, mode compactionMode, args string, includeManualCarryover bool) error {
	e := c.engine
	return c.steps.Run(ctx, exclusiveStepOptions{}, func(stepCtx context.Context, stepID string) error {
		if err := e.ensureMetaContextForCompaction(stepCtx, stepID); err != nil {
			return err
		}
		_, err := e.compactNow(stepCtx, stepID, mode, args, includeManualCarryover)
		if err == nil {
			e.clearPendingHandoffRequest()
		}
		return err
	})
}

func (e *Engine) autoCompactIfNeeded(ctx context.Context, stepID string, mode compactionMode) error {
	e.ensureOrchestrationCollaborators()
	return e.compactionFlow.AutoCompactIfNeeded(ctx, stepID, mode)
}

func (c *defaultContextCompactor) AutoCompactIfNeeded(ctx context.Context, stepID string, mode compactionMode) error {
	e := c.engine
	if mode == compactionModeAuto && !e.shouldAutoCompactWithContext(ctx) {
		return nil
	}
	_, err := e.compactNow(ctx, stepID, mode, "", false)
	if err == nil {
		e.clearPendingHandoffRequest()
	}
	if err != nil && mode == compactionModeAuto {
		return fmt.Errorf("auto compaction failed: %w", err)
	}
	if err == nil && mode == compactionModeAuto && e.shouldAutoCompactWithContext(ctx) {
		return errors.New("auto compaction did not reduce context below threshold")
	}
	return err
}

func (e *Engine) shouldAutoCompact() bool {
	return e.shouldAutoCompactWithContext(context.Background())
}

func (e *Engine) shouldAutoCompactWithContext(ctx context.Context) bool {
	snapshot := e.compactionPlanningSnapshot()
	planner := e.compactionPlannerState()
	if !planner.autoCompactionAvailable(snapshot) {
		return false
	}
	limit := planner.autoCompactTokenLimit(snapshot)
	if limit <= 0 {
		return false
	}
	return e.usageAtOrAboveLimit(ctx, limit)
}

func (e *Engine) preSubmitCompactionTokenLimit(ctx context.Context) int {
	return e.compactionPlannerState().preSubmitTokenLimit(e.compactionPlanningSnapshot())
}

func (e *Engine) ShouldCompactBeforeUserMessage(ctx context.Context, text string) (bool, error) {
	e.ensureOrchestrationCollaborators()
	return e.compactionFlow.ShouldCompactBeforeUserMessage(ctx, text)
}

func (c *defaultContextCompactor) ShouldCompactBeforeUserMessage(ctx context.Context, text string) (bool, error) {
	e := c.engine
	if strings.TrimSpace(text) == "" {
		return false, nil
	}
	planningSnapshot := e.compactionPlanningSnapshot()
	planner := e.compactionPlannerState()
	if !planner.autoCompactionAvailable(planningSnapshot) {
		return false, nil
	}
	limit := planner.autoCompactTokenLimit(planningSnapshot)
	if limit <= 0 {
		return false, nil
	}
	reservedOutput := planner.reservedOutputTokens(planningSnapshot)
	preSubmitLimit := planner.preSubmitTokenLimit(planningSnapshot)
	if preSubmitLimit > 0 {
		_, _ = e.currentInputTokensPreciselyIfCritical(ctx, preSubmitLimit)
	}
	estimatedCurrentTotal := e.currentTokenUsage() + reservedOutput
	if preSubmitLimit > 0 && estimatedCurrentTotal >= preSubmitLimit {
		if preciseInput, ok := e.currentInputTokensPrecisely(ctx); ok {
			return preciseInput+reservedOutput >= preSubmitLimit, nil
		}
		return true, nil
	}
	promptEstimate := estimateItemsTokens(llm.ItemsFromMessages([]llm.Message{{Role: llm.RoleUser, Content: text}}))
	if estimatedCurrentTotal+promptEstimate < limit {
		return false, nil
	}
	req, err := e.buildRequestWithExtraItems(ctx, "", []llm.ResponseItem{{Type: llm.ResponseItemTypeMessage, Role: llm.RoleUser, Content: text}}, true)
	if err != nil {
		return false, err
	}
	if preciseInput, ok := e.requestInputTokensPrecisely(ctx, req); ok {
		return preciseInput+reservedOutput >= limit, nil
	}
	return estimatedCurrentTotal+promptEstimate >= limit, nil
}

func (e *Engine) resolveContextWindowTokens(ctx context.Context) int {
	if configured := e.configuredContextWindowTokens(); configured > 0 {
		return configured
	}

	model := e.currentModel()
	if resolver, ok := e.llm.(llm.ModelContextWindowClient); ok {
		resolved, err := resolver.ResolveModelContextWindow(ctx, model)
		if err == nil && resolved > 0 {
			e.setContextWindowTokens(resolved)
			return resolved
		}
	}
	return e.contextWindowTokens()
}

func (e *Engine) configuredContextWindowTokens() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cfg.ContextWindowTokens > 0 {
		return e.cfg.ContextWindowTokens
	}
	return 0
}

func (e *Engine) setContextWindowTokens(tokens int) {
	if tokens <= 0 {
		return
	}
	e.mu.Lock()
	e.cfg.ContextWindowTokens = tokens
	e.mu.Unlock()
}

func (e *Engine) currentModel() string {
	if model := e.lockedContractState().Model(); model != "" {
		return model
	}
	return strings.TrimSpace(e.cfg.Model)
}

func (e *Engine) reservedOutputTokens() int {
	return e.compactionPlannerState().reservedOutputTokens(e.compactionPlanningSnapshot())
}

func autoCompactPrecisionMarginForLimit(limit int) int {
	if limit <= 0 {
		return autoCompactNearLimitMargin
	}
	percentMargin := limit / 50
	if percentMargin > autoCompactNearLimitMargin {
		return percentMargin
	}
	return autoCompactNearLimitMargin
}

func (e *Engine) usageAtOrAboveLimit(ctx context.Context, limit int) bool {
	if limit <= 0 {
		return false
	}
	reservedOutput := e.reservedOutputTokens()
	if preciseInput, ok := e.currentInputTokensPreciselyIfCritical(ctx, limit); ok {
		return preciseInput+reservedOutput >= limit
	}
	estimatedInput := e.currentTokenUsage()
	estimatedTotal := estimatedInput + reservedOutput
	margin := autoCompactPrecisionMarginForLimit(limit)
	if estimatedTotal < limit && estimatedTotal+margin < limit {
		return false
	}
	preciseInput, ok := e.currentInputTokensPrecisely(ctx)
	if !ok {
		return estimatedTotal >= limit
	}
	return preciseInput+reservedOutput >= limit
}

func (e *Engine) currentInputTokensPrecisely(ctx context.Context) (int, bool) {
	req, err := e.buildRequest(ctx, "", true)
	if err != nil {
		return 0, false
	}
	return e.requestInputTokensPreciselyTracked(ctx, req, true)
}

func (e *Engine) currentInputTokensPreciselyIfDue(ctx context.Context, limit int) (int, bool) {
	return e.currentInputTokensPreciselyIfDueWithPriority(ctx, limit, false)
}

func (e *Engine) currentInputTokensPreciselyIfCritical(ctx context.Context, limit int) (int, bool) {
	return e.currentInputTokensPreciselyIfDueWithPriority(ctx, limit, true)
}

func (e *Engine) currentInputTokensPreciselyIfDueWithPriority(ctx context.Context, limit int, critical bool) (int, bool) {
	if precise, ok := e.lookupCurrentPreciseInputTokens(); ok {
		if !e.shouldRefreshCurrentPreciseInputTokens(limit, critical) {
			return precise, true
		}
	}
	if !e.shouldRefreshCurrentPreciseInputTokens(limit, critical) {
		return 0, false
	}
	req, err := e.buildRequest(ctx, "", true)
	if err != nil {
		return 0, false
	}
	return e.requestInputTokensPreciselyTracked(ctx, req, true)
}

func (e *Engine) requestInputTokensPrecisely(ctx context.Context, req llm.Request) (int, bool) {
	return e.requestInputTokensPreciselyTracked(ctx, req, false)
}

func (e *Engine) requestInputTokensPreciselyTracked(ctx context.Context, req llm.Request, current bool) (int, bool) {
	counter, ok := e.llm.(llm.RequestInputTokenCountClient)
	if !ok {
		return 0, false
	}
	if !e.preciseInputTokenCountSupported(ctx) {
		return 0, false
	}
	cacheKey := ""
	if payload, err := json.Marshal(req); err == nil {
		sum := sha256.Sum256(payload)
		cacheKey = hex.EncodeToString(sum[:])
	}
	if cacheKey != "" {
		if cached, ok := e.lookupPreciseTokenCount(cacheKey, current); ok {
			if current {
				e.storePreciseTokenCount(cacheKey, cached, true)
			}
			return cached, true
		}
	}
	if e.hasPersistedDiagnostic(preciseTokenCountFailureDiagnostic) {
		return 0, false
	}
	count, err := counter.CountRequestInputTokens(ctx, req)
	if err != nil {
		if e.errorIsRepairableMissingToolOutput(err) {
			// The request carries interrupted tool calls without outputs that the
			// model request path repairs by appending synthetic outputs. Fall back to
			// an estimate for this probe only; do not persist a permanent failure that
			// would disable exact counting for the rest of the active list.
			return 0, false
		}
		e.reportPreciseTokenCountFailure(err)
		return 0, false
	}
	if count <= 0 {
		return 0, false
	}
	if cacheKey != "" {
		e.storePreciseTokenCount(cacheKey, count, current)
	}
	return count, true
}

func (e *Engine) preciseInputTokenCountSupported(ctx context.Context) bool {
	caps, err := e.providerCapabilities(ctx)
	if err != nil {
		e.reportPreciseTokenCountSupportFailure(err)
		return false
	}
	if !caps.SupportsRequestInputTokenCount {
		return false
	}
	support, ok := e.llm.(llm.RequestInputTokenCountSupportClient)
	if !ok {
		return true
	}
	supported, err := support.SupportsRequestInputTokenCount(ctx)
	if err != nil {
		e.reportPreciseTokenCountSupportFailure(err)
		return false
	}
	return supported
}

func (e *Engine) reportPreciseTokenCountSupportFailure(err error) {
	if err == nil {
		return
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "unknown exact token counting support failure"
	}
	entryText := fmt.Sprintf("Exact token counting availability check failed: %s. Falling back to a local token estimate.", message)
	if persistErr := e.steerPersistedDiagnosticEntry(
		"",
		preciseTokenCountSupportDiagnostic,
		"error",
		entryText,
	); persistErr != nil {
		e.AppendCommittedEntry("error", fmt.Sprintf("%s Diagnostic persistence failed: %v", entryText, persistErr))
	}
}

func (e *Engine) reportPreciseTokenCountFailure(err error) {
	if err == nil {
		return
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		message = "unknown exact token counting failure"
	}
	entryText := fmt.Sprintf("Exact token counting failed: %s. Falling back to a local token estimate.", message)
	if persistErr := e.steerPersistedDiagnosticEntry(
		"",
		preciseTokenCountFailureDiagnostic,
		"error",
		entryText,
	); persistErr != nil {
		e.AppendCommittedEntry("error", fmt.Sprintf("%s Diagnostic persistence failed: %v", entryText, persistErr))
	}
}

func (e *Engine) lookupPreciseTokenCount(cacheKey string, current bool) (int, bool) {
	if strings.TrimSpace(cacheKey) == "" || e.modelRequests().TokenUsage() == nil {
		return 0, false
	}
	if current {
		if cached, ok := e.modelRequests().TokenUsage().lookupCurrent(cacheKey); ok {
			return cached, true
		}
	}
	return e.modelRequests().TokenUsage().lookup(cacheKey)
}

func (e *Engine) storePreciseTokenCount(cacheKey string, count int, current bool) {
	if strings.TrimSpace(cacheKey) == "" || count <= 0 || e.modelRequests().TokenUsage() == nil {
		return
	}
	e.modelRequests().TokenUsage().store(cacheKey, count, current)
}

func (e *Engine) lookupCurrentPreciseInputTokens() (int, bool) {
	if e.modelRequests().TokenUsage() == nil {
		return 0, false
	}
	return e.modelRequests().TokenUsage().lookupCurrent("")
}

// markCurrentRequestShapeDirty invalidates the current-context exact token count
// whenever the next provider request may differ from the previously counted one.
func (e *Engine) markCurrentRequestShapeDirty() {
	tracker := e.modelRequests().TokenUsage()
	if tracker == nil {
		return
	}
	tracker.invalidateCurrent(tokenUsageMutationPlain)
}

func (e *Engine) markCurrentRequestShapeDirtyForSignificantMutation() {
	tracker := e.modelRequests().TokenUsage()
	if tracker == nil {
		return
	}
	tracker.invalidateCurrent(tokenUsageMutationSignificant)
}

func (e *Engine) resetCurrentPreciseInputTracking() {
	tracker := e.modelRequests().TokenUsage()
	if tracker == nil {
		return
	}
	tracker.invalidateCurrent(tokenUsageMutationHardReset)
}

func (e *Engine) shouldRefreshCurrentPreciseInputTokens(limit int, critical bool) bool {
	if limit <= 0 || e.modelRequests().TokenUsage() == nil {
		return false
	}
	return e.modelRequests().TokenUsage().currentCheckpointDue(e.estimatedCurrentTokenUsage(), limit, critical)
}

func (e *Engine) contextWindowTokens() int {
	return e.compactionPlannerState().contextWindowTokens(e.compactionPlanningSnapshot())
}

func (e *Engine) estimatedCurrentTokenUsage() int {
	estimated := 0
	if e != nil {
		estimated = e.transcriptRuntimeState().EstimatedProviderTokens()
	}
	if e.modelRequests().TokenUsage() != nil {
		if baseline, ok := e.modelRequests().TokenUsage().estimateCurrentInputTokens(estimated); ok {
			return baseline
		}
	}
	if estimated > 0 {
		return estimated
	}
	usage := e.lastUsageSnapshot()
	if usage.InputTokens > 0 {
		return usage.InputTokens
	}
	return 0
}

func (e *Engine) currentTokenUsage() int {
	if precise, ok := e.lookupCurrentPreciseInputTokens(); ok {
		return precise
	}
	return e.estimatedCurrentTokenUsage()
}

func (e *Engine) compactNow(ctx context.Context, stepID string, mode compactionMode, args string, includeManualCarryover bool) (compactionResult, error) {
	planningSnapshot := e.compactionPlanningSnapshot()
	planner := e.compactionPlannerState()
	if planner.mode(planningSnapshot.compactionMode) == "none" {
		if mode == compactionModeAuto {
			return compactionResult{}, nil
		}
		return compactionResult{}, errCompactionDisabledModeNone
	}

	input := e.snapshotItems()
	if len(input) == 0 {
		return compactionResult{}, nil
	}

	_ = e.resolveContextWindowTokens(ctx)

	caps, err := e.providerCapabilities(ctx)
	if err != nil {
		return compactionResult{}, err
	}
	providerID := strings.TrimSpace(caps.ProviderID)
	if providerID == "" {
		providerID = "unknown"
	}

	if err := e.emitCompactionStatus(stepID, EventCompactionStarted, mode, "selector", providerID, 0, 0, ""); err != nil {
		return compactionResult{}, err
	}

	instructions := compactionInstructions(args)
	manualCarryover := ""
	if mode == compactionModeManual && includeManualCarryover {
		manualCarryover = lastVisibleUserMessageSinceLatestCompaction(input)
	}
	var result compactionResult
	enginePlan := planner.enginePlan(planningSnapshot, caps)
	if enginePlan.engineKind == compactionEngineRemote {
		result, err = e.compactRemote(ctx, stepID, input, providerID, instructions)
		if err != nil && enginePlan.fallbackToLocalOnBadCheckpoint && errors.Is(err, errRemoteCompactionMissingCheckpoint) {
			result, err = e.compactLocal(ctx, input, providerID, instructions, mode)
		}
	} else {
		result, err = e.compactLocal(ctx, input, providerID, instructions, mode)
	}
	if err != nil {
		statusErr := e.emitCompactionStatus(stepID, EventCompactionFailed, mode, result.engine, providerID, result.trimmedItemsCount, 0, err.Error())
		return compactionResult{}, errors.Join(err, statusErr)
	}

	if len(result.items) == 0 {
		err := errors.New("compaction returned empty replacement history")
		statusErr := e.emitCompactionStatus(stepID, EventCompactionFailed, mode, result.engine, providerID, result.trimmedItemsCount, 0, err.Error())
		return compactionResult{}, errors.Join(err, statusErr)
	}

	compactionNumber := e.compactionCountSnapshot() + 1
	result.items = withCompactionSummaryLabel(result.items, CompactionNoticeText(compactionNumber))
	postReplacementMeta, err := e.compactionReinjectedMetaMessages(ctx)
	if err != nil {
		statusErr := e.emitCompactionStatus(stepID, EventCompactionFailed, mode, result.engine, providerID, result.trimmedItemsCount, 0, err.Error())
		return compactionResult{}, errors.Join(err, statusErr)
	}
	// Reinject base meta as part of the single history_replaced commit so the
	// rebuilt active list is born with it atomically: a restart can never observe
	// a compacted session that has a summary but no base meta, and the summary
	// precedes the reinjected meta in both provider and transcript order.
	replacementItems := append(llm.CloneResponseItems(result.items), llm.ItemsFromMessages(postReplacementMeta)...)
	if err := e.replaceHistory(stepID, result.engine, mode, replacementItems); err != nil {
		statusErr := e.emitCompactionStatus(stepID, EventCompactionFailed, mode, result.engine, providerID, result.trimmedItemsCount, 0, err.Error())
		return compactionResult{}, errors.Join(err, statusErr)
	}
	if strings.TrimSpace(result.summary) != "" && result.engine != "local" {
		summary := strings.TrimSpace(result.summary)
		if err := e.steer(stepID, steerLocalEntryIntent(storedLocalEntry{Role: "compaction_summary", Text: summary})); err != nil {
			statusErr := e.emitCompactionStatus(stepID, EventCompactionFailed, mode, result.engine, providerID, result.trimmedItemsCount, 0, err.Error())
			return compactionResult{}, errors.Join(err, statusErr)
		}
	}
	if result.overflowRepair.Collapsed() {
		if err := e.steer(stepID, steerLocalEntryIntent(storedLocalEntry{Role: string(transcript.EntryRoleDeveloperErrorFeedback), Text: fmt.Sprintf(
			"Context compaction succeeded after collapsing tool payloads: %d shell outputs, %d patch inputs, ~%d tokens omitted. Full original tool payloads remain in pre-compaction transcript history but are omitted from the compacted model context.",
			result.overflowRepair.ShellOutputsCollapsed,
			result.overflowRepair.PatchInputsCollapsed,
			result.overflowRepair.EstimatedSavedTokens,
		)})); err != nil {
			statusErr := e.emitCompactionStatus(stepID, EventCompactionFailed, mode, result.engine, providerID, result.trimmedItemsCount, 0, err.Error())
			return compactionResult{}, errors.Join(err, statusErr)
		}
	}
	if err := e.appendPostCompactionMessages(stepID, e.postCompactionMessages(mode, manualCarryover, e.store.Meta().HeadlessActive)); err != nil {
		statusErr := e.emitCompactionStatus(stepID, EventCompactionFailed, mode, result.engine, providerID, result.trimmedItemsCount, 0, err.Error())
		return compactionResult{}, errors.Join(err, statusErr)
	}
	compactionNumber = e.nextCompactionCount()
	windowTokens := result.usage.WindowTokens
	if windowTokens <= 0 {
		windowTokens = e.contextWindowTokens()
	}
	inputTokens := estimateItemsTokens(e.snapshotItems())
	if preciseInput, ok := e.currentInputTokensPrecisely(ctx); ok {
		inputTokens = preciseInput
	}
	if err := e.recordLastUsage(llm.Usage{
		InputTokens:  inputTokens,
		OutputTokens: 0,
		WindowTokens: windowTokens,
	}); err != nil {
		return compactionResult{}, err
	}

	if err := e.emitCompactionStatus(stepID, EventCompactionCompleted, mode, result.engine, providerID, result.trimmedItemsCount, compactionNumber, ""); err != nil {
		return compactionResult{}, err
	}
	return result, nil
}

func lastVisibleUserMessageSinceLatestCompaction(items []llm.ResponseItem) string {
	start := 0
	for i := len(items) - 1; i >= 0; i-- {
		if !isCompactionBoundaryItem(items[i]) {
			continue
		}
		start = i + 1
		break
	}
	for i := len(items) - 1; i >= start; i-- {
		item := items[i]
		if item.Type != llm.ResponseItemTypeMessage || item.Role != llm.RoleUser {
			continue
		}
		if item.MessageType == llm.MessageTypeCompactionSummary || strings.TrimSpace(item.Content) == "" {
			continue
		}
		return item.Content
	}
	return ""
}

func (e *Engine) queueHandoffRequest(summarizerPrompt string, futureAgentMessage string) {
	e.handoffRuntimeState().QueueRequest(summarizerPrompt, futureAgentMessage)
}

func (e *Engine) clearPendingHandoffRequest() {
	e.handoffRuntimeState().ClearRequest()
}

func (e *Engine) queuePendingHandoffFutureMessage(message string) {
	e.handoffRuntimeState().QueueFutureMessage(message)
}

func (e *Engine) clearPendingHandoffFutureMessage() {
	e.handoffRuntimeState().ClearFutureMessage()
}

func (e *Engine) pendingHandoffRequestSnapshot() *handoffRequest {
	return e.handoffRuntimeState().RequestSnapshot()
}

func (e *Engine) pendingHandoffFutureMessageSnapshot() string {
	return e.handoffRuntimeState().FutureMessageSnapshot()
}

func (e *Engine) handoffRuntimeState() *handoffRuntimeState {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.handoffState == nil {
		e.handoffState = newHandoffRuntimeState()
	}
	return e.handoffState
}

func (e *Engine) applyPendingHandoffIfNeeded(ctx context.Context, stepID string) (bool, error) {
	if futureMessage := e.pendingHandoffFutureMessageSnapshot(); futureMessage != "" {
		if err := e.steer(stepID, steerMessageIntent(handoffFutureAgentMessage(futureMessage))); err != nil {
			return false, err
		}
		e.clearPendingHandoffFutureMessage()
		return false, nil
	}
	req := e.pendingHandoffRequestSnapshot()
	if req == nil {
		return false, nil
	}
	if _, err := e.compactNow(ctx, stepID, compactionModeHandoff, req.summarizerPrompt, false); err != nil {
		if e.pendingHandoffFutureMessageSnapshot() != "" {
			e.clearPendingHandoffRequest()
		}
		return false, err
	}
	e.clearPendingHandoffRequest()
	return true, nil
}

func withCompactionSummaryLabel(items []llm.ResponseItem, label string) []llm.ResponseItem {
	label = strings.TrimSpace(label)
	if label == "" || len(items) == 0 {
		return llm.CloneResponseItems(items)
	}
	out := llm.CloneResponseItems(items)
	for idx := range out {
		if out[idx].MessageType != llm.MessageTypeCompactionSummary {
			continue
		}
		out[idx].CompactContent = label
		return out
	}
	return out
}

func (e *Engine) compactionPlannerState() *compactionPlanner {
	if e == nil || e.compactionPlanner == nil {
		return newCompactionPlanner()
	}
	return e.compactionPlanner
}

func (e *Engine) compactionPlanningSnapshot() compactionPlanningSnapshot {
	if e == nil {
		return compactionPlanningSnapshot{autoCompactionEnabled: true}
	}
	e.mu.Lock()
	autoEnabled := true
	if e.cfg.AutoCompactionEnabled != nil {
		autoEnabled = *e.cfg.AutoCompactionEnabled
	}
	snapshot := compactionPlanningSnapshot{
		autoCompactionEnabled:         autoEnabled,
		compactionMode:                e.cfg.CompactionMode,
		autoCompactTokenLimit:         e.cfg.AutoCompactTokenLimit,
		preSubmitCompactionLeadTokens: e.cfg.PreSubmitCompactionLeadTokens,
		contextWindowTokens:           e.cfg.ContextWindowTokens,
		effectiveContextWindowPercent: e.cfg.EffectiveContextWindowPercent,
		maxOutputTokens:               e.cfg.MaxTokens,
	}
	e.mu.Unlock()
	snapshot.lockedMaxOutputTokens = e.lockedContractState().MaxOutputToken()
	snapshot.lastUsage = e.lastUsageSnapshot()
	snapshot.currentUsedTokens = e.currentTokenUsage()
	return snapshot
}

func compactionInstructions(args string) string {
	instructions := prompts.CompactionPrompt
	if strings.TrimSpace(args) == "" {
		return instructions
	}
	instructions = strings.TrimRight(instructions, "\n")
	return instructions + "\n\n" + additionalCompactionInstructionsHeader + "\n " + strings.TrimSpace(args)
}
