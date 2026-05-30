package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"builder/server/llm"
	"builder/server/session"
	"builder/shared/toolspec"
	"builder/shared/transcript"
)

func (e *Engine) snapshotMessages() []llm.Message {
	return e.transcriptRuntimeState().SnapshotMessages()
}

func (e *Engine) snapshotItems() []llm.ResponseItem {
	return e.transcriptRuntimeState().SnapshotItems()
}

func (e *Engine) ChatSnapshot() ChatSnapshot {
	return e.transcriptRuntimeState().Snapshot()
}

func (e *Engine) OngoingTailTranscriptWindow(maxEntries int) TranscriptWindowSnapshot {
	if e == nil {
		return TranscriptWindowSnapshot{}
	}
	return e.transcriptRuntimeState().OngoingTailSnapshot(maxEntries)
}

func (e *Engine) TranscriptPageSnapshot(offset, limit int) transcriptPageSnapshot {
	if e == nil {
		return transcriptPageSnapshot{}
	}
	return e.transcriptRuntimeState().TranscriptPageSnapshot(offset, limit)
}

func (e *Engine) TranscriptRevision() int64 {
	if e == nil || e.store == nil {
		return 0
	}
	return e.store.Meta().LastSequence
}

func (e *Engine) CommittedTranscriptEntryCount() int {
	if e == nil {
		return 0
	}
	return e.transcriptRuntimeState().CommittedEntryCount()
}

func (e *Engine) ActiveRun() *RunSnapshot {
	if e == nil || e.stepLifecycle == nil {
		return nil
	}
	return e.stepLifecycle.Snapshot()
}

func (e *Engine) LastCommittedAssistantFinalAnswer() string {
	if e == nil {
		return ""
	}
	return e.transcriptRuntimeState().LastCommittedAssistantFinalAnswer()
}

func messagePreservesLastCommittedAssistantFinalAnswer(message llm.Message) bool {
	if message.Role != llm.RoleDeveloper {
		return false
	}
	switch message.MessageType {
	case llm.MessageTypeCompactionSoonReminder, llm.MessageTypeErrorFeedback, llm.MessageTypeGoal, llm.MessageTypeHandoffFutureMessage, llm.MessageTypeReviewerFeedback:
		return true
	default:
		return false
	}
}

func (e *Engine) ContextUsage() ContextUsage {
	window := e.contextWindowTokens()
	used := e.currentTokenUsage()
	cacheHitPercent, hasCacheHitPercentage := e.cacheHitSnapshot()
	if used < 0 {
		used = 0
	}
	if window < 0 {
		window = 0
	}
	return ContextUsage{
		UsedTokens:            used,
		WindowTokens:          window,
		CacheHitPercent:       cacheHitPercent,
		HasCacheHitPercentage: hasCacheHitPercentage,
	}
}

func (e *Engine) AppendLocalEntry(role, text string) {
	e.AppendLocalEntryWithOngoingText(role, text, "")
}

func (e *Engine) AppendLocalEntryWithVisibility(role, text string, visibility transcript.EntryVisibility) {
	e.appendLocalEntry(storedLocalEntry{
		Visibility: transcript.NormalizeEntryVisibility(visibility),
		Role:       strings.TrimSpace(role),
		Text:       strings.TrimSpace(text),
	})
}

func (e *Engine) AppendLocalEntryWithNoticeID(role, text, noticeID string) {
	e.appendLocalEntry(storedLocalEntry{
		Visibility: transcript.EntryVisibilityAuto,
		Role:       strings.TrimSpace(role),
		Text:       strings.TrimSpace(text),
		NoticeID:   strings.TrimSpace(noticeID),
	})
}

func (e *Engine) AppendLocalEntryWithOngoingText(role, text, ongoingText string) {
	e.appendLocalEntry(storedLocalEntry{
		Visibility:  transcript.EntryVisibilityAuto,
		Role:        strings.TrimSpace(role),
		Text:        strings.TrimSpace(text),
		OngoingText: strings.TrimSpace(ongoingText),
	})
}

