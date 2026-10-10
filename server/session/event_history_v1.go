package session

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"core/shared/rollbacktarget"
)

type ProviderHistoryItemType string

const (
	ProviderHistoryItemTypeMessage             ProviderHistoryItemType = "message"
	ProviderHistoryItemTypeFunctionCall        ProviderHistoryItemType = "function_call"
	ProviderHistoryItemTypeFunctionCallOutput  ProviderHistoryItemType = "function_call_output"
	ProviderHistoryItemTypeCustomToolCall      ProviderHistoryItemType = "custom_tool_call"
	ProviderHistoryItemTypeCustomToolOutput    ProviderHistoryItemType = "custom_tool_call_output"
	ProviderHistoryItemTypeReasoning           ProviderHistoryItemType = "reasoning"
	ProviderHistoryItemTypeCompaction          ProviderHistoryItemType = "compaction"
	ProviderHistoryItemTypeConfigurationUpdate ProviderHistoryItemType = "configuration_update"
	ProviderHistoryItemTypeOther               ProviderHistoryItemType = "other"
)

type ProviderHistoryReasoningEntry struct {
	Role *string `json:"role,omitempty"`
	Text string  `json:"text"`
}

type ProviderHistoryItem struct {
	// Slice position is the durable provider order. Parser output indexes are
	// assembly-only facts and are intentionally not persisted.
	Type                 ProviderHistoryItemType         `json:"type"`
	Role                 *MessageRole                    `json:"role,omitempty"`
	MessageType          *MessageType                    `json:"message_type,omitempty"`
	SourcePath           *string                         `json:"source_path,omitempty"`
	WorktreeContext      *WorktreeContext                `json:"worktree_context,omitempty"`
	Phase                *MessagePhase                   `json:"phase,omitempty"`
	ID                   *string                         `json:"id,omitempty"`
	Name                 *string                         `json:"name,omitempty"`
	CallID               *string                         `json:"call_id,omitempty"`
	Content              *string                         `json:"content,omitempty"`
	CompactContent       *string                         `json:"compact_content,omitempty"`
	BackgroundActivityID *string                         `json:"background_activity_id,omitempty"`
	BackgroundExitCode   *int                            `json:"background_exit_code,omitempty"`
	ToolPresentation     json.RawMessage                 `json:"tool_presentation,omitempty"`
	Arguments            json.RawMessage                 `json:"arguments,omitempty"`
	CustomInput          *string                         `json:"custom_input,omitempty"`
	Output               json.RawMessage                 `json:"output,omitempty"`
	ReasoningSummary     []ProviderHistoryReasoningEntry `json:"reasoning_summary,omitempty"`
	EncryptedContent     *string                         `json:"encrypted_content,omitempty"`
	ConfigurationEffort  *string                         `json:"configuration_effort,omitempty"`
	Raw                  json.RawMessage                 `json:"raw,omitempty"`
	LinkedCallID         *string                         `json:"linked_call_id,omitempty"`
	LinkKind             *ProviderItemLinkKind           `json:"link_kind,omitempty"`
}

type HistoryReplacementRecord struct {
	Engine                            string                           `json:"engine"`
	Mode                              CompactionMode                   `json:"mode"`
	CompactionNumber                  *int                             `json:"compaction_number,omitempty"`
	CommittedEntryStart               *int                             `json:"committed_entry_start,omitempty"`
	PendingHandoffFutureMessage       *string                          `json:"pending_handoff_future_message,omitempty"`
	LastCommittedAssistantFinalAnswer *string                          `json:"last_committed_assistant_final_answer,omitempty"`
	LatestRollbackCandidate           *rollbacktarget.CandidateLocator `json:"latest_rollback_candidate,omitempty"`
	Items                             []ProviderHistoryItem            `json:"items,omitempty"`
	// CompactedOutput awaits fresh operation context. Items represents an
	// already prepared historical working set.
	CompactedOutput *CompactedOutput `json:"compacted_output,omitempty"`
}

var ErrProviderHistoryItem = errors.New("invalid provider history item")

type ConfigurationUpdateRecord struct {
	Item ProviderHistoryItem `json:"item"`
}

func (ConfigurationUpdateRecord) eventKind() EventKind { return EventKindConfigurationUpdate }

func (r ConfigurationUpdateRecord) validate() error {
	if r.Item.Type != ProviderHistoryItemTypeConfigurationUpdate {
		return errors.New("configuration update requires a configuration history item")
	}
	_, err := normalizeProviderHistoryItem(0, r.Item)
	return err
}

type ProviderHistoryItemErrorReason string

