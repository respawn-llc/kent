package runtime

import (
	"encoding/json"
	"sort"

	"builder/server/llm"
	"builder/server/tools"
	"builder/shared/cachewarn"
	"builder/shared/transcript"
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
	message        *steeringMessage
	localEntry     *steeringLocalEntry
	toolCompletion *tools.Result
	event          *Event
	streaming      *steeringStreamingOutput
	cacheWarning   *steeringCacheWarning
}

type steeringMessage struct {
	message     llm.Message
	eventPolicy steeringMessageEventPolicy
	persist     bool
}

type steeringLocalEntry struct {
	entry   storedLocalEntry
	persist bool
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
	return steerLocalEntryWithPersistenceIntent(entry, true)
}

func steerTransientLocalEntryIntent(entry storedLocalEntry) steeringIntent {
	return steerLocalEntryWithPersistenceIntent(entry, false)
}

func steerLocalEntryWithPersistenceIntent(entry storedLocalEntry, persist bool) steeringIntent {
	copyEntry := entry
	return steeringIntent{
		priority: steeringPriorityNormal,
		items: []steeringItem{{localEntry: &steeringLocalEntry{
			entry:   copyEntry,
			persist: persist,
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

func (e *Engine) steerEvent(stepID string, evt Event) error {
	return e.steer(stepID, steerEventIntent(evt))
}

func (e *Engine) steerConversationUpdated(stepID string) error {
	return e.steerEvent(stepID, Event{Kind: EventConversationUpdated, StepID: stepID})
}

func (e *Engine) steerCommittedTranscriptAdvanced(stepID string) error {
	return e.steerEvent(stepID, Event{Kind: EventConversationUpdated, StepID: stepID, CommittedTranscriptChanged: true})
}

func (e *Engine) steerCommittedMessageTranscriptAdvanced(stepID string, msg llm.Message) error {
	return e.steerEvent(stepID, Event{Kind: EventConversationUpdated, StepID: stepID, CommittedTranscriptChanged: true, Message: msg})
}

func (e *Engine) steer(stepID string, intents ...steeringIntent) error {
	ordered := make([]steeringIntent, 0, len(intents))
	for _, intent := range intents {
		if len(intent.items) == 0 {
			continue
		}
		ordered = append(ordered, intent)
	}
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
		if item.localEntry.persist {
			return e.appendPersistedLocalEntryRecordRaw(stepID, item.localEntry.entry)
		}
		e.appendTransientLocalEntryRecordRaw(item.localEntry.entry)
		return nil
	}
	if item.toolCompletion != nil {
		if err := e.persistToolCompletionRaw(stepID, *item.toolCompletion); err != nil {
			return err
		}
		result := cloneToolResult(*item.toolCompletion)
		e.emitRaw(Event{Kind: EventToolCallCompleted, StepID: stepID, ToolResult: &result, CommittedTranscriptChanged: true})
		return nil
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
		e.transcriptPersistence().AppendLocalEntryWithVisibility(cacheWarningTranscriptRole, cachewarn.Text(warning), visibility)
		if item.cacheWarning.emit {
			e.emitRaw(Event{Kind: EventCacheWarning, StepID: stepID, CacheWarning: copyCacheWarning(&warning), CacheWarningVisibility: visibility, CommittedTranscriptChanged: true})
		}
		return nil
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

func cloneToolResult(result tools.Result) tools.Result {
	copyResult := result
	copyResult.Output = append(json.RawMessage(nil), result.Output...)
	copyResult.Presentation = clonePersistedToolCallMeta(result.Presentation)
	return copyResult
}
