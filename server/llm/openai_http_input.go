package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"core/prompts"
	"core/server/llm/openaiwire"
	"core/shared/textutil"
	"core/shared/toolspec"
	"core/shared/transcript"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// ErrResponsesInputItemUnprepared reports that provider-neutral history reached
// the Responses serializer without a valid provider-ready Raw payload.
var ErrResponsesInputItemUnprepared = errors.New("responses input item is not prepared")

type ResponsesInputPreparationDetail string

const (
	ResponsesInputPreparationMissingRaw           ResponsesInputPreparationDetail = "missing_raw"
	ResponsesInputPreparationInvalidRaw           ResponsesInputPreparationDetail = "invalid_raw"
	ResponsesInputInvariantEmptyContent           ResponsesInputPreparationDetail = "empty_content"
	ResponsesInputInvariantEmptyCallID            ResponsesInputPreparationDetail = "empty_call_id"
	ResponsesInputInvariantEmptyArguments         ResponsesInputPreparationDetail = "empty_arguments"
	ResponsesInputInvariantInvalidOutputJSON      ResponsesInputPreparationDetail = "invalid_output_json"
	ResponsesInputInvariantEmptyReasoningID       ResponsesInputPreparationDetail = "empty_reasoning_id"
	ResponsesInputInvariantEmptyCompactionContent ResponsesInputPreparationDetail = "empty_compaction_content"
	ResponsesInputInvariantUnsupportedType        ResponsesInputPreparationDetail = "unsupported_type"
)

type ResponsesInputItemPreparationError struct {
	Index     int
	Type      ResponseItemType
	Name      *string
	CallID    *string
	State     ResponsesInputPreparationDetail
	Invariant ResponsesInputPreparationDetail
}

func (e *ResponsesInputItemPreparationError) Error() string {
	return fmt.Sprintf("responses input item at index %d is not prepared (type=%q name=%s call_id=%s state=%q invariant=%q)", e.Index, e.Type, formatOptionalResponsesInputFact(e.Name), formatOptionalResponsesInputFact(e.CallID), e.State, e.Invariant)
}

func (e *ResponsesInputItemPreparationError) Unwrap() error { return ErrResponsesInputItemUnprepared }

func buildResponsesInput(canonical []ResponseItem) ([]responses.ResponseInputItemUnionParam, error) {
	items := make([]responses.ResponseInputItemUnionParam, 0, len(canonical))
	for idx, item := range canonical {
		raw := bytes.TrimSpace(item.Raw)
		if len(raw) == 0 {
			return nil, newResponsesInputItemPreparationError(idx, item, ResponsesInputPreparationMissingRaw)
		}
		if !json.Valid(raw) {
			return nil, newResponsesInputItemPreparationError(idx, item, ResponsesInputPreparationInvalidRaw)
		}
		items = append(items, param.Override[responses.ResponseInputItemUnionParam](append(json.RawMessage(nil), raw...)))
	}
	return items, nil
}

func newResponsesInputItemPreparationError(index int, item ResponseItem, state ResponsesInputPreparationDetail) error {
	invariant := unpreparedResponsesInputInvariant(item)
	if state == ResponsesInputPreparationInvalidRaw {
		invariant = ResponsesInputPreparationInvalidRaw
	}
	return &ResponsesInputItemPreparationError{
		Index: index, Type: item.Type, Name: optionalTrimmedPointer(item.Name),
		CallID: optionalFirstTrimmedPointer(item.CallID, item.ID), State: state, Invariant: invariant,
	}
}

func formatOptionalResponsesInputFact(value *string) string {
	if value == nil {
		return "null"
	}
	return fmt.Sprintf("%q", *value)
}

