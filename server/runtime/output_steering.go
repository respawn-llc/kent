package runtime

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"core/server/llm"
	"core/server/session"
	"core/server/tools"
	"core/shared/cachewarn"
	"core/shared/transcript"
)

type steeringPriority int

const (
	steeringPriorityRuntimeContext steeringPriority = iota
	steeringPriorityUser
	steeringPriorityNormal
	steeringPriorityRuntimeEvent
)

type steeringIntent struct {
	priority steeringPriority
	items    []steeringItem
}

type steeringItem struct {
	message          *steeringMessage
	localEntry       *steeringLocalEntry
	historyReplace   *steeringHistoryReplacement
	toolCompletion   *tools.Result
	queuedFlush      *steeringQueuedUserMessageFlush
	event            *Event
	streaming        *steeringStreamingOutput
	cacheWarning     *steeringCacheWarning
	cacheObservation *steeringCacheObservation
	repairReload     *steeringRepairReload
}

type steeringMessage struct {
	message     llm.Message
	eventPolicy steeringMessageEventPolicy
	persist     bool
}

type steeringLocalEntry struct {
	entry storedLocalEntry
}

type steeringHistoryReplacement struct {
	payload          historyReplacementPayload
	projectedEntries []ChatEntry
	workflowRunID    string
}

type steeringStreamingOutput struct {
	assistantDelta *string
	reasoningDelta *llm.ReasoningSummaryDelta
	clear          bool
}

type steeringCacheWarning struct {
	warning    cachewarn.Warning
	visibility transcript.EntryVisibility
	emit       bool
}

type steeringCacheObservation struct {
	events     []session.EventInput
	warning    cachewarn.Warning
	visibility transcript.EntryVisibility
	emit       bool
}

type steeringRepairReload struct {
	rewrite                 session.EventRewriteResult
	removedCallIDs          []string
	removedToolCallIDs      []string
	preRepairCommittedCount int
}

type steeringQueuedUserMessageFlush struct {
	text         string
	batch        []string
	queueItemIDs []string
}

type steeringMessageEventPolicy uint8

const (
	steeringMessageEventDefault steeringMessageEventPolicy = iota
	steeringMessageEventNone
)

func steerMessageIntent(msg llm.Message) steeringIntent {
	return steerMessagesIntent(steeringPriorityNormal, steeringMessageEventDefault, []llm.Message{msg})
}

func steerMessageWithoutDerivedEventIntent(msg llm.Message) steeringIntent {
	return steerMessagesIntent(steeringPriorityNormal, steeringMessageEventNone, []llm.Message{msg})
}

func steerRuntimeContextMessagesIntent(messages []llm.Message) steeringIntent {
	return steerMessagesIntent(steeringPriorityRuntimeContext, steeringMessageEventDefault, messages)
}

func steerUserMessageIntent(msg llm.Message) steeringIntent {
	return steerMessagesIntent(steeringPriorityUser, steeringMessageEventDefault, []llm.Message{msg})
}

func steerUserMessageWithoutDerivedEventIntent(msg llm.Message) steeringIntent {
	return steerMessagesIntent(steeringPriorityUser, steeringMessageEventNone, []llm.Message{msg})
}

func steerMessagesIntent(priority steeringPriority, eventPolicy steeringMessageEventPolicy, messages []llm.Message) steeringIntent {
	return steerMessagesWithPersistenceIntent(priority, eventPolicy, true, messages)
}

func steerStoredMessageProjectionIntent(msg llm.Message) steeringIntent {
	return steerMessagesWithPersistenceIntent(steeringPriorityNormal, steeringMessageEventDefault, false, []llm.Message{msg})
}

func steerMessagesWithPersistenceIntent(priority steeringPriority, eventPolicy steeringMessageEventPolicy, persist bool, messages []llm.Message) steeringIntent {
	items := make([]steeringItem, 0, len(messages))
	for _, message := range messages {
		msg := message
		items = append(items, steeringItem{message: &steeringMessage{
			message:     msg,
			eventPolicy: eventPolicy,
			persist:     persist,
		}})
	}
	return steeringIntent{priority: priority, items: items}
}

func steerLocalEntryIntent(entry storedLocalEntry) steeringIntent {
	copyEntry := entry
	return steeringIntent{
		priority: steeringPriorityNormal,
		items: []steeringItem{{localEntry: &steeringLocalEntry{
			entry: copyEntry,
		}}},
	}
}

