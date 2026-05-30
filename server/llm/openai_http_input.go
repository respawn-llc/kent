package llm

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"builder/prompts"
	"builder/shared/jsonutil"
	"builder/shared/textutil"
	"builder/shared/toolspec"

	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
)

func buildResponsesInput(canonical []ResponseItem) ([]responses.ResponseInputItemUnionParam, error) {
	items := make([]responses.ResponseInputItemUnionParam, 0, len(canonical))
	for idx, item := range canonical {
		if raw := bytes.TrimSpace(item.Raw); len(raw) > 0 {
			if !json.Valid(raw) {
				return nil, fmt.Errorf("invalid raw openai input item at index %d", idx)
			}
			items = append(items, param.Override[responses.ResponseInputItemUnionParam](append(json.RawMessage(nil), raw...)))
			continue
		}
		switch item.Type {
		case ResponseItemTypeMessage:
			if strings.TrimSpace(item.Content) == "" {
				return nil, fmt.Errorf("message input item at index %d has empty content", idx)
			}
			items = append(items, messageInput(item, item.Content))
		case ResponseItemTypeFunctionCall:
			callID := textutil.FirstNonEmpty(strings.TrimSpace(item.CallID), strings.TrimSpace(item.ID))
			if callID == "" {
				return nil, fmt.Errorf("function_call input item at index %d has empty call_id", idx)
			}
			arguments := strings.TrimSpace(string(item.Arguments))
			if arguments == "" {
				return nil, fmt.Errorf("function_call input item at index %d has empty arguments", idx)
			}
			items = append(items, responses.ResponseInputItemParamOfFunctionCall(arguments, callID, strings.TrimSpace(item.Name)))
		case ResponseItemTypeFunctionCallOutput:
			callID := strings.TrimSpace(item.CallID)
			if callID == "" {
				return nil, fmt.Errorf("function_call_output input item at index %d has empty call_id", idx)
			}
			converted, err := functionCallOutputInputItemFromPreparedOutput(callID, item.Name, item.Output)
			if err != nil {
				return nil, fmt.Errorf("function_call_output input item at index %d: %w", idx, err)
			}
			items = append(items, converted)
		case ResponseItemTypeCustomToolCall:
			callID := textutil.FirstNonEmpty(strings.TrimSpace(item.CallID), strings.TrimSpace(item.ID))
			if callID == "" {
				return nil, fmt.Errorf("custom_tool_call input item at index %d has empty call_id", idx)
			}
			items = append(items, responses.ResponseInputItemParamOfCustomToolCall(callID, item.CustomInput, strings.TrimSpace(item.Name)))
		case ResponseItemTypeCustomToolOutput:
			callID := strings.TrimSpace(item.CallID)
			if callID == "" {
				return nil, fmt.Errorf("custom_tool_call_output input item at index %d has empty call_id", idx)
			}
			output, err := providerOutputStringFromRaw(item.Output)
			if err != nil {
				return nil, fmt.Errorf("custom_tool_call_output input item at index %d: %w", idx, err)
			}
			items = append(items, responses.ResponseInputItemParamOfCustomToolCallOutput(callID, output))
		case ResponseItemTypeReasoning:
			id := strings.TrimSpace(item.ID)
			if id == "" {
				return nil, fmt.Errorf("reasoning input item at index %d has empty id", idx)
			}
			reasoningParam := responses.ResponseReasoningItemParam{
				ID:      id,
				Summary: []responses.ResponseReasoningItemSummaryParam{},
			}
			for _, summary := range item.ReasoningSummary {
				text := strings.TrimSpace(summary.Text)
				if text == "" {
					continue
				}
				reasoningParam.Summary = append(reasoningParam.Summary, responses.ResponseReasoningItemSummaryParam{
					Text: text,
					Type: "summary_text",
				})
			}
			if encrypted := strings.TrimSpace(item.EncryptedContent); encrypted != "" {
				reasoningParam.EncryptedContent = param.NewOpt(encrypted)
			}
			items = append(items, responses.ResponseInputItemUnionParam{OfReasoning: &reasoningParam})
		case ResponseItemTypeCompaction:
			encrypted := strings.TrimSpace(item.EncryptedContent)
			if encrypted == "" {
				return nil, fmt.Errorf("compaction input item at index %d has empty encrypted_content", idx)
			}
			compactionParam := responses.ResponseCompactionItemParam{EncryptedContent: encrypted}
			if id := strings.TrimSpace(item.ID); id != "" {
				compactionParam.ID = param.NewOpt(id)
			}
			items = append(items, responses.ResponseInputItemUnionParam{OfCompaction: &compactionParam})
		default:
			if len(item.Raw) == 0 || !json.Valid(item.Raw) {
				return nil, fmt.Errorf("unsupported input item at index %d has no valid raw provider payload", idx)
			}
			items = append(items, param.Override[responses.ResponseInputItemUnionParam](item.Raw))
		}
	}
	return items, nil
}