func unpreparedResponsesInputInvariant(item ResponseItem) ResponsesInputPreparationDetail {
	switch item.Type {
	case ResponseItemTypeMessage:
		if _, present := textutil.OptionalTrimmed(item.Content); !present {
			return ResponsesInputInvariantEmptyContent
		}
	case ResponseItemTypeFunctionCall:
		if _, present := textutil.FirstOptionalTrimmed(item.CallID, item.ID); !present {
			return ResponsesInputInvariantEmptyCallID
		}
		if strings.TrimSpace(string(item.Arguments)) == "" {
			return ResponsesInputInvariantEmptyArguments
		}
	case ResponseItemTypeFunctionCallOutput:
		if _, present := textutil.OptionalTrimmed(item.CallID); !present {
			return ResponsesInputInvariantEmptyCallID
		}
		if !json.Valid(item.Output) {
			return ResponsesInputInvariantInvalidOutputJSON
		}
	case ResponseItemTypeCustomToolCall:
		if _, present := textutil.FirstOptionalTrimmed(item.CallID, item.ID); !present {
			return ResponsesInputInvariantEmptyCallID
		}
	case ResponseItemTypeCustomToolOutput:
		if _, present := textutil.OptionalTrimmed(item.CallID); !present {
			return ResponsesInputInvariantEmptyCallID
		}
		if !json.Valid(item.Output) {
			return ResponsesInputInvariantInvalidOutputJSON
		}
	case ResponseItemTypeReasoning:
		if _, present := textutil.OptionalTrimmed(item.ID); !present {
			return ResponsesInputInvariantEmptyReasoningID
		}
	case ResponseItemTypeCompaction:
		if _, present := textutil.OptionalTrimmed(item.EncryptedContent); !present {
			return ResponsesInputInvariantEmptyCompactionContent
		}
	default:
		return ResponsesInputInvariantUnsupportedType
	}
	return ResponsesInputPreparationMissingRaw
}

func normalizeToolArguments(arguments string) string {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return "{}"
	}
	if json.Valid([]byte(arguments)) {
		return textutil.CompactNoHTMLEscape([]byte(arguments))
	}
	quoted, _ := json.Marshal(arguments)
	return textutil.CompactNoHTMLEscape(quoted)
}

func normalizeToolInput(arguments string) json.RawMessage {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(arguments)) {
		return json.RawMessage(textutil.CompactNoHTMLEscape([]byte(arguments)))
	}
	quoted, _ := json.Marshal(arguments)
	return json.RawMessage(textutil.CompactNoHTMLEscape(quoted))
}

// PrepareResponsesInputItems stamps provider-ready Responses input payloads onto
// locally materialized response items. The transport can then pass Raw through
// without making history-shape decisions at request serialization time.
func PrepareResponsesInputItems(items []ResponseItem) []ResponseItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]ResponseItem, 0, len(items))
	for _, item := range items {
		out = append(out, prepareResponsesInputItem(item)...)
	}
	return out
}

func prepareResponsesInputItem(item ResponseItem) []ResponseItem {
	copyItem := CloneResponseItems([]ResponseItem{item})[0]
	if len(bytes.TrimSpace(copyItem.Raw)) > 0 {
		return []ResponseItem{copyItem}
	}
	if promoted, ok := promotedResponsesViewImageFileItems(copyItem); ok {
		return promoted
	}
	if raw, ok := responsesInputRawForResponseItem(copyItem); ok {
		copyItem.Raw = raw
	}
	return []ResponseItem{copyItem}
}

type responsesInputTextContentRaw struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesOutputTextContentRaw struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations,omitempty"`
}

type responsesInputMessageRaw struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content any    `json:"content"`
	Status  string `json:"status,omitempty"`
	Phase   string `json:"phase,omitempty"`
}

type responsesFunctionCallRaw struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type responsesCustomToolCallRaw struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Input  string `json:"input"`
}

type responsesReasoningSummaryRaw struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responsesReasoningRaw struct {
	Type             string                         `json:"type"`
	ID               string                         `json:"id"`
	Summary          []responsesReasoningSummaryRaw `json:"summary"`
	EncryptedContent string                         `json:"encrypted_content,omitempty"`
}