const (
	ProviderHistoryItemUnsupportedType ProviderHistoryItemErrorReason = "unsupported_type"
	ProviderHistoryItemMissingRaw      ProviderHistoryItemErrorReason = "missing_raw"
	ProviderHistoryItemInvalidRaw      ProviderHistoryItemErrorReason = "invalid_raw"
	ProviderHistoryItemInvalidFacts    ProviderHistoryItemErrorReason = "invalid_facts"
)

type ProviderHistoryItemError struct {
	Index  int
	Type   ProviderHistoryItemType
	Reason ProviderHistoryItemErrorReason
}

func (e ProviderHistoryItemError) Error() string {
	return fmt.Sprintf(
		"provider history item %d is invalid (type=%q reason=%q)",
		e.Index,
		e.Type,
		e.Reason,
	)
}

func (e ProviderHistoryItemError) Unwrap() error {
	return ErrProviderHistoryItem
}

func normalizeHistoryReplacementRecord(record HistoryReplacementRecord) (HistoryReplacementRecord, error) {
	if err := validateCompactionEngine(CompactionEngine(record.Engine)); err != nil {
		return HistoryReplacementRecord{}, err
	}
	var err error
	switch record.Mode {
	case CompactionModeAuto, CompactionModeHandoff, CompactionModeManual, CompactionModeWorkflowPostCompletion:
	default:
		return HistoryReplacementRecord{}, fmt.Errorf("unsupported compaction mode %q", record.Mode)
	}

	if record.CompactionNumber != nil {
		if *record.CompactionNumber <= 0 {
			return HistoryReplacementRecord{}, fmt.Errorf(
				"compaction number must be positive when present: %d",
				*record.CompactionNumber,
			)
		}
		value := *record.CompactionNumber
		record.CompactionNumber = &value
	}
	if record.CommittedEntryStart != nil {
		if *record.CommittedEntryStart < 0 {
			return HistoryReplacementRecord{}, fmt.Errorf(
				"committed entry start must not be negative: %d",
				*record.CommittedEntryStart,
			)
		}
		value := *record.CommittedEntryStart
		record.CommittedEntryStart = &value
	}
	if record.PendingHandoffFutureMessage, err = normalizeOptionalEventText(
		"pending handoff future message",
		record.PendingHandoffFutureMessage,
	); err != nil {
		return HistoryReplacementRecord{}, err
	}
	if record.LastCommittedAssistantFinalAnswer, err = normalizeOptionalEventText(
		"last committed assistant final answer",
		record.LastCommittedAssistantFinalAnswer,
	); err != nil {
		return HistoryReplacementRecord{}, err
	}
	record.LatestRollbackCandidate, err = normalizeRollbackCandidate(record.LatestRollbackCandidate)
	if err != nil {
		return HistoryReplacementRecord{}, err
	}
	record.Items, err = normalizeProviderHistoryItems(record.Items)
	if err != nil {
		return HistoryReplacementRecord{}, err
	}
	if record.CompactedOutput != nil {
		if record.Items != nil {
			return HistoryReplacementRecord{}, errors.New("history replacement cannot contain both prepared history and compacted output")
		}
		if record.CompactionNumber == nil || record.CommittedEntryStart == nil {
			return HistoryReplacementRecord{}, errors.New("compacted output requires compaction number and committed entry start")
		}
		output, err := normalizeCompactedOutput(*record.CompactedOutput)
		if err != nil {
			return HistoryReplacementRecord{}, err
		}
		record.CompactedOutput = &output
	}
	return record, nil
}

func normalizeRollbackCandidate(candidate *rollbacktarget.CandidateLocator) (*rollbacktarget.CandidateLocator, error) {
	if candidate == nil {
		return nil, nil
	}
	value := *candidate
	if err := value.Validate(); err != nil {
		return nil, fmt.Errorf("latest rollback candidate: %w", err)
	}
	return &value, nil
}

func normalizeProviderHistoryItems(items []ProviderHistoryItem) ([]ProviderHistoryItem, error) {
	if len(items) > 0 {
		items = append([]ProviderHistoryItem(nil), items...)
		for index := range items {
			item, err := normalizeProviderHistoryItem(index, items[index])
			if err != nil {
				return nil, err
			}
			items[index] = item
		}
	}
	return items, nil
}