func messageInput(item ResponseItem, text string) responses.ResponseInputItemUnionParam {
	role := strings.TrimSpace(string(item.Role))
	role = strings.TrimSpace(role)
	if role == string(RoleAssistant) {
		content := []responses.ResponseOutputMessageContentUnionParam{{
			OfOutputText: &responses.ResponseOutputTextParam{
				Annotations: []responses.ResponseOutputTextAnnotationUnionParam{},
				Text:        text,
			},
		}}
		messageItem := responses.ResponseInputItemParamOfOutputMessage(content, "", responses.ResponseOutputMessageStatusCompleted)
		if messageItem.OfOutputMessage != nil && item.Phase != "" {
			messageItem.OfOutputMessage.Phase = responses.ResponseOutputMessagePhase(item.Phase)
		}
		return messageItem
	}

	inputRole := string(RoleUser)
	switch role {
	case string(RoleSystem), string(RoleDeveloper), string(RoleUser):
		inputRole = role
	}
	content := responses.ResponseInputMessageContentListParam{responses.ResponseInputContentParamOfInputText(text)}
	return responses.ResponseInputItemParamOfInputMessage(content, inputRole)
}

func normalizeToolArguments(arguments string) string {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return "{}"
	}
	if json.Valid([]byte(arguments)) {
		return jsonutil.CompactNoHTMLEscape([]byte(arguments))
	}
	quoted, _ := json.Marshal(arguments)
	return jsonutil.CompactNoHTMLEscape(quoted)
}

func normalizeToolInput(arguments string) json.RawMessage {
	arguments = strings.TrimSpace(arguments)
	if arguments == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(arguments)) {
		return json.RawMessage(jsonutil.CompactNoHTMLEscape([]byte(arguments)))
	}
	quoted, _ := json.Marshal(arguments)
	return json.RawMessage(jsonutil.CompactNoHTMLEscape(quoted))
}

// PrepareOpenAIInputItems stamps provider-ready OpenAI input payloads onto
// locally materialized response items. The transport can then pass Raw through
// without making history-shape decisions at request serialization time.
func PrepareOpenAIInputItems(items []ResponseItem) []ResponseItem {
	if len(items) == 0 {
		return nil
	}
	out := make([]ResponseItem, 0, len(items))
	for _, item := range items {
		out = append(out, prepareOpenAIInputItem(item)...)
	}
	return out
}

func prepareOpenAIInputItem(item ResponseItem) []ResponseItem {
	copyItem := CloneResponseItems([]ResponseItem{item})[0]
	if len(bytes.TrimSpace(copyItem.Raw)) > 0 {
		return []ResponseItem{copyItem}
	}
	if promoted, ok := promotedOpenAIViewImageFileItems(copyItem); ok {
		return promoted
	}
	if raw, ok := openAIInputRawForResponseItem(copyItem); ok {
		copyItem.Raw = raw
	}
	return []ResponseItem{copyItem}
}

type openAIInputTextContentRaw struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type openAIOutputTextContentRaw struct {
	Type        string `json:"type"`
	Text        string `json:"text"`
	Annotations []any  `json:"annotations,omitempty"`
}

type openAIInputContentRaw struct {
	Type     string `json:"type"`
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
	Detail   string `json:"detail,omitempty"`
	FileID   string `json:"file_id,omitempty"`
	FileData string `json:"file_data,omitempty"`
	FileURL  string `json:"file_url,omitempty"`
	Filename string `json:"filename,omitempty"`
}

