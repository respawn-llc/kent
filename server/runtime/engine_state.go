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
	return e.chat.snapshotMessages()
}

func (e *Engine) snapshotItems() []llm.ResponseItem {
	return e.chat.snapshotItems()
}

func (e *Engine) ChatSnapshot() ChatSnapshot {
	return e.chat.snapshot()
}

func (e *Engine) OngoingTailTranscriptWindow(maxEntries int) TranscriptWindowSnapshot {
	if e == nil || e.chat == nil {
		return TranscriptWindowSnapshot{}
	}
	return e.chat.ongoingTailSnapshot(maxEntries)
}

func (e *Engine) TranscriptPageSnapshot(offset, limit int) transcriptPageSnapshot {
	if e == nil || e.chat == nil {
		return transcriptPageSnapshot{}
	}
	return e.chat.transcriptPageSnapshot(offset, limit)
}

func (e *Engine) TranscriptRevision() int64 {
	if e == nil || e.store == nil {
		return 0
	}
	return e.store.Meta().LastSequence
}

func (e *Engine) CommittedTranscriptEntryCount() int {
	if e == nil || e.chat == nil {
		return 0
	}
	return e.chat.committedEntryCount()
}

func (e *Engine) ActiveRun() *RunSnapshot {
	if e == nil || e.stepLifecycle == nil {
		return nil
	}
	return e.stepLifecycle.Snapshot()
}

func (e *Engine) LastCommittedAssistantFinalAnswer() string {
	if e == nil || e.chat == nil {
		return ""
	}
	return e.chat.cachedLastCommittedAssistantFinalAnswer()
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
	e.chat.appendLocalEntryWithOngoingTextAndVisibility(entry.Role, entry.Text, entry.OngoingText, entry.Visibility)
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
	e.chat.setOngoingError(text)
	e.emit(Event{Kind: EventOngoingErrorUpdated})
}

func (e *Engine) ClearOngoingError() {
	e.chat.clearOngoingError()
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
	defer e.mu.Unlock()
	if e.cfg.FastModeState != nil {
		changed := e.cfg.FastModeState.SetEnabled(enabled)
		if changed {
			e.markCurrentRequestShapeDirty()
		}
		return changed, nil
	}
	if e.cfg.FastModeEnabled == enabled {
		return false, nil
	}
	e.cfg.FastModeEnabled = enabled
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
	if current != "off" {
		e.reviewerResumeFrequency = current
	}

	if enabled {
		if current != "off" {
			return false, current, nil
		}
		if err := e.initReviewerClientLocked(); err != nil {
			return false, current, err
		}
		target := e.reviewerResumeFrequency
		if target == "" || target == "off" {
			target = "edits"
		}
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
	normalized, ok := NormalizeCompactionMode(e.cfg.CompactionMode)
	if !ok {
		return "native"
	}
	return normalized
}

func (e *Engine) initReviewerClient() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.initReviewerClientLocked()
}

func (e *Engine) initReviewerClientLocked() error {
	if e.reviewer != nil {
		return nil
	}
	if e.cfg.Reviewer.ClientFactory == nil {
		return errors.New("reviewer client is not configured")
	}
	client, err := e.cfg.Reviewer.ClientFactory()
	if err != nil {
		return fmt.Errorf("configure reviewer client: %w", err)
	}
	e.reviewer = client
	return nil
}

func (e *Engine) reviewerClientSnapshot() llm.Client {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.reviewer
}

func (e *Engine) reviewerTurnConfigSnapshot() (string, llm.Client) {
	e.mu.Lock()
	defer e.mu.Unlock()
	normalized, ok := NormalizeReviewerFrequency(e.cfg.Reviewer.Frequency)
	if !ok {
		normalized = "off"
	}
	return normalized, e.reviewer
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
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.compactionCount
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
	trimmed := strings.TrimSpace(workdir)
	if trimmed == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.transcriptCWD = trimmed
}

