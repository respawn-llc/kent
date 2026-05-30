package runtime

import (
	"encoding/json"
	"fmt"

	"builder/server/llm"
	"builder/shared/cachewarn"
	"builder/shared/config"
	"builder/shared/transcript"
)

type transcriptPersistenceCoordinator struct {
	state *transcriptRuntimeState
}

func newTranscriptPersistenceCoordinator(state *transcriptRuntimeState) transcriptPersistenceCoordinator {
	return transcriptPersistenceCoordinator{state: state}
}

func (p transcriptPersistenceCoordinator) AppendMessage(msg llm.Message) {
	if chat := p.chatProjection(); chat != nil {
		chat.appendMessage(msg)
	}
}

func (p transcriptPersistenceCoordinator) AppendLocalEntryRecord(entry ChatEntry) {
	if chat := p.chatProjection(); chat != nil {
		chat.appendLocalEntryRecord(entry)
	}
}

func (p transcriptPersistenceCoordinator) AppendLocalEntryWithOngoingText(role, text, ongoingText string) {
	if chat := p.chatProjection(); chat != nil {
		chat.appendLocalEntryWithOngoingText(role, text, ongoingText)
	}
}

func (p transcriptPersistenceCoordinator) AppendLocalEntryWithVisibility(role, text string, visibility transcript.EntryVisibility) {
	if chat := p.chatProjection(); chat != nil {
		chat.appendLocalEntryWithVisibility(role, text, visibility)
	}
}

func (p transcriptPersistenceCoordinator) AppendOngoingDelta(delta string) {
	if chat := p.chatProjection(); chat != nil {
		chat.appendOngoingDelta(delta)
	}
}

func (p transcriptPersistenceCoordinator) RecordStoredToolCompletion(completion storedToolCompletion) {
	if chat := p.chatProjection(); chat != nil {
		chat.recordStoredToolCompletion(completion)
	}
}

func (p transcriptPersistenceCoordinator) RestoreToolCompletionPayload(payload []byte) error {
	if chat := p.chatProjection(); chat != nil {
		return chat.restoreToolCompletionPayload(payload)
	}
	return nil
}

func (p transcriptPersistenceCoordinator) ReplaceHistory(items []llm.ResponseItem) {
	if chat := p.chatProjection(); chat != nil {
		chat.replaceHistory(items)
	}
}

func (p transcriptPersistenceCoordinator) ClearStreamingAssistantState() {
	if chat := p.chatProjection(); chat != nil {
		chat.clearOngoing()
		chat.clearOngoingError()
	}
}

func (p transcriptPersistenceCoordinator) SetOngoingError(text string) {
	if chat := p.chatProjection(); chat != nil {
		chat.setOngoingError(text)
	}
}

func (p transcriptPersistenceCoordinator) ClearOngoingError() {
	if chat := p.chatProjection(); chat != nil {
		chat.clearOngoingError()
	}
}

func (p transcriptPersistenceCoordinator) chatProjection() *chatStore {
	if p.state == nil {
		return nil
	}
	return p.state.chatProjection()
}

func applyPersistedCacheWarningToTranscript(persistence transcriptPersistenceCoordinator, payload []byte, mode config.CacheWarningMode) error {
	var warning cachewarn.Warning
	if err := json.Unmarshal(payload, &warning); err != nil {
		return fmt.Errorf("decode %s event: %w", sessionEventCacheWarning, err)
	}
	persistence.AppendLocalEntryWithVisibility(cacheWarningTranscriptRole, cachewarn.Text(warning), cacheWarningEntryVisibility(mode))
	return nil
}