type responsesCompactionRaw struct {
	Type             string `json:"type"`
	ID               string `json:"id,omitempty"`
	EncryptedContent string `json:"encrypted_content"`
}

func responsesInputRawForResponseItem(item ResponseItem) (json.RawMessage, bool) {
	switch item.Type {
	case ResponseItemTypeConfigurationUpdate:
		if item.ConfigurationEffort == nil || *item.ConfigurationEffort == "" {
			return nil, false
		}
		return marshalResponsesInputRaw(responses.ResponseConfigurationUpdateItemParam{
			Reasoning: responses.ResponseConfigurationUpdateItemParamReasoning{
				Effort: shared.ReasoningEffort(*item.ConfigurationEffort),
			},
		})
	case ResponseItemTypeMessage:
		return responsesMessageInputRaw(item)
	case ResponseItemTypeFunctionCall:
		callID, present := textutil.FirstOptionalTrimmed(item.CallID, item.ID)
		arguments := strings.TrimSpace(string(item.Arguments))
		if !present || arguments == "" {
			return nil, false
		}
		name, _ := textutil.OptionalTrimmed(item.Name)
		return marshalResponsesInputRaw(responsesFunctionCallRaw{
			Type:      string(ResponseItemTypeFunctionCall),
			CallID:    callID,
			Name:      name,
			Arguments: arguments,
		})
	case ResponseItemTypeFunctionCallOutput:
		callID, present := textutil.OptionalTrimmed(item.CallID)
		if !present {
			return nil, false
		}
		raw, err := openaiwire.NewFunctionCallOutput(callID, item.Output)
		if err != nil {
			return nil, false
		}
		return raw.Bytes(), true
	case ResponseItemTypeCustomToolCall:
		callID, present := textutil.FirstOptionalTrimmed(item.CallID, item.ID)
		if !present {
			return nil, false
		}
		name, _ := textutil.OptionalTrimmed(item.Name)
		input, _ := textutil.OptionalExact(item.CustomInput)
		return marshalResponsesInputRaw(responsesCustomToolCallRaw{
			Type:   string(ResponseItemTypeCustomToolCall),
			CallID: callID,
			Name:   name,
			Input:  input,
		})
	case ResponseItemTypeCustomToolOutput:
		callID, present := textutil.OptionalTrimmed(item.CallID)
		if !present {
			return nil, false
		}
		raw, err := openaiwire.NewCustomToolOutput(callID, item.Output)
		if err != nil {
			return nil, false
		}
		return raw.Bytes(), true
	case ResponseItemTypeReasoning:
		id, present := textutil.OptionalTrimmed(item.ID)
		if !present {
			return nil, false
		}
		summary := make([]responsesReasoningSummaryRaw, 0, len(item.ReasoningSummary))
		for _, entry := range item.ReasoningSummary {
			text := strings.TrimSpace(entry.Text)
			if text == "" {
				continue
			}
			summary = append(summary, responsesReasoningSummaryRaw{Type: "summary_text", Text: text})
		}
		encrypted, _ := textutil.OptionalTrimmed(item.EncryptedContent)
		return marshalResponsesInputRaw(responsesReasoningRaw{
			Type:             string(ResponseItemTypeReasoning),
			ID:               id,
			Summary:          summary,
			EncryptedContent: encrypted,
		})
	case ResponseItemTypeCompaction:
		encrypted, present := textutil.OptionalTrimmed(item.EncryptedContent)
		if !present {
			return nil, false
		}
		id, _ := textutil.OptionalTrimmed(item.ID)
		return marshalResponsesInputRaw(responsesCompactionRaw{
			Type:             string(ResponseItemTypeCompaction),
			ID:               id,
			EncryptedContent: encrypted,
		})
	default:
		return nil, false
	}
}