type openAIInputMessageRaw struct {
	Type    string `json:"type"`
	Role    string `json:"role"`
	Content any    `json:"content"`
	Status  string `json:"status,omitempty"`
	Phase   string `json:"phase,omitempty"`
}

type openAIFunctionCallRaw struct {
	Type      string `json:"type"`
	CallID    string `json:"call_id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type openAIFunctionCallOutputRaw struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output any    `json:"output"`
}

type openAICustomToolCallRaw struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Name   string `json:"name"`
	Input  string `json:"input"`
}

type openAICustomToolCallOutputRaw struct {
	Type   string `json:"type"`
	CallID string `json:"call_id"`
	Output string `json:"output"`
}

type openAIReasoningSummaryRaw struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type openAIReasoningRaw struct {
	Type             string                      `json:"type"`
	ID               string                      `json:"id"`
	Summary          []openAIReasoningSummaryRaw `json:"summary"`
	EncryptedContent string                      `json:"encrypted_content,omitempty"`
}

type openAICompactionRaw struct {
	Type             string `json:"type"`
	ID               string `json:"id,omitempty"`
	EncryptedContent string `json:"encrypted_content"`
}

func openAIInputRawForResponseItem(item ResponseItem) (json.RawMessage, bool) {
	switch item.Type {
	case ResponseItemTypeMessage:
		return openAIMessageInputRaw(item)
	case ResponseItemTypeFunctionCall:
		callID := textutil.FirstNonEmpty(strings.TrimSpace(item.CallID), strings.TrimSpace(item.ID))
		arguments := strings.TrimSpace(string(item.Arguments))
		if callID == "" || arguments == "" {
			return nil, false
		}
		return marshalOpenAIInputRaw(openAIFunctionCallRaw{
			Type:      string(ResponseItemTypeFunctionCall),
			CallID:    callID,
			Name:      strings.TrimSpace(item.Name),
			Arguments: arguments,
		})
	case ResponseItemTypeFunctionCallOutput:
		callID := strings.TrimSpace(item.CallID)
		if callID == "" {
			return nil, false
		}
		output, ok := openAIFunctionOutputValueFromRaw(item.Output)
		if !ok {
			return nil, false
		}
		return marshalOpenAIInputRaw(openAIFunctionCallOutputRaw{
			Type:   string(ResponseItemTypeFunctionCallOutput),
			CallID: callID,
			Output: output,
		})
	case ResponseItemTypeCustomToolCall:
		callID := textutil.FirstNonEmpty(strings.TrimSpace(item.CallID), strings.TrimSpace(item.ID))
		if callID == "" {
			return nil, false
		}
		return marshalOpenAIInputRaw(openAICustomToolCallRaw{
			Type:   string(ResponseItemTypeCustomToolCall),
			CallID: callID,
			Name:   strings.TrimSpace(item.Name),
			Input:  item.CustomInput,
		})
	case ResponseItemTypeCustomToolOutput:
		callID := strings.TrimSpace(item.CallID)
		if callID == "" {
			return nil, false
		}
		output, err := providerOutputStringFromRaw(item.Output)
		if err != nil {
			return nil, false
		}
		return marshalOpenAIInputRaw(openAICustomToolCallOutputRaw{
			Type:   string(ResponseItemTypeCustomToolOutput),
			CallID: callID,
			Output: output,
		})
	case ResponseItemTypeReasoning:
		id := strings.TrimSpace(item.ID)
		if id == "" {
			return nil, false
		}
		summary := make([]openAIReasoningSummaryRaw, 0, len(item.ReasoningSummary))
		for _, entry := range item.ReasoningSummary {
			text := strings.TrimSpace(entry.Text)
			if text == "" {
				continue
			}
			summary = append(summary, openAIReasoningSummaryRaw{Type: "summary_text", Text: text})
		}
		return marshalOpenAIInputRaw(openAIReasoningRaw{
			Type:             string(ResponseItemTypeReasoning),
			ID:               id,
			Summary:          summary,
			EncryptedContent: strings.TrimSpace(item.EncryptedContent),
		})
	case ResponseItemTypeCompaction:
		encrypted := strings.TrimSpace(item.EncryptedContent)
		if encrypted == "" {
			return nil, false
		}
		return marshalOpenAIInputRaw(openAICompactionRaw{
			Type:             string(ResponseItemTypeCompaction),
			ID:               strings.TrimSpace(item.ID),
			EncryptedContent: encrypted,
		})
	default:
		return nil, false
	}
}

func openAIMessageInputRaw(item ResponseItem) (json.RawMessage, bool) {
	text := item.Content
	if item.MessageType == MessageTypeCompactionSummary {
		text = prompts.CompactionSummaryPrefix + "\n\n" + strings.TrimSpace(text)
	}
	if strings.TrimSpace(text) == "" {
		return nil, false
	}
	role := strings.TrimSpace(string(item.Role))
	if role == string(RoleAssistant) {
		content := []openAIOutputTextContentRaw{{
			Type:        "output_text",
			Text:        text,
			Annotations: []any{},
		}}
		raw := openAIInputMessageRaw{
			Type:    "message",
			Role:    string(RoleAssistant),
			Content: content,
			Status:  "completed",
		}
		if item.Phase != "" {
			raw.Phase = string(item.Phase)
		}
		return marshalOpenAIInputRaw(raw)
	}
	switch role {
	case string(RoleSystem), string(RoleDeveloper), string(RoleUser):
	default:
		role = string(RoleUser)
	}
	return marshalOpenAIInputRaw(openAIInputMessageRaw{
		Type:    "message",
		Role:    role,
		Content: []openAIInputTextContentRaw{{Type: "input_text", Text: text}},
	})
}

func promotedOpenAIViewImageFileItems(item ResponseItem) ([]ResponseItem, bool) {
	if item.Type != ResponseItemTypeFunctionCallOutput || strings.TrimSpace(item.Name) != string(toolspec.ToolViewImage) {
		return nil, false
	}
	callID := strings.TrimSpace(item.CallID)
	if callID == "" {
		return nil, false
	}
	content, ok := openAIInputContentItemsFromRaw(item.Output)
	if !ok {
		return nil, false
	}
	promotedRaw, promoted := promotedOpenAIInputMessageRaw(content)
	if !promoted {
		return nil, false
	}
	output := CloneResponseItems([]ResponseItem{item})[0]
	output.Raw, _ = marshalOpenAIInputRaw(openAIFunctionCallOutputRaw{
		Type:   string(ResponseItemTypeFunctionCallOutput),
		CallID: callID,
		Output: "attached file content",
	})
	return []ResponseItem{
		output,
		{
			Type:         ResponseItemTypeOther,
			Name:         string(toolspec.ToolViewImage),
			CallID:       callID,
			Raw:          promotedRaw,
			LinkedCallID: callID,
			LinkKind:     ResponseItemLinkToolOutputAttachment,
		},
	}, true
}

func promotedOpenAIInputMessageRaw(content []openAIInputContentRaw) (json.RawMessage, bool) {
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
	return marshalOpenAIInputRaw(openAIInputMessageRaw{
		Type:    "message",
		Role:    string(RoleUser),
		Content: content,
	})
}

func openAIFunctionOutputValueFromRaw(raw json.RawMessage) (any, bool) {
	if content, ok := openAIInputContentItemsFromRaw(raw); ok {
		return content, true
	}
	output, err := providerOutputStringFromRaw(raw)
	if err != nil {
		return nil, false
	}
	return output, true
}

func openAIInputContentItemsFromRaw(raw json.RawMessage) ([]openAIInputContentRaw, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || !strings.HasPrefix(trimmed, "[") {
		return nil, false
	}
	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil || len(arr) == 0 {
		return nil, false
	}
	out := make([]openAIInputContentRaw, 0, len(arr))
	for _, rawItem := range arr {
		item, ok := openAIInputContentItemFromRaw(rawItem)
		if !ok {
			return nil, false
		}
		out = append(out, item)
	}
	return out, true
}

func openAIInputContentItemFromRaw(raw json.RawMessage) (openAIInputContentRaw, bool) {
	var item struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL string `json:"image_url"`
		Detail   string `json:"detail"`
		FileID   string `json:"file_id"`
		FileData string `json:"file_data"`
		FileURL  string `json:"file_url"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return openAIInputContentRaw{}, false
	}
	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "input_text":
		return openAIInputContentRaw{Type: "input_text", Text: item.Text}, true
	case "input_image":
		image := openAIInputContentRaw{Type: "input_image"}
		if v := strings.TrimSpace(item.ImageURL); v != "" {
			image.ImageURL = v
		}
		if v := strings.TrimSpace(item.FileID); v != "" {
			image.FileID = v
		}
		switch strings.ToLower(strings.TrimSpace(item.Detail)) {
		case "low", "high", "auto":
			image.Detail = strings.ToLower(strings.TrimSpace(item.Detail))
		}
		if image.ImageURL == "" && image.FileID == "" {
			return openAIInputContentRaw{}, false
		}
		return image, true
	case "input_file":
		file := openAIInputContentRaw{Type: "input_file"}
		if v := strings.TrimSpace(item.FileData); v != "" {
			file.FileData = v
		}
		if v := strings.TrimSpace(item.FileURL); v != "" {
			file.FileURL = v
		}
		if v := strings.TrimSpace(item.FileID); v != "" {
			file.FileID = v
		}
		if v := strings.TrimSpace(item.Filename); v != "" {
			file.Filename = v
		}
		if file.FileData == "" && file.FileURL == "" && file.FileID == "" {
			return openAIInputContentRaw{}, false
		}
		return file, true
	default:
		return openAIInputContentRaw{}, false
	}
}