func (e *Engine) appendLocalEntry(entry storedLocalEntry) {
	if entry.Role == "" || entry.Text == "" {
		return
	}
	e.transcriptPersistence().AppendLocalEntryRecord(*localEntryChatEntry(entry))
	e.emit(Event{Kind: EventLocalEntryAdded, LocalEntry: localEntryChatEntry(entry)})
	e.emitConversationUpdated("")
}

func (e *Engine) RecordPromptHistory(text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	_, err := e.store.AppendEvent("", "prompt_history", map[string]any{"text": text})
	return err
}

func (e *Engine) SetOngoingError(text string) {
	e.transcriptPersistence().SetOngoingError(text)
	e.emit(Event{Kind: EventOngoingErrorUpdated})
}

func (e *Engine) ClearOngoingError() {
	e.transcriptPersistence().ClearOngoingError()
	e.emit(Event{Kind: EventOngoingErrorUpdated})
}

func (e *Engine) SetSessionName(name string) error {
	return e.store.SetName(name)
}

func (e *Engine) SetThinkingLevel(level string) error {
	normalized, ok := NormalizeThinkingLevel(level)
	if !ok {
		return fmt.Errorf("invalid thinking level %q (expected low|medium|high|xhigh)", strings.TrimSpace(level))
	}
	e.mu.Lock()
	e.cfg.ThinkingLevel = normalized
	e.mu.Unlock()
	e.markCurrentRequestShapeDirty()
	return nil
}

func (e *Engine) SetFastModeEnabled(enabled bool) (bool, error) {
	if enabled && !e.FastModeAvailable() {
		return false, errors.New("fast mode is only available for OpenAI-based Responses providers")
	}
	e.mu.Lock()
	if e.cfg.FastModeState != nil {
		changed := e.cfg.FastModeState.SetEnabled(enabled)
		e.mu.Unlock()
		if changed {
			e.markCurrentRequestShapeDirty()
		}
		return changed, nil
	}
	if e.cfg.FastModeEnabled == enabled {
		e.mu.Unlock()
		return false, nil
	}
	e.cfg.FastModeEnabled = enabled
	e.mu.Unlock()
	e.markCurrentRequestShapeDirty()
	return true, nil
}

func (e *Engine) SetAutoCompactionEnabled(enabled bool) (bool, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	current := true
	if e.cfg.AutoCompactionEnabled != nil {
		current = *e.cfg.AutoCompactionEnabled
	}
	if current == enabled {
		return false, current
	}
	if e.cfg.AutoCompactionEnabled == nil {
		e.cfg.AutoCompactionEnabled = new(bool)
	}
	*e.cfg.AutoCompactionEnabled = enabled
	return true, enabled
}

func (e *Engine) SetReviewerEnabled(enabled bool) (bool, string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	current, ok := NormalizeReviewerFrequency(e.cfg.Reviewer.Frequency)
	if !ok {
		current = "off"
	}
	reviewerState := e.reviewerRuntimeStateLocked()
	if current != "off" {
		reviewerState.RecordResumeFrequency(current)
	}

	if enabled {
		if current != "off" {
			return false, current, nil
		}
		if err := e.initReviewerClientLocked(); err != nil {
			return false, current, err
		}
		target := reviewerState.ResumeFrequency("edits")
		e.cfg.Reviewer.Frequency = target
		return true, target, nil
	}

	if current == "off" {
		return false, current, nil
	}
	e.cfg.Reviewer.Frequency = "off"
	return true, "off", nil
}

func (e *Engine) ThinkingLevel() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return strings.TrimSpace(e.cfg.ThinkingLevel)
}

func (e *Engine) FastModeEnabled() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cfg.FastModeState != nil {
		return e.cfg.FastModeState.Enabled()
	}
	return e.cfg.FastModeEnabled
}

func (e *Engine) FastModeAvailable() bool {
	caps, err := e.providerCapabilities(context.Background())
	if err != nil {
		return false
	}
	return llm.SupportsFastModeProvider(caps)
}

func (e *Engine) ReviewerFrequency() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	normalized, ok := NormalizeReviewerFrequency(e.cfg.Reviewer.Frequency)
	if !ok {
		return "off"
	}
	return normalized
}