func (e *Engine) transcriptWorkingDir() string {
	if e == nil {
		return ""
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return strings.TrimSpace(e.transcriptCWD)
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
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.lastUsage
}

func (e *Engine) setLastUsage(usage llm.Usage) {
	baselineEstimate := 0
	if e != nil && e.chat != nil {
		baselineEstimate = e.chat.estimatedProviderTokens()
	}
	normalizedUsage, totalInputTokens, totalCachedInputTokens := e.nextUsageTrackingState(usage)
	e.applyUsageTrackingState(normalizedUsage, baselineEstimate, totalInputTokens, totalCachedInputTokens)
}

func (e *Engine) recordLastUsage(usage llm.Usage) error {
	baselineEstimate := 0
	if e != nil && e.chat != nil {
		baselineEstimate = e.chat.estimatedProviderTokens()
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

func normalizeUsageForTracking(usage llm.Usage) llm.Usage {
	if usage.InputTokens < 0 {
		usage.InputTokens = 0
	}
	if usage.OutputTokens < 0 {
		usage.OutputTokens = 0
	}
	if usage.WindowTokens < 0 {
		usage.WindowTokens = 0
	}
	if usage.CachedInputTokens < 0 {
		usage.CachedInputTokens = 0
	}
	if usage.CachedInputTokens > usage.InputTokens {
		usage.CachedInputTokens = usage.InputTokens
	}
	return usage
}

func normalizePersistedUsageState(state session.UsageState) session.UsageState {
	if state.InputTokens < 0 {
		state.InputTokens = 0
	}
	if state.OutputTokens < 0 {
		state.OutputTokens = 0
	}
	if state.WindowTokens < 0 {
		state.WindowTokens = 0
	}
	if state.CachedInputTokens < 0 {
		state.CachedInputTokens = 0
	}
	if state.CachedInputTokens > state.InputTokens {
		state.CachedInputTokens = state.InputTokens
	}
	if state.EstimatedProviderTokens < 0 {
		state.EstimatedProviderTokens = 0
	}
	if state.TotalInputTokens < 0 {
		state.TotalInputTokens = 0
	}
	if state.TotalCachedInputTokens < 0 {
		state.TotalCachedInputTokens = 0
	}
	if state.TotalCachedInputTokens > state.TotalInputTokens {
		state.TotalCachedInputTokens = state.TotalInputTokens
	}
	return state
}

func nextUsageTotals(totalInputTokens, totalCachedInputTokens int, usage llm.Usage) (int, int) {
	if totalInputTokens < 0 {
		totalInputTokens = 0
	}
	if totalCachedInputTokens < 0 {
		totalCachedInputTokens = 0
	}
	if usage.HasCachedInputTokens && usage.InputTokens > 0 {
		totalInputTokens += usage.InputTokens
		totalCachedInputTokens += usage.CachedInputTokens
		if totalCachedInputTokens > totalInputTokens {
			totalCachedInputTokens = totalInputTokens
		}
	}
	return totalInputTokens, totalCachedInputTokens
}

func (e *Engine) nextUsageTrackingState(usage llm.Usage) (llm.Usage, int, int) {
	normalizedUsage := normalizeUsageForTracking(usage)
	e.mu.Lock()
	totalInputTokens := e.totalInputTokens
	totalCachedInputTokens := e.totalCachedInputTokens
	e.mu.Unlock()
	totalInputTokens, totalCachedInputTokens = nextUsageTotals(totalInputTokens, totalCachedInputTokens, normalizedUsage)
	return normalizedUsage, totalInputTokens, totalCachedInputTokens
}

func (e *Engine) applyUsageTrackingState(usage llm.Usage, baselineEstimate, totalInputTokens, totalCachedInputTokens int) {
	if baselineEstimate < 0 {
		baselineEstimate = 0
	}
	e.mu.Lock()
	e.lastUsage = usage
	e.totalInputTokens = totalInputTokens
	e.totalCachedInputTokens = totalCachedInputTokens
	e.mu.Unlock()
	if e.tokenUsage != nil {
		e.tokenUsage.storeUsageBaseline(usage.InputTokens, baselineEstimate)
	}
}

func (e *Engine) cacheHitSnapshot() (int, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.totalInputTokens <= 0 {
		return 0, false
	}
	cachedTokens := e.totalCachedInputTokens
	if cachedTokens < 0 {
		cachedTokens = 0
	}
	if cachedTokens > e.totalInputTokens {
		cachedTokens = e.totalInputTokens
	}
	pct := (cachedTokens * 100) / e.totalInputTokens
	if pct < 0 {
		return 0, false
	}
	if pct > 100 {
		return 100, true
	}
	return pct, true
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
	if e == nil || len(starts) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pendingToolCallStarts == nil {
		e.pendingToolCallStarts = make(map[string]int, len(starts))
	}
	for callID, start := range starts {
		e.pendingToolCallStarts[callID] = start
	}
}

func (e *Engine) pendingToolCallStart(callID string) (int, bool) {
	if e == nil || callID == "" {
		return 0, false
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	start, ok := e.pendingToolCallStarts[callID]
	if !ok {
		return 0, false
	}
	return start, true
}

func (e *Engine) forgetPendingToolCallStart(callID string) {
	if e == nil || callID == "" {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	delete(e.pendingToolCallStarts, callID)
}

func (e *Engine) nextCompactionCount() int {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.compactionCount++
	return e.compactionCount
}