func steerHistoryReplacementIntent(engine string, mode compactionMode, workflowRunID string, items []llm.ResponseItem) steeringIntent {
	preparedItems := llm.PrepareOpenAIInputItems(items)
	payload := historyReplacementPayload{
		Engine:        normalizeHistoryReplacementEngine(engine),
		Mode:          string(mode),
		WorkflowRunID: workflowRunID,
		Items:         llm.CloneResponseItems(preparedItems),
	}
	return steeringIntent{
		priority: steeringPriorityNormal,
		items: []steeringItem{{historyReplace: &steeringHistoryReplacement{
			payload:          payload,
			projectedEntries: transcriptEntriesFromHistoryReplacement(payload.Items),
			workflowRunID:    workflowRunID,
		}}},
	}
}

func steerToolCompletionIntent(result tools.Result) steeringIntent {
	copyResult := cloneToolResult(result)
	return steeringIntent{
		priority: steeringPriorityNormal,
		items:    []steeringItem{{toolCompletion: &copyResult}},
	}
}

func steerQueuedUserMessageFlushIntent(text string, batch []string, queueItemIDs []string) steeringIntent {
	return steeringIntent{
		priority: steeringPriorityUser,
		items: []steeringItem{{queuedFlush: &steeringQueuedUserMessageFlush{
			text:         text,
			batch:        append([]string(nil), batch...),
			queueItemIDs: append([]string(nil), queueItemIDs...),
		}}},
	}
}

func steerEventIntent(evt Event) steeringIntent {
	copyEvent := evt
	return steeringIntent{
		priority: steeringPriorityRuntimeEvent,
		items:    []steeringItem{{event: &copyEvent}},
	}
}

func steerAssistantDeltaIntent(delta string) steeringIntent {
	copyDelta := delta
	return steeringIntent{
		priority: steeringPriorityRuntimeEvent,
		items:    []steeringItem{{streaming: &steeringStreamingOutput{assistantDelta: &copyDelta}}},
	}
}

func steerReasoningDeltaIntent(delta llm.ReasoningSummaryDelta) steeringIntent {
	copyDelta := delta
	return steeringIntent{
		priority: steeringPriorityRuntimeEvent,
		items:    []steeringItem{{streaming: &steeringStreamingOutput{reasoningDelta: &copyDelta}}},
	}
}

func steerClearStreamingStateIntent() steeringIntent {
	return steeringIntent{
		priority: steeringPriorityRuntimeEvent,
		items:    []steeringItem{{streaming: &steeringStreamingOutput{clear: true}}},
	}
}

func steerCacheWarningIntent(warning cachewarn.Warning, visibility transcript.EntryVisibility, emit bool) steeringIntent {
	copyWarning := warning
	return steeringIntent{
		priority: steeringPriorityRuntimeEvent,
		items: []steeringItem{{cacheWarning: &steeringCacheWarning{
			warning:    copyWarning,
			visibility: transcript.NormalizeEntryVisibility(visibility),
			emit:       emit,
		}}},
	}
}

func steerCacheObservationIntent(events []session.EventInput, warning cachewarn.Warning, visibility transcript.EntryVisibility, emit bool) steeringIntent {
	copyEvents := make([]session.EventInput, len(events))
	copy(copyEvents, events)
	copyWarning := warning
	return steeringIntent{
		priority: steeringPriorityRuntimeEvent,
		items: []steeringItem{{cacheObservation: &steeringCacheObservation{
			events:     copyEvents,
			warning:    copyWarning,
			visibility: transcript.NormalizeEntryVisibility(visibility),
			emit:       emit,
		}}},
	}
}

func steerRepairReloadIntent(rewrite session.EventRewriteResult, removedCallIDs []string, removedToolCallIDs []string, preRepairCommittedCount int) steeringIntent {
	copyRemovedCallIDs := append([]string(nil), removedCallIDs...)
	copyRemovedToolCallIDs := append([]string(nil), removedToolCallIDs...)
	return steeringIntent{
		priority: steeringPriorityRuntimeEvent,
		items: []steeringItem{{repairReload: &steeringRepairReload{
			rewrite:                 rewrite,
			removedCallIDs:          copyRemovedCallIDs,
			removedToolCallIDs:      copyRemovedToolCallIDs,
			preRepairCommittedCount: preRepairCommittedCount,
		}}},
	}
}

func (e *Engine) steerEvent(stepID string, evt Event) error {
	return e.steer(stepID, steerEventIntent(evt))
}

