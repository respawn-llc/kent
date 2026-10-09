package session

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
)

type CompactionEngine string

const (
	CompactionEngineLocal  CompactionEngine = "local"
	CompactionEngineRemote CompactionEngine = "remote"
)

func normalizeCompactionEngine(engine CompactionEngine) (CompactionEngine, error) {
	engine = CompactionEngine(strings.TrimSpace(string(engine)))
	switch engine {
	case CompactionEngineLocal, CompactionEngineRemote:
		return engine, nil
	default:
		return "", fmt.Errorf("unsupported compaction engine %q", engine)
	}
}

// CompactedOutput retains the immutable result of compaction until an operation
// supplies its generation context. It is not yet a prepared provider history.
type CompactedOutput struct {
	Summary              []ProviderHistoryItem `json:"summary"`
	RunningShells        []MessageRecord       `json:"running_shells,omitempty"`
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
	record.RunningShells = append([]MessageRecord(nil), record.RunningShells...)
	for index, message := range record.RunningShells {
		if message.Role != MessageRoleDeveloper {
			return CompactedOutput{}, fmt.Errorf("running shell reminder %d must be a developer message", index)
		}
		record.RunningShells[index], err = normalizeMessageRecord(message)
		if err != nil {
			return CompactedOutput{}, fmt.Errorf("running shell reminder %d: %w", index, err)
		}
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

func encodeCompactedOutput(buffer *bytes.Buffer, record CompactedOutput) error {
	buffer.WriteByte('{')
	// The item encoder preserves opaque provider bytes, including whitespace.
	buffer.WriteString(`"summary":`)
	if err := encodeProviderHistoryItemArray(buffer, record.Summary); err != nil {
		return err
	}
	if len(record.RunningShells) > 0 {
		if err := writeMarshaledJSONField(buffer, "running_shells", record.RunningShells, true); err != nil {
			return err
		}
	}
	for _, write := range []func() error{
		func() error {
			return writeOptionalHistoryField(buffer, "preserved_user_message", record.PreservedUserMessage)
		},
		func() error {
			return writeOptionalHistoryField(buffer, "future_agent_message", record.FutureAgentMessage)
		},
	} {
		if err := write(); err != nil {
			return err
		}
	}
	buffer.WriteByte('}')
	return nil
}