func (e *Engine) reviewerMetaTimestamp() time.Time {
	if e == nil || e.store == nil {
		return time.Now().UTC()
	}
	if createdAt := e.store.Meta().CreatedAt; !createdAt.IsZero() {
		return createdAt.UTC()
	}
	return time.Now().UTC()
}

func (e *Engine) ReviewerEnabled() bool {
	return e.ReviewerFrequency() != "off"
}

func (e *Engine) AutoCompactionEnabled() bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.cfg.AutoCompactionEnabled == nil {
		return true
	}
	return *e.cfg.AutoCompactionEnabled
}

func (e *Engine) CompactionMode() string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.compactionPlannerState().mode(e.cfg.CompactionMode)
}

func (e *Engine) initReviewerClient() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.initReviewerClientLocked()
}

func (e *Engine) initReviewerClientLocked() error {
	return e.reviewerRuntimeStateLocked().EnsureClient(e.cfg.Reviewer.ClientFactory)
}

func (e *Engine) reviewerClientSnapshot() llm.Client {
	return e.reviewerRuntimeState().Client()
}

func (e *Engine) reviewerTurnConfigSnapshot() (string, llm.Client) {
	e.mu.Lock()
	reviewerState := e.reviewerRuntimeStateLocked()
	normalized, ok := NormalizeReviewerFrequency(e.cfg.Reviewer.Frequency)
	if !ok {
		normalized = "off"
	}
	e.mu.Unlock()
	return normalized, reviewerState.Client()
}

func (e *Engine) reviewerRequestConfigSnapshot() reviewerRequestConfig {
	e.mu.Lock()
	defer e.mu.Unlock()
	return reviewerRequestConfig{
		Model:             strings.TrimSpace(e.cfg.Reviewer.Model),
		ThinkingLevel:     strings.TrimSpace(e.cfg.Reviewer.ThinkingLevel),
		ModelCapabilities: e.cfg.Reviewer.ModelCapabilities,
	}
}

func (e *Engine) SessionName() string {
	return strings.TrimSpace(e.store.Meta().Name)
}

func (e *Engine) SessionID() string {
	return strings.TrimSpace(e.store.Meta().SessionID)
}

func (e *Engine) compactionCountSnapshot() int {
	return e.compactionRuntimeState().Count()
}

func (e *Engine) conversationSessionID() string {
	return e.SessionID()

}

func conversationPromptCacheKey(sessionID string, compactionCount int) string {
	trimmed := strings.TrimSpace(sessionID)
	if trimmed == "" {
		return ""
	}
	if compactionCount <= 0 {
		return trimmed
	}
	return fmt.Sprintf("%s/compact-%d", trimmed, compactionCount)
}

func (e *Engine) conversationPromptCacheKey() string {
	return conversationPromptCacheKey(e.conversationSessionID(), e.compactionCountSnapshot())
}

func (e *Engine) ParentSessionID() string {
	return strings.TrimSpace(e.store.Meta().ParentSessionID)
}

func (e *Engine) SetTranscriptWorkingDir(workdir string) {
	if e == nil {
		return
	}
	e.transcriptRuntimeState().SetWorkingDir(workdir)
}

func (e *Engine) transcriptWorkingDir() string {
	if e == nil {
		return ""
	}
	return e.transcriptRuntimeState().WorkingDir()
}

func transcriptWorkingDir(primary string, fallback string) string {
	if trimmed := strings.TrimSpace(primary); trimmed != "" {
		return trimmed
	}
	return strings.TrimSpace(fallback)
}

func (e *Engine) ConversationFreshness() session.ConversationFreshness {
	return e.store.ConversationFreshness()
}

func mustJSON(v any) json.RawMessage {
	b, _ := json.Marshal(v)
	return b
}

type storedLocalEntry struct {
	Visibility    transcript.EntryVisibility `json:"visibility,omitempty"`
	Role          string                     `json:"role"`
	Text          string                     `json:"text"`
	OngoingText   string                     `json:"ongoing_text,omitempty"`
	DiagnosticKey string                     `json:"diagnostic_key,omitempty"`
	NoticeID      string                     `json:"notice_id,omitempty"`
}

