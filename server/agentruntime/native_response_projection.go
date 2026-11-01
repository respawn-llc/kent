package runtime

import (
	"core/server/llm"
	"core/server/tools"
	"core/shared/textutil"
	"core/shared/toolspec"
	"core/shared/transcriptmodel"
)

// Local call presentation may be normalized, but it must not change the
// provider's stored items or positions.
type nativeResponseMessage struct {
	OutputIndex       int64
	Message           llm.Message
	Reasoning         *ChatEntry
	ReasoningIdentity *TranscriptReasoningTraceIdentity
}

func (p nativeResponseMessage) entryCount() int {
	if p.Reasoning != nil {
		return 1
	}
	return len(VisibleChatEntriesFromMessage(p.Message))
}

func (e *Engine) steerProviderOutputItem(stepID string, item llm.ResponseItem) error {
	for _, hosted := range hostedToolExecutionsFromOutputItems([]llm.ResponseItem{item}, tools.DefinitionsFor(toolspec.CatalogIDs())) {
		call, err := normalizeToolCallForTranscriptChecked(hosted.Call, e.transcriptWorkingDir())
		if err != nil {
			return err
		}
		if err := e.steer(stepID, steerEventIntent(Event{
			Kind: EventToolCallStarted, StepID: exactStepIDPointer(stepID), ToolCall: &call,
		})); err != nil {
			return err
		}
	}
	return nil
}

func nativeResponseMessages(message llm.Message) []nativeResponseMessage {
	calls := make(map[string]llm.ToolCall, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		calls[call.ID] = call
	}
	var projected []nativeResponseMessage
	presentation := message.NativeResponse.ReasoningPresentation()
	for _, item := range message.NativeResponse.PresentationItems() {
		if item.Type == llm.ResponseItemTypeReasoning {
			for _, summary := range item.ReasoningSummary {
				if summary.Text == "" {
					continue
				}
				entry := &ChatEntry{Role: string(transcript.EntryRoleReasoning), Visibility: transcript.EntryVisibilityDetail, Text: summary.Text}
				var identity *TranscriptReasoningTraceIdentity
				for _, fact := range presentation {
					if fact.OutputIndex != item.OutputIndex || summary.SourceCoordinate == nil || summary.SourceCoordinate.PartIndex == nil || fact.PartIndex != *summary.SourceCoordinate.PartIndex {
						continue
					}
					entry.DurationMs = fact.DurationMs
					if fact.KentTraceID != nil {
						identity = &TranscriptReasoningTraceIdentity{Kent: fact.KentTraceID}
					}
					break
				}
				if identity == nil && summary.ItemIdentity != nil {
					identity = &TranscriptReasoningTraceIdentity{Provider: summary.ItemIdentity}
				}
				projected = append(projected, nativeResponseMessage{OutputIndex: item.OutputIndex, Reasoning: entry, ReasoningIdentity: identity})
			}
			continue
		}
		if item.Type == llm.ResponseItemTypeMessage {
			phase := item.Phase
			if phase == nil {
				phase = message.Phase
			}
			projected = append(projected, nativeResponseMessage{OutputIndex: item.OutputIndex, Message: llm.Message{Role: llm.RoleAssistant, Content: item.Content, Phase: phase}})
			continue
		}
		id := item.CallID
		if id == nil {
			id = item.ID
		}
		if id != nil {
			if call, exists := calls[*id]; exists {
				projected = append(projected, nativeResponseMessage{OutputIndex: item.OutputIndex, Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{call}, Phase: textutil.Pointer(message.Phase)}})
			}
		}
	}
	return projected
}