func marshalOpenAIInputRaw(value any) (json.RawMessage, bool) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, false
	}
	return append(json.RawMessage(nil), bytes.TrimSpace(buf.Bytes())...), true
}

func providerOutputStringFromRaw(raw json.RawMessage) (string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "", nil
	}
	if !json.Valid(raw) {
		return "", fmt.Errorf("output is invalid json")
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text, nil
	}
	return trimmed, nil
}

func functionCallOutputInputItemFromPreparedOutput(callID string, toolName string, raw json.RawMessage) (responses.ResponseInputItemUnionParam, error) {
	if contentItems, ok := functionCallOutputContentItemsFromRaw(raw); ok {
		if strings.TrimSpace(toolName) == string(toolspec.ToolViewImage) {
			if _, promoted := promoteFunctionOutputFilesToInputMessage(contentItems); promoted {
				return responses.ResponseInputItemUnionParam{}, fmt.Errorf("view_image input_file outputs must be materialized as provider raw input items before request serialization")
			}
		}
		return responses.ResponseInputItemParamOfFunctionCallOutput(callID, contentItems), nil
	}
	output, err := providerOutputStringFromRaw(raw)
	if err != nil {
		return responses.ResponseInputItemUnionParam{}, err
	}
	return responses.ResponseInputItemParamOfFunctionCallOutput(callID, output), nil
}

