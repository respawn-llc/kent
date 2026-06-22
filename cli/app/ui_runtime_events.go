package app

import (
	"strings"

	"core/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

func waitRuntimeEvent(ch <-chan clientui.Event) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return nil
		}
		events := []clientui.Event{evt}
		if runtimeEventBatchFence(evt) {
			if runtimeEventCanDeferCommittedConversationFence(evt) {
				select {
				case next, ok := <-ch:
					if !ok {
						return runtimeEventBatchMsg{events: events}
					}
					if runtimeEventCoversDeferredCommittedConversationUpdate(evt, next) {
						return runtimeEventBatchMsg{events: []clientui.Event{next}}
					}
					if runtimeEventShouldBatchAfterCommittedConversationFence(evt, next) {
						return runtimeEventBatchMsg{events: []clientui.Event{evt, next}}
					}
					if runtimeEventBatchFence(next) {
						carry := next
						return runtimeEventBatchMsg{events: events, carry: &carry}
					}
					events = append(events, next)
				default:
					return runtimeEventBatchMsg{events: events}
				}
				for len(events) < 64 {
					select {
					case next, ok := <-ch:
						if !ok {
							return runtimeEventBatchMsg{events: events}
						}
						if runtimeEventBatchFence(next) {
							carry := next
							return runtimeEventBatchMsg{events: events, carry: &carry}
						}
						events = append(events, next)
					default:
						return runtimeEventBatchMsg{events: events}
					}
				}
				return runtimeEventBatchMsg{events: events}
			}
			return runtimeEventBatchMsg{events: events}
		}
		for len(events) < 64 {
			select {
			case next, ok := <-ch:
				if !ok {
					return runtimeEventBatchMsg{events: events}
				}
				if runtimeEventBatchFence(next) {
					carry := next
					return runtimeEventBatchMsg{events: events, carry: &carry}
				}
				events = append(events, next)
			default:
				return runtimeEventBatchMsg{events: events}
			}
		}
		return runtimeEventBatchMsg{events: events}
	}
}

func runtimeEventCanDeferCommittedConversationFence(evt clientui.Event) bool {
	return evt.Kind == clientui.EventConversationUpdated &&
		evt.CommittedTranscriptChanged &&
		evt.RecoveryCause == clientui.TranscriptRecoveryCauseNone &&
		len(evt.TranscriptEntries) == 0
}

func runtimeEventCoversDeferredCommittedConversationUpdate(update clientui.Event, next clientui.Event) bool {
	if !runtimeEventCanDeferCommittedConversationFence(update) {
		return false
	}
	if !next.CommittedTranscriptChanged || len(next.TranscriptEntries) == 0 {
		return false
	}
	if strings.TrimSpace(next.StepID) == "" || strings.TrimSpace(next.StepID) != strings.TrimSpace(update.StepID) {
		return false
	}
	if update.TranscriptRevision > 0 && next.TranscriptRevision > 0 && next.TranscriptRevision < update.TranscriptRevision {
		return false
	}
	if next.CommittedEntryCount != update.CommittedEntryCount {
		return false
	}
	return true
}

func runtimeEventShouldBatchAfterCommittedConversationFence(update clientui.Event, next clientui.Event) bool {
	if !runtimeEventCanDeferCommittedConversationFence(update) {
		return false
	}
	if !next.CommittedTranscriptChanged || len(next.TranscriptEntries) == 0 {
		return false
	}
	if strings.TrimSpace(next.StepID) == "" || strings.TrimSpace(next.StepID) != strings.TrimSpace(update.StepID) {
		return false
	}
	if update.TranscriptRevision > 0 && next.TranscriptRevision > 0 && next.TranscriptRevision < update.TranscriptRevision {
		return false
	}
	if runtimeEventCoversDeferredCommittedConversationUpdate(update, next) {
		return false
	}
	return true
}

func runtimeEventBatchFence(evt clientui.Event) bool {
	if len(evt.TranscriptEntries) > 0 {
		return true
	}
	switch evt.Kind {
	case clientui.EventStreamGap,
		clientui.EventConversationUpdated,
		clientui.EventAssistantDelta,
		clientui.EventReasoningDelta,
		clientui.EventStreamingErrorUpdated,
		clientui.EventAssistantDeltaReset,
		clientui.EventReasoningDeltaReset:
		return true
	default:
		return false
	}
}
