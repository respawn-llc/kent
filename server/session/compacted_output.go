package session

import (
	"errors"
	"fmt"
)

type CompactionEngine string

const (
	CompactionEngineLocal  CompactionEngine = "local"
	CompactionEngineRemote CompactionEngine = "remote"
)

func validateCompactionEngine(engine CompactionEngine) error {
	switch engine {
	case CompactionEngineLocal, CompactionEngineRemote:
		return nil
	default:
		return fmt.Errorf("unsupported compaction engine %q", engine)
	}
}

// CompactedOutput retains the immutable result of compaction until an operation
// supplies its generation context. It is not yet a prepared provider history.
type CompactedOutput struct {
	Summary              []ProviderHistoryItem `json:"summary"`
	PreservedUserMessage *MessageRecord        `json:"preserved_user_message,omitempty"`
	FutureAgentMessage   *MessageRecord        `json:"future_agent_message,omitempty"`
}

func normalizeCompactedOutput(record CompactedOutput) (CompactedOutput, error) {
	var err error
	if len(record.Summary) == 0 {
		return CompactedOutput{}, errors.New("compacted output summary is required")
	}
	record.Summary, err = normalizeProviderHistoryItems(record.Summary)
	if err != nil {
		return CompactedOutput{}, err
	}
	if record.PreservedUserMessage != nil {
		if record.PreservedUserMessage.Role != MessageRoleDeveloper ||
			record.PreservedUserMessage.MessageType == nil ||
			*record.PreservedUserMessage.MessageType != MessageTypeCompactionPreservedUserMessage {
			return CompactedOutput{}, errors.New("preserved user message requires a typed compaction carryover developer message")
		}
		message, err := normalizeMessageRecord(*record.PreservedUserMessage)
		if err != nil {
			return CompactedOutput{}, fmt.Errorf("preserved user message: %w", err)
		}
		record.PreservedUserMessage = &message
	}
	if record.FutureAgentMessage != nil {
		message, err := normalizeMessageRecord(*record.FutureAgentMessage)
		if err != nil {
			return CompactedOutput{}, fmt.Errorf("future agent message: %w", err)
		}
		record.FutureAgentMessage = &message
	}
	return record, nil
}