func normalizeProviderHistoryItem(
	index int,
	item ProviderHistoryItem,
) (ProviderHistoryItem, error) {
	switch item.Type {
	case ProviderHistoryItemTypeMessage,
		ProviderHistoryItemTypeFunctionCall,
		ProviderHistoryItemTypeFunctionCallOutput,
		ProviderHistoryItemTypeCustomToolCall,
		ProviderHistoryItemTypeCustomToolOutput,
		ProviderHistoryItemTypeReasoning,
		ProviderHistoryItemTypeCompaction,
		ProviderHistoryItemTypeConfigurationUpdate,
		ProviderHistoryItemTypeOther:
	default:
		return ProviderHistoryItem{}, providerHistoryItemError(
			index,
			item.Type,
			ProviderHistoryItemUnsupportedType,
		)
	}

	if item.Type == ProviderHistoryItemTypeConfigurationUpdate && item.ConfigurationEffort == nil {
		return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
	}
	if item.Type != ProviderHistoryItemTypeConfigurationUpdate && item.ConfigurationEffort != nil {
		return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
	}
	if item.Role != nil {
		if err := validateMessageRole(*item.Role); err != nil {
			return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
		}
		role := *item.Role
		item.Role = &role
	}
	var err error
	if item.MessageType, err = normalizeOptionalMessageType(item.MessageType); err != nil {
		return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
	}
	if item.Phase, err = normalizeOptionalMessagePhase(item.Phase); err != nil {
		return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
	}
	if item.WorktreeContext != nil {
		context, contextErr := normalizeWorktreeContext(*item.WorktreeContext)
		if contextErr != nil {
			return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
		}
		item.WorktreeContext = &context
	}
	if item.BackgroundExitCode != nil {
		exitCode := *item.BackgroundExitCode
		item.BackgroundExitCode = &exitCode
	}
	optionalTexts := []struct {
		name  string
		value **string
	}{
		{name: "source path", value: &item.SourcePath},
		{name: "item identity", value: &item.ID},
		{name: "item name", value: &item.Name},
		{name: "call identity", value: &item.CallID},
		{name: "content", value: &item.Content},
		{name: "compact content", value: &item.CompactContent},
		{name: "background activity identity", value: &item.BackgroundActivityID},
		{name: "custom input", value: &item.CustomInput},
		{name: "encrypted content", value: &item.EncryptedContent},
		{name: "configuration effort", value: &item.ConfigurationEffort},
		{name: "linked call identity", value: &item.LinkedCallID},
	}
	for _, optional := range optionalTexts {
		*optional.value, err = normalizeOptionalEventText(optional.name, *optional.value)
		if err != nil {
			return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
		}
	}
	jsonValues := []struct {
		name  string
		value *json.RawMessage
	}{
		{name: "tool presentation", value: &item.ToolPresentation},
		{name: "arguments", value: &item.Arguments},
		{name: "output", value: &item.Output},
	}
	for _, jsonValue := range jsonValues {
		if len(bytes.TrimSpace(*jsonValue.value)) == 0 {
			*jsonValue.value = nil
			continue
		}
		if err := validateJSONValue(jsonValue.name, *jsonValue.value); err != nil {
			return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
		}
		*jsonValue.value = append(json.RawMessage(nil), (*jsonValue.value)...)
	}
	if len(item.ReasoningSummary) > 0 {
		item.ReasoningSummary = append([]ProviderHistoryReasoningEntry(nil), item.ReasoningSummary...)
		for summaryIndex := range item.ReasoningSummary {
			entry := item.ReasoningSummary[summaryIndex]
			if entry.Role, err = normalizeOptionalEventText("reasoning summary role", entry.Role); err != nil {
				return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
			}
			if strings.TrimSpace(entry.Text) == "" {
				return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
			}
			item.ReasoningSummary[summaryIndex] = entry
		}
	}
	if item.LinkKind != nil {
		if item.Type != ProviderHistoryItemTypeOther ||
			*item.LinkKind != ProviderItemLinkToolOutputAttachment ||
			item.LinkedCallID == nil {
			return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
		}
		linkKind := *item.LinkKind
		item.LinkKind = &linkKind
	} else if item.LinkedCallID != nil {
		return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
	}
	if len(bytes.TrimSpace(item.Raw)) == 0 {
		switch item.Type {
		case ProviderHistoryItemTypeFunctionCallOutput, ProviderHistoryItemTypeCustomToolOutput:
			if item.CallID == nil || len(item.Output) == 0 {
				return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
			}
			raw, rawErr := encodeMissingProviderOutputRaw(
				ProviderInputItemType(item.Type),
				*item.CallID,
				item.Output,
			)
			if rawErr != nil {
				return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidFacts)
			}
			item.Raw = raw
		default:
			return ProviderHistoryItem{}, providerHistoryItemError(
				index,
				item.Type,
				ProviderHistoryItemMissingRaw,
			)
		}
	}
	if !json.Valid(item.Raw) {
		return ProviderHistoryItem{}, providerHistoryItemError(index, item.Type, ProviderHistoryItemInvalidRaw)
	}
	item.Raw = append(json.RawMessage(nil), item.Raw...)
	return item, nil
}

func providerHistoryItemError(
	index int,
	itemType ProviderHistoryItemType,
	reason ProviderHistoryItemErrorReason,
) error {
	return ProviderHistoryItemError{Index: index, Type: itemType, Reason: reason}
}