type historyReplacementPayload struct {
	Engine string             `json:"engine"`
	Mode   string             `json:"mode"`
	Items  []llm.ResponseItem `json:"items"`
}

func toToolNames(ids []toolspec.ID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		if id == "" {
			continue
		}
		out = append(out, string(id))
	}
	return out
}

func (e *Engine) lastUsageSnapshot() llm.Usage {
	return e.usageTrackingState().Last()
}

func (e *Engine) setLastUsage(usage llm.Usage) {
	baselineEstimate := 0
	if e != nil {
		baselineEstimate = e.transcriptRuntimeState().EstimatedProviderTokens()
	}
	normalizedUsage, totalInputTokens, totalCachedInputTokens := e.nextUsageTrackingState(usage)
	e.applyUsageTrackingState(normalizedUsage, baselineEstimate, totalInputTokens, totalCachedInputTokens)
}

func (e *Engine) recordLastUsage(usage llm.Usage) error {
	baselineEstimate := 0
	if e != nil {
		baselineEstimate = e.transcriptRuntimeState().EstimatedProviderTokens()
	}
	normalizedUsage, totalInputTokens, totalCachedInputTokens := e.nextUsageTrackingState(usage)
	if e != nil && e.store != nil {
		if err := e.store.SetUsageState(&session.UsageState{
			InputTokens:             normalizedUsage.InputTokens,
			OutputTokens:            normalizedUsage.OutputTokens,
			WindowTokens:            normalizedUsage.WindowTokens,
			CachedInputTokens:       normalizedUsage.CachedInputTokens,
			HasCachedInputTokens:    normalizedUsage.HasCachedInputTokens,
			EstimatedProviderTokens: baselineEstimate,
			TotalInputTokens:        totalInputTokens,
			TotalCachedInputTokens:  totalCachedInputTokens,
		}); err != nil {
			return err
		}
	}
	e.applyUsageTrackingState(normalizedUsage, baselineEstimate, totalInputTokens, totalCachedInputTokens)
	return nil
}

func (e *Engine) restorePersistedUsageState(state *session.UsageState) {
	if e == nil || state == nil {
		return
	}
	normalized := normalizePersistedUsageState(*state)
	e.applyUsageTrackingState(
		llm.Usage{
			InputTokens:          normalized.InputTokens,
			OutputTokens:         normalized.OutputTokens,
			WindowTokens:         normalized.WindowTokens,
			CachedInputTokens:    normalized.CachedInputTokens,
			HasCachedInputTokens: normalized.HasCachedInputTokens,
		},
		normalized.EstimatedProviderTokens,
		normalized.TotalInputTokens,
		normalized.TotalCachedInputTokens,
	)
}

func normalizePersistedUsageState(state session.UsageState) session.UsageState {
	return normalizePersistedUsageTrackingState(state)
}

func (e *Engine) nextUsageTrackingState(usage llm.Usage) (llm.Usage, int, int) {
	return e.usageTrackingState().Next(usage)
}

func (e *Engine) applyUsageTrackingState(usage llm.Usage, baselineEstimate, totalInputTokens, totalCachedInputTokens int) {
	if baselineEstimate < 0 {
		baselineEstimate = 0
	}
	e.usageTrackingState().Apply(usage, totalInputTokens, totalCachedInputTokens)
	if e.modelRequests().TokenUsage() != nil {
		e.modelRequests().TokenUsage().storeUsageBaseline(usage.InputTokens, baselineEstimate)
	}
}

func (e *Engine) cacheHitSnapshot() (int, bool) {
	return e.usageTrackingState().CacheHitSnapshot()
}

func (e *Engine) usageTrackingState() *usageTrackingState {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.usageState == nil {
		e.usageState = newUsageTrackingState()
	}
	return e.usageState
}

func (e *Engine) reviewerRuntimeState() *reviewerRuntimeState {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.reviewerRuntimeStateLocked()
}

func (e *Engine) reviewerRuntimeStateLocked() *reviewerRuntimeState {
	if e.reviewerState == nil {
		e.reviewerState = newReviewerRuntimeState(e.cfg.Reviewer.Client)
	}
	return e.reviewerState
}