func responsesMessageInputRaw(item ResponseItem) (json.RawMessage, bool) {
	text, present := textutil.OptionalExact(item.Content)
	if !present {
		return nil, false
	}
	if item.MessageType != nil && *item.MessageType == MessageTypeCompactionSummary {
		text = prompts.CompactionSummaryPrefix + "\n\n" + strings.TrimSpace(text)
	}
	role := ""
	if item.Role != nil {
		role = strings.TrimSpace(string(*item.Role))
	}
	blankFinal := transcript.IsBlankAssistantFinal(transcript.AssistantFinalCandidate{
		IsAssistant:    role == string(RoleAssistant),
		IsFinal:        item.Phase != nil && *item.Phase == MessagePhaseFinal,
		HasMessageType: item.MessageType != nil,
		Content:        textutil.Value(text),
	})
	if strings.TrimSpace(text) == "" && !blankFinal {
		return nil, false
	}
	if role == string(RoleAssistant) {
		content := []responsesOutputTextContentRaw{{
			Type:        "output_text",
			Text:        text,
			Annotations: []any{},
		}}
		raw := responsesInputMessageRaw{
			Type:    "message",
			Role:    string(RoleAssistant),
			Content: content,
			Status:  "completed",
		}
		if item.Phase != nil {
			raw.Phase = string(*item.Phase)
		}
		return marshalResponsesInputRaw(raw)
	}
	switch role {
	case string(RoleSystem), string(RoleDeveloper), string(RoleUser):
	default:
		role = string(RoleUser)
	}
	return marshalResponsesInputRaw(responsesInputMessageRaw{
		Type:    "message",
		Role:    role,
		Content: []responsesInputTextContentRaw{{Type: "input_text", Text: text}},
	})
}

func promotedResponsesViewImageFileItems(item ResponseItem) ([]ResponseItem, bool) {
	name, hasName := textutil.OptionalTrimmed(item.Name)
	if item.Type != ResponseItemTypeFunctionCallOutput ||
		!hasName ||
		name != string(toolspec.ToolViewImage) {
		return nil, false
	}
	callID, present := textutil.OptionalTrimmed(item.CallID)
	if !present {
		return nil, false
	}
	content, ok := openaiwire.InputContentItems(item.Output)
	if !ok {
		return nil, false
	}
	promotedRaw, promoted := promotedResponsesInputMessageRaw(content)
	if !promoted {
		return nil, false
	}
	output := CloneResponseItems([]ResponseItem{item})[0]
	promotedOutput, err := openaiwire.NewFunctionCallOutput(callID, json.RawMessage(`"attached file content"`))
	if err != nil {
		return nil, false
	}
	output.Raw = promotedOutput.Bytes()
	return []ResponseItem{
		output,
		{
			Type:         ResponseItemTypeOther,
			Name:         textutil.Value(string(toolspec.ToolViewImage)),
			CallID:       textutil.Value(callID),
			Raw:          promotedRaw,
			LinkedCallID: textutil.Value(callID),
			LinkKind:     textutil.Value(ResponseItemLinkToolOutputAttachment),
		},
	}, true
}

func optionalTrimmedPointer[T ~string](value *T) *string {
	trimmed, present := textutil.OptionalTrimmed(value)
	if !present {
		return nil
	}
	return &trimmed
}

func optionalFirstTrimmedPointer[T ~string](values ...*T) *string {
	trimmed, present := textutil.FirstOptionalTrimmed(values...)
	if !present {
		return nil
	}
	return &trimmed
}

func promotedResponsesInputMessageRaw(content []openaiwire.InputContent) (json.RawMessage, bool) {
	if len(content) == 0 {
		return nil, false
	}
	hasInputFile := false
	for _, item := range content {
		if item.Type == "input_file" {
			hasInputFile = true
			break
		}
	}
	if !hasInputFile {
		return nil, false
	}
	return marshalResponsesInputRaw(responsesInputMessageRaw{
		Type:    "message",
		Role:    string(RoleUser),
		Content: content,
	})
}

func marshalResponsesInputRaw(value any) (json.RawMessage, bool) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, false
	}
	return append(json.RawMessage(nil), bytes.TrimSpace(buf.Bytes())...), true
}
