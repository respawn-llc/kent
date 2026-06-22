package runtime

import (
	"encoding/json"
	"fmt"
	"strings"

	"core/server/llm"
	"core/server/session"
	"core/shared/config"
)

// TranscriptProjector reconstructs transcript-visible state from persisted events
// without needing a full runtime instance.
type TranscriptProjector struct {
	chat             *chatStore
	cacheWarningMode config.CacheWarningMode
}

func NewTranscriptProjector() *TranscriptProjector {
	return NewTranscriptProjectorWithCacheWarningMode(config.CacheWarningModeDefault)
}

func NewTranscriptProjectorWithCacheWarningMode(mode config.CacheWarningMode) *TranscriptProjector {
	if normalized, ok := normalizeCacheWarningMode(mode); ok {
		mode = normalized
	} else {
		mode = config.CacheWarningModeDefault
	}
	return &TranscriptProjector{chat: newChatStore(), cacheWarningMode: mode}
}

func (p *TranscriptProjector) ApplyPersistedEvent(evt session.Event) error {
	if p == nil || p.chat == nil {
		return nil
	}
	switch strings.TrimSpace(evt.Kind) {
	case "message":
		var msg llm.Message
		if err := json.Unmarshal(evt.Payload, &msg); err != nil {
			return fmt.Errorf("decode message event: %w", err)
		}
		p.chat.appendMessage(msg)
	case "tool_completed":
		if err := p.chat.restoreToolCompletionPayload(evt.Payload); err != nil {
			return err
		}
	case "local_entry":
		var entry storedLocalEntry
		if err := json.Unmarshal(evt.Payload, &entry); err != nil {
			return fmt.Errorf("decode local_entry event: %w", err)
		}
		p.chat.appendLocalEntryRecord(*localEntryChatEntry(entry))
	case sessionEventCacheWarning:
		if err := applyPersistedCacheWarningToChat(p.chat, evt.Payload, p.cacheWarningMode); err != nil {
			return err
		}
	case "history_replaced":
		payload, ignoredLegacy, err := decodePersistedHistoryReplacementPayload(evt.Payload)
		if err != nil {
			return fmt.Errorf("%w: %w", errDecodeHistoryReplacedEvent, err)
		}
		if ignoredLegacy {
			return nil
		}
		p.chat.replaceHistory(payload.Items)
	}
	return nil
}

func (p *TranscriptProjector) ChatSnapshot() ChatSnapshot {
	if p == nil || p.chat == nil {
		return ChatSnapshot{}
	}
	return p.chat.snapshotWithMetadata().Snapshot
}

func (p *TranscriptProjector) TranscriptPageSnapshot(offset, limit int) transcriptPageSnapshot {
	if p == nil || p.chat == nil {
		return transcriptPageSnapshot{}
	}
	return p.chat.transcriptPageSnapshot(offset, limit)
}

func (p *TranscriptProjector) CommittedEntryCount() int {
	if p == nil || p.chat == nil {
		return 0
	}
	return p.chat.committedEntryCount()
}

func (p *TranscriptProjector) RecentTailSnapshot(maxEntries int) TranscriptWindowSnapshot {
	if p == nil || p.chat == nil {
		return TranscriptWindowSnapshot{}
	}
	return p.chat.ongoingTailSnapshot(maxEntries)
}

func (p *TranscriptProjector) LastCommittedAssistantFinalAnswer() string {
	if p == nil || p.chat == nil {
		return ""
	}
	return p.chat.cachedLastCommittedAssistantFinalAnswer()
}