func (e *Engine) transcriptRuntimeState() *transcriptRuntimeState {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.transcriptState == nil {
		e.transcriptState = newTranscriptRuntimeState(transcriptWorkingDir(e.cfg.TranscriptWorkingDir, e.store.Meta().WorkspaceRoot))
	}
	return e.transcriptState
}

func (e *Engine) transcriptPersistence() transcriptPersistenceCoordinator {
	return newTranscriptPersistenceCoordinator(e.transcriptRuntimeState())
}

func (e *Engine) lockedContractState() *lockedContractState {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.lockedState == nil {
		e.lockedState = newLockedContractState()
	}
	return e.lockedState
}

func (e *Engine) modelRequests() *modelRequestRuntimeState {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.modelRequestsState == nil {
		e.modelRequestsState = newModelRequestRuntimeState()
	}
	return e.modelRequestsState
}

func (e *Engine) emit(evt Event) {
	evt.TranscriptRevision = e.TranscriptRevision()
	evt.CommittedEntryCount = e.CommittedTranscriptEntryCount()
	if evt.ContextUsage == nil && eventShouldCarryContextUsage(evt) {
		usage := e.ContextUsage()
		evt.ContextUsage = &usage
	}
	if !evt.CommittedEntryStartSet && eventMayInferCommittedEntryStart(evt.Kind) {
		entries := TranscriptEntriesFromEvent(evt)
		if len(entries) > 0 {
			start := evt.CommittedEntryCount - len(entries)
			if start < 0 {
				start = 0
			}
			evt.CommittedEntryStart = start
			evt.CommittedEntryStartSet = true
		}
	}
	if e.cfg.OnEvent != nil {
		e.cfg.OnEvent(evt)
	}
}

func eventShouldCarryContextUsage(evt Event) bool {
	switch evt.Kind {
	case EventModelResponse, EventUserMessageFlushed, EventCompactionCompleted, EventCompactionFailed:
		return true
	case EventAssistantMessage, EventToolCallStarted, EventToolCallCompleted, EventLocalEntryAdded, EventCacheWarning, EventConversationUpdated:
		return evt.CommittedTranscriptChanged
	default:
		return false
	}
}

func (e *Engine) emitConversationUpdated(stepID string) {
	e.emit(Event{Kind: EventConversationUpdated, StepID: stepID})
}

func (e *Engine) emitCommittedTranscriptAdvanced(stepID string) {
	e.emit(Event{Kind: EventConversationUpdated, StepID: stepID, CommittedTranscriptChanged: true})
}

func (e *Engine) emitCommittedMessageTranscriptAdvanced(stepID string, msg llm.Message) {
	e.emit(Event{Kind: EventConversationUpdated, StepID: stepID, CommittedTranscriptChanged: true, Message: msg})
}

func eventMayInferCommittedEntryStart(kind EventKind) bool {
	switch kind {
	case EventCompactionCompleted, EventCompactionFailed:
		return false
	default:
		return true
	}
}

func (e *Engine) rememberPendingToolCallStarts(starts map[string]int) {
	if e == nil {
		return
	}
	e.pendingToolCallStartStore().Remember(starts)
}

func (e *Engine) pendingToolCallStart(callID string) (int, bool) {
	if e == nil {
		return 0, false
	}
	return e.pendingToolCallStartStore().Lookup(callID)
}

func (e *Engine) forgetPendingToolCallStart(callID string) {
	if e == nil {
		return
	}
	e.pendingToolCallStartStore().Forget(callID)
}

func (e *Engine) pendingToolCallStartStore() *pendingToolCallStartStore {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.toolCallStarts == nil {
		e.toolCallStarts = newPendingToolCallStartStore()
	}
	return e.toolCallStarts
}

func (e *Engine) nextCompactionCount() int {
	return e.compactionRuntimeState().IncrementCount()
}

func (e *Engine) compactionRuntimeState() *compactionRuntimeState {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.compactionState == nil {
		e.compactionState = newCompactionRuntimeState()
	}
	return e.compactionState
}