func promoteFunctionOutputFilesToInputMessage(contentItems responses.ResponseFunctionCallOutputItemListParam) (responses.ResponseInputMessageContentListParam, bool) {
	out := make(responses.ResponseInputMessageContentListParam, 0, len(contentItems))
	hasInputFile := false

	for _, item := range contentItems {
		switch {
		case item.OfInputText != nil:
			out = append(out, responses.ResponseInputContentParamOfInputText(item.OfInputText.Text))
		case item.OfInputImage != nil:
			image := responses.ResponseInputImageParam{}
			detail := responses.ResponseInputImageDetailAuto
			switch item.OfInputImage.Detail {
			case responses.ResponseInputImageContentDetailLow:
				detail = responses.ResponseInputImageDetailLow
			case responses.ResponseInputImageContentDetailHigh:
				detail = responses.ResponseInputImageDetailHigh
			case responses.ResponseInputImageContentDetailAuto:
				detail = responses.ResponseInputImageDetailAuto
			}
			image.Detail = detail
			if item.OfInputImage.ImageURL.Valid() {
				image.ImageURL = item.OfInputImage.ImageURL
			}
			if item.OfInputImage.FileID.Valid() {
				image.FileID = item.OfInputImage.FileID
			}
			out = append(out, responses.ResponseInputContentUnionParam{OfInputImage: &image})
		case item.OfInputFile != nil:
			hasInputFile = true
			file := responses.ResponseInputFileParam{}
			if item.OfInputFile.FileData.Valid() {
				file.FileData = item.OfInputFile.FileData
			}
			if item.OfInputFile.FileID.Valid() {
				file.FileID = item.OfInputFile.FileID
			}
			if item.OfInputFile.FileURL.Valid() {
				file.FileURL = item.OfInputFile.FileURL
			}
			if item.OfInputFile.Filename.Valid() {
				file.Filename = item.OfInputFile.Filename
			}
			out = append(out, responses.ResponseInputContentUnionParam{OfInputFile: &file})
		}
	}

	if !hasInputFile || len(out) == 0 {
		return nil, false
	}
	return out, true
}