func (e *Engine) steer(stepID string, intents ...steeringIntent) error {
	ordered := make([]steeringIntent, 0, len(intents))
	for _, intent := range intents {
		if len(intent.items) == 0 {
			continue
		}
		ordered = append(ordered, intent)
	}
	if len(ordered) == 0 {
		return nil
	}
	e.outputMutationMu.Lock()
	defer e.outputMutationMu.Unlock()
	sort.SliceStable(ordered, func(i, j int) bool {
		return ordered[i].priority < ordered[j].priority
	})
	for _, intent := range ordered {
		for _, item := range intent.items {
			if err := e.applySteeringItem(stepID, item); err != nil {
				return err
			}
		}
	}
	return nil
}

func (e *Engine) applySteeringItem(stepID string, item steeringItem) error {
	if item.message != nil {
		return e.appendMessageRaw(stepID, item.message.message, item.message.eventPolicy, item.message.persist)
	}
	if item.localEntry != nil {
		return e.appendPersistedLocalEntryRecordRaw(stepID, item.localEntry.entry)
	}
	if item.historyReplace != nil {
		return e.replaceHistoryRaw(stepID, *item.historyReplace)
	}
	if item.toolCompletion != nil {
		if err := e.persistToolCompletionRaw(stepID, *item.toolCompletion); err != nil {
			return err
		}
		result := cloneToolResult(*item.toolCompletion)
		e.emitRaw(Event{Kind: EventToolCallCompleted, StepID: stepID, ToolResult: &result, CommittedTranscriptChanged: true})
		return nil
	}
	if item.queuedFlush != nil {
		return e.appendQueuedUserMessageFlush(stepID, item.queuedFlush.text, item.queuedFlush.batch, item.queuedFlush.queueItemIDs)
	}
	if item.event != nil {
		evt := *item.event
		if evt.StepID == "" {
			evt.StepID = stepID
		}
		e.emitRaw(evt)
	}
	if item.cacheWarning != nil {
		warning := item.cacheWarning.warning
		visibility := transcript.NormalizeEntryVisibility(item.cacheWarning.visibility)
		e.transcriptPersistence().AppendCommittedEntryWithVisibility(cacheWarningTranscriptRole, cachewarn.Text(warning), visibility)
		if item.cacheWarning.emit {
			e.emitRaw(Event{Kind: EventCacheWarning, StepID: stepID, CacheWarning: copyCacheWarning(&warning), CacheWarningVisibility: visibility, CommittedTranscriptChanged: true})
		}
		return nil
	}
	if item.cacheObservation != nil {
		observation := item.cacheObservation
		if e.beforePersistCacheObservation != nil {
			if err := e.beforePersistCacheObservation(observation.events); err != nil {
				return err
			}
		}
		if _, err := e.store.AppendTurnAtomic(stepID, observation.events); err != nil {
			return err
		}
		warning := observation.warning
		visibility := transcript.NormalizeEntryVisibility(observation.visibility)
		e.transcriptPersistence().AppendCommittedEntryWithVisibility(cacheWarningTranscriptRole, cachewarn.Text(warning), visibility)
		if observation.emit {
			e.emitRaw(Event{Kind: EventCacheWarning, StepID: stepID, CacheWarning: copyCacheWarning(&warning), CacheWarningVisibility: visibility, CommittedTranscriptChanged: true})
		}
		return nil
	}
	if item.repairReload != nil {
		return e.applyRepairReloadRaw(stepID, *item.repairReload)
	}
	if item.streaming != nil {
		if item.streaming.assistantDelta != nil {
			delta := *item.streaming.assistantDelta
			e.transcriptPersistence().AppendOngoingDelta(delta)
			e.emitRaw(Event{Kind: EventAssistantDelta, StepID: stepID, AssistantDelta: delta})
			return nil
		}
		if item.streaming.reasoningDelta != nil {
			delta := *item.streaming.reasoningDelta
			e.emitRaw(Event{Kind: EventReasoningDelta, StepID: stepID, ReasoningDelta: &delta})
			return nil
		}
		if item.streaming.clear {
			e.clearStreamingAssistantStateRaw(stepID)
			return nil
		}
	}
	return nil
}

