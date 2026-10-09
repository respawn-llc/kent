package session

import (
	"bytes"
	"errors"
	"fmt"
	"strings"

	"core/shared/rollbacktarget"
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

// WorkflowCompactionRecord saves completed provider output before the next
// assignment establishes its role-specific context. Its sections are not yet
// a prepared provider history or transcript-visible replacement.
type WorkflowCompactionRecord struct {
	Engine                            CompactionEngine                 `json:"engine"`
	CompactionNumber                  int                              `json:"compaction_number"`
	CommittedEntryStart               int                              `json:"committed_entry_start"`
	Summary                           []ProviderHistoryItem            `json:"summary"`
	RunningShells                     []MessageRecord                  `json:"running_shells,omitempty"`
	PreservedUserMessage              *MessageRecord                   `json:"preserved_user_message,omitempty"`
	LastCommittedAssistantFinalAnswer *string                          `json:"last_committed_assistant_final_answer,omitempty"`
	LatestRollbackCandidate           *rollbacktarget.CandidateLocator `json:"latest_rollback_candidate,omitempty"`
}

func (WorkflowCompactionRecord) eventKind() EventKind { return EventKindWorkflowCompaction }

func (r WorkflowCompactionRecord) validate() error {
	_, err := normalizeWorkflowCompactionRecord(r)
	return err
}

func normalizeWorkflowCompactionRecord(record WorkflowCompactionRecord) (WorkflowCompactionRecord, error) {
	var err error
	record.Engine, err = normalizeCompactionEngine(record.Engine)
	if err != nil {
		return WorkflowCompactionRecord{}, err
	}
	if record.CompactionNumber <= 0 {
		return WorkflowCompactionRecord{}, errors.New("workflow compaction number must be positive")
	}
	if record.CommittedEntryStart < 0 {
		return WorkflowCompactionRecord{}, errors.New("committed entry start must not be negative")
	}
	if len(record.Summary) == 0 {
		return WorkflowCompactionRecord{}, errors.New("workflow compaction summary is required")
	}
	record.Summary, err = normalizeProviderHistoryItems(record.Summary)
	if err != nil {
		return WorkflowCompactionRecord{}, err
	}
	record.RunningShells = append([]MessageRecord(nil), record.RunningShells...)
	for index, message := range record.RunningShells {
		if message.Role != MessageRoleDeveloper {
			return WorkflowCompactionRecord{}, fmt.Errorf("running shell reminder %d must be a developer message", index)
		}
		record.RunningShells[index], err = normalizeMessageRecord(message)
		if err != nil {
			return WorkflowCompactionRecord{}, fmt.Errorf("running shell reminder %d: %w", index, err)
		}
	}
	if record.PreservedUserMessage != nil {
		if record.PreservedUserMessage.Role != MessageRoleDeveloper ||
			record.PreservedUserMessage.MessageType == nil ||
			*record.PreservedUserMessage.MessageType != MessageTypeCompactionPreservedUserMessage {
			return WorkflowCompactionRecord{}, errors.New("preserved user message requires a typed compaction carryover developer message")
		}
		message, err := normalizeMessageRecord(*record.PreservedUserMessage)
		if err != nil {
			return WorkflowCompactionRecord{}, fmt.Errorf("preserved user message: %w", err)
		}
		record.PreservedUserMessage = &message
	}
	record.LastCommittedAssistantFinalAnswer, err = normalizeOptionalEventText(
		"last committed assistant final answer", record.LastCommittedAssistantFinalAnswer)
	if err != nil {
		return WorkflowCompactionRecord{}, err
	}
	record.LatestRollbackCandidate, err = normalizeRollbackCandidate(record.LatestRollbackCandidate)
	if err != nil {
		return WorkflowCompactionRecord{}, err
	}
	return record, nil
}

func encodeWorkflowCompactionRecord(record WorkflowCompactionRecord) ([]byte, error) {
	var buffer bytes.Buffer
	buffer.WriteByte('{')
	if err := writeMarshaledJSONField(&buffer, "engine", record.Engine, false); err != nil {
		return nil, err
	}
	if err := writeMarshaledJSONField(&buffer, "compaction_number", record.CompactionNumber, true); err != nil {
		return nil, err
	}
	if err := writeMarshaledJSONField(&buffer, "committed_entry_start", record.CommittedEntryStart, true); err != nil {
		return nil, err
	}
	if err := encodeProviderHistoryItems(&buffer, "summary", record.Summary); err != nil {
		return nil, err
	}
	if len(record.RunningShells) > 0 {
		if err := writeMarshaledJSONField(&buffer, "running_shells", record.RunningShells, true); err != nil {
			return nil, err
		}
	}
	for _, write := range []func() error{
		func() error {
			return writeOptionalHistoryField(&buffer, "preserved_user_message", record.PreservedUserMessage)
		},
		func() error {
			return writeOptionalHistoryField(&buffer, "last_committed_assistant_final_answer", record.LastCommittedAssistantFinalAnswer)
		},
		func() error {
			return writeOptionalHistoryField(&buffer, "latest_rollback_candidate", record.LatestRollbackCandidate)
		},
	} {
		if err := write(); err != nil {
			return nil, err
		}
	}
	buffer.WriteByte('}')
	return buffer.Bytes(), nil
}