func functionCallOutputContentItemsFromRaw(raw json.RawMessage) (responses.ResponseFunctionCallOutputItemListParam, bool) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || !strings.HasPrefix(trimmed, "[") {
		return nil, false
	}

	var arr []json.RawMessage
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, false
	}
	if len(arr) == 0 {
		return nil, false
	}

	out := make(responses.ResponseFunctionCallOutputItemListParam, 0, len(arr))
	for _, rawItem := range arr {
		item, ok := functionCallOutputContentItemFromRaw(rawItem)
		if !ok {
			return nil, false
		}
		out = append(out, item)
	}
	return out, true
}

func functionCallOutputContentItemFromRaw(raw json.RawMessage) (responses.ResponseFunctionCallOutputItemUnionParam, bool) {
	var item struct {
		Type     string `json:"type"`
		Text     string `json:"text"`
		ImageURL string `json:"image_url"`
		Detail   string `json:"detail"`
		FileID   string `json:"file_id"`
		FileData string `json:"file_data"`
		FileURL  string `json:"file_url"`
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal(raw, &item); err != nil {
		return responses.ResponseFunctionCallOutputItemUnionParam{}, false
	}

	switch strings.ToLower(strings.TrimSpace(item.Type)) {
	case "input_text":
		return responses.ResponseFunctionCallOutputItemUnionParam{
			OfInputText: &responses.ResponseInputTextContentParam{Text: item.Text},
		}, true
	case "input_image":
		image := responses.ResponseInputImageContentParam{}
		if v := strings.TrimSpace(item.ImageURL); v != "" {
			image.ImageURL = param.NewOpt(v)
		}
		if v := strings.TrimSpace(item.FileID); v != "" {
			image.FileID = param.NewOpt(v)
		}
		switch strings.ToLower(strings.TrimSpace(item.Detail)) {
		case "low":
			image.Detail = responses.ResponseInputImageContentDetailLow
		case "high":
			image.Detail = responses.ResponseInputImageContentDetailHigh
		case "auto":
			image.Detail = responses.ResponseInputImageContentDetailAuto
		}
		if !image.ImageURL.Valid() && !image.FileID.Valid() {
			return responses.ResponseFunctionCallOutputItemUnionParam{}, false
		}
		return responses.ResponseFunctionCallOutputItemUnionParam{OfInputImage: &image}, true
	case "input_file":
		file := responses.ResponseInputFileContentParam{}
		if v := strings.TrimSpace(item.FileData); v != "" {
			file.FileData = param.NewOpt(v)
		}
		if v := strings.TrimSpace(item.FileURL); v != "" {
			file.FileURL = param.NewOpt(v)
		}
		if v := strings.TrimSpace(item.FileID); v != "" {
			file.FileID = param.NewOpt(v)
		}
		if v := strings.TrimSpace(item.Filename); v != "" {
			file.Filename = param.NewOpt(v)
		}
		if !file.FileData.Valid() && !file.FileURL.Valid() && !file.FileID.Valid() {
			return responses.ResponseFunctionCallOutputItemUnionParam{}, false
		}
		return responses.ResponseFunctionCallOutputItemUnionParam{OfInputFile: &file}, true
	default:
		return responses.ResponseFunctionCallOutputItemUnionParam{}, false
	}
}