func (e *Engine) applyRepairReloadRaw(stepID string, repair steeringRepairReload) error {
	if e == nil {
		return nil
	}
	appendedEntries := make([]struct {
		stepID string
		entry  *ChatEntry
	}, 0, len(repair.rewrite.AppendedEvents))
	for _, appended := range repair.rewrite.AppendedEvents {
		if strings.TrimSpace(appended.Kind) != "local_entry" {
			continue
		}
		var entry storedLocalEntry
		if err := json.Unmarshal(appended.Payload, &entry); err != nil {
			return fmt.Errorf("decode repair warning event: %w", err)
		}
		chatEntry := localEntryChatEntry(entry)
		eventStepID := strings.TrimSpace(appended.StepID)
		if eventStepID == "" {
			eventStepID = stepID
		}
		appendedEntries = append(appendedEntries, struct {
			stepID string
			entry  *ChatEntry
		}{
			stepID: eventStepID,
			entry:  chatEntry,
		})
	}
	if err := e.applyMissingToolOutputRepairProjection(repair); err != nil {
		return err
	}
	committedBaseCount := e.CommittedTranscriptEntryCount() - len(appendedEntries)
	if committedBaseCount < 0 {
		committedBaseCount = 0
	}
	for idx, appended := range appendedEntries {
		e.emitRepairLocalEntryAddedRaw(Event{
			Kind:                       EventLocalEntryAdded,
			StepID:                     appended.stepID,
			LocalEntry:                 appended.entry,
			CommittedTranscriptChanged: true,
			TranscriptRevision:         repair.rewrite.LastSequence,
			CommittedEntryStart:        committedBaseCount + idx,
			CommittedEntryStartSet:     true,
			CommittedEntryCount:        committedBaseCount + idx + 1,
		})
	}
	e.emitRaw(Event{Kind: EventConversationUpdated, StepID: stepID, CommittedTranscriptChanged: true})
	return nil
}

func (e *Engine) emitRepairLocalEntryAddedRaw(evt Event) {
	if evt.ContextUsage == nil && eventShouldCarryContextUsage(evt) {
		usage := e.ContextUsage()
		evt.ContextUsage = &usage
	}
	if e.cfg.OnEvent != nil {
		e.cfg.OnEvent(evt)
	}
}

func (e *Engine) replaceHistoryRaw(stepID string, replacement steeringHistoryReplacement) error {
	reminderIssued := false
	projectedStart := e.CommittedTranscriptEntryCount()
	preparedItems := llm.CloneResponseItems(replacement.payload.Items)
	// Compaction reinjects base meta into the same replacement payload, so a
	// non-empty replacement active list is born already carrying it. Mirror the
	// restore-time length signal here rather than scanning the items.
	e.baseMetaInjected = len(preparedItems) > 0
	_, committed, appendErr := e.store.AppendEvent(stepID, "history_replaced", replacement.payload)
	if appendErr != nil && !committed {
		return appendErr
	}
	// The committed event is the single durable record of this compaction's
	// provenance; mirror it into runtime state so an in-process gate sees it
	// without re-reading the transcript, matching what restore reconstructs.
	e.setLastCompactionWorkflowRunID(replacement.workflowRunID)
	e.resetCurrentPreciseInputTracking()
	e.resetLocalDiagnostics()
	e.transcriptPersistence().ReplaceHistory(preparedItems)
	e.setCompactionSoonReminderIssued(false)
	e.emitProjectedHistoryReplacementEntriesRaw(stepID, projectedStart, replacement.projectedEntries)
	e.emitRaw(Event{Kind: EventConversationUpdated, StepID: stepID})
	return errors.Join(
		appendErr,
		e.store.SetCompactionSoonReminderIssued(reminderIssued),
		e.store.SetUsageState(nil),
	)
}

func (e *Engine) emitProjectedHistoryReplacementEntriesRaw(stepID string, start int, entries []ChatEntry) {
	if e == nil || len(entries) == 0 {
		return
	}
	// Live subscribers must observe the same committed transcript progression that
	// restart hydration reconstructs from history_replaced. Emit projected
	// compaction rows before any later local entry.
	if start < 0 {
		start = 0
	}
	for idx, entry := range entries {
		copyEntry := clonePersistedChatEntry(entry)
		e.emitRaw(Event{
			Kind:                       EventLocalEntryAdded,
			StepID:                     stepID,
			LocalEntry:                 &copyEntry,
			CommittedTranscriptChanged: true,
			CommittedEntryStart:        start + idx,
			CommittedEntryStartSet:     true,
		})
	}
}

func cloneToolResult(result tools.Result) tools.Result {
	copyResult := result
	copyResult.Output = append(json.RawMessage(nil), result.Output...)
	copyResult.Presentation = clonePersistedToolCallMeta(result.Presentation)
	return copyResult
}
