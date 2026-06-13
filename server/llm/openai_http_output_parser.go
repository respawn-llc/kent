package llm

import (
	"cmp"
	"encoding/json"
	"slices"
	"strings"

	"core/shared/clientui"
	"core/shared/textutil"

	"github.com/openai/openai-go/v3/responses"
)

type responseOutputItemParser interface {
	Parse(item responses.ResponseOutputItemUnion) parsedResponseOutputItem
}

type parsedResponseOutputItem struct {
	CanonicalItems    []ResponseItem
	AssistantSegments []assistantOutputSegment
	ToolCalls         []ToolCall
	Reasoning         []ReasoningEntry
	ReasoningItems    []ReasoningItem
}

type responseOutputItemParsers struct {
	byType map[string]responseOutputItemParser
}

type responseOutputItemParserRegistration struct {
	itemType string
	parser   responseOutputItemParser
}

func newResponseOutputItemParsers(registrations ...responseOutputItemParserRegistration) responseOutputItemParsers {
	byType := make(map[string]responseOutputItemParser, len(registrations))
	for _, registration := range registrations {
		byType[registration.itemType] = registration.parser
	}
	return responseOutputItemParsers{byType: byType}
}

func parseOutputItems(items []responses.ResponseOutputItemUnion) ([]ResponseItem, string, MessagePhase, []ToolCall, []ReasoningEntry, []ReasoningItem) {
	parsers := newResponseOutputItemParsers(
		responseOutputItemParserRegistration{itemType: "message", parser: messageOutputItemParser{}},
		responseOutputItemParserRegistration{itemType: "function_call", parser: functionCallOutputItemParser{}},
		responseOutputItemParserRegistration{itemType: "custom_tool_call", parser: customToolCallOutputItemParser{}},
		responseOutputItemParserRegistration{itemType: "reasoning", parser: reasoningOutputItemParser{}},
		responseOutputItemParserRegistration{itemType: "compaction", parser: compactionOutputItemParser{}},
	)
	canonical := make([]ResponseItem, 0, len(items))
	assistantSegments := make([]assistantOutputSegment, 0, len(items))
	toolCalls := make([]ToolCall, 0, len(items))
	reasoning := make([]ReasoningEntry, 0, len(items))
	reasoningItems := make([]ReasoningItem, 0, len(items))

	for outputIndex, item := range items {
		parsed, ok := parsers.byType[item.Type]
		if !ok {
			raw := json.RawMessage(item.RawJSON())
			if len(raw) > 0 && json.Valid(raw) {
				canonical = append(canonical, ResponseItem{Type: ResponseItemTypeOther, OutputIndex: int64(outputIndex), Raw: raw})
			}
			continue
		}
		contribution := parsed.Parse(item)
		stampParsedOutputIndex(&contribution, int64(outputIndex))
		canonical = append(canonical, contribution.CanonicalItems...)
		assistantSegments = append(assistantSegments, contribution.AssistantSegments...)
		toolCalls = append(toolCalls, contribution.ToolCalls...)
		reasoning = append(reasoning, contribution.Reasoning...)
		reasoningItems = append(reasoningItems, contribution.ReasoningItems...)
	}

	assistantText, assistantPhase, _, _ := resolveAssistantOutput(assistantSegments)
	return canonical, assistantText, assistantPhase, toolCalls, reasoning, reasoningItems
}

func stampParsedOutputIndex(parsed *parsedResponseOutputItem, outputIndex int64) {
	if parsed == nil {
		return
	}
	for idx := range parsed.CanonicalItems {
		parsed.CanonicalItems[idx].OutputIndex = outputIndex
	}
	for idx := range parsed.AssistantSegments {
		parsed.AssistantSegments[idx].OutputIndex = outputIndex
	}
}

type messageOutputItemParser struct{}

func (messageOutputItemParser) Parse(item responses.ResponseOutputItemUnion) parsedResponseOutputItem {
	role := Role(strings.TrimSpace(string(item.Role)))
	if role == "" {
		role = RoleAssistant
	}
	textParts := make([]string, 0, len(item.Content))
	for _, part := range item.Content {
		if part.Type == "output_text" || part.Type == "text" || part.Type == "input_text" {
			textParts = append(textParts, part.Text)
		}
	}
	text := strings.Join(textParts, "")
	phase := clientui.NormalizeMessagePhase(string(item.Phase))
	raw := json.RawMessage(item.RawJSON())
	parsed := parsedResponseOutputItem{
		CanonicalItems: []ResponseItem{{
			Type:    ResponseItemTypeMessage,
			Role:    role,
			Phase:   phase,
			ID:      item.ID,
			Content: text,
			Raw:     raw,
		}},
	}
	if role == RoleAssistant {
		parsed.AssistantSegments = append(parsed.AssistantSegments, assistantOutputSegment{Text: text, Phase: phase})
	}
	return parsed
}

type functionCallOutputItemParser struct{}

func (functionCallOutputItemParser) Parse(item responses.ResponseOutputItemUnion) parsedResponseOutputItem {
	call := item.AsFunctionCall()
	callID := textutil.FirstNonEmpty(strings.TrimSpace(call.CallID), strings.TrimSpace(call.ID))
	name := strings.TrimSpace(call.Name)
	if callID == "" && name == "" {
		return parsedResponseOutputItem{}
	}
	arguments := normalizeToolInput(call.Arguments)
	raw := json.RawMessage(item.RawJSON())
	return parsedResponseOutputItem{
		CanonicalItems: []ResponseItem{{
			Type:      ResponseItemTypeFunctionCall,
			ID:        strings.TrimSpace(call.ID),
			CallID:    callID,
			Name:      call.Name,
			Arguments: arguments,
			Raw:       raw,
		}},
		ToolCalls: []ToolCall{{
			ID:    callID,
			Name:  call.Name,
			Input: arguments,
		}},
	}
}

type reasoningOutputItemParser struct{}

type customToolCallOutputItemParser struct{}

func (customToolCallOutputItemParser) Parse(item responses.ResponseOutputItemUnion) parsedResponseOutputItem {
	call := item.AsCustomToolCall()
	callID := textutil.FirstNonEmpty(strings.TrimSpace(call.CallID), strings.TrimSpace(call.ID))
	name := strings.TrimSpace(call.Name)
	if callID == "" && name == "" {
		return parsedResponseOutputItem{}
	}
	raw := json.RawMessage(item.RawJSON())
	return parsedResponseOutputItem{
		CanonicalItems: []ResponseItem{{
			Type:        ResponseItemTypeCustomToolCall,
			ID:          strings.TrimSpace(call.ID),
			CallID:      callID,
			Name:        call.Name,
			CustomInput: call.Input,
			Raw:         raw,
		}},
		ToolCalls: []ToolCall{{
			ID:          callID,
			Name:        call.Name,
			Input:       normalizeToolInput(call.Input),
			Custom:      true,
			CustomInput: call.Input,
		}},
	}
}

func (reasoningOutputItemParser) Parse(item responses.ResponseOutputItemUnion) parsedResponseOutputItem {
	reasoningItem := item.AsReasoning()
	summaries := make([]ReasoningEntry, 0, len(reasoningItem.Summary))
	reasoning := make([]ReasoningEntry, 0, len(reasoningItem.Summary))
	for _, summary := range reasoningItem.Summary {
		text := strings.TrimSpace(summary.Text)
		if text == "" {
			continue
		}
		entry := ReasoningEntry{Role: reasoningRoleSummary, Text: text}
		summaries = append(summaries, entry)
		reasoning = append(reasoning, entry)
	}
	raw := json.RawMessage(item.RawJSON())
	parsed := parsedResponseOutputItem{
		CanonicalItems: []ResponseItem{{
			Type:             ResponseItemTypeReasoning,
			ID:               strings.TrimSpace(reasoningItem.ID),
			ReasoningSummary: summaries,
			EncryptedContent: strings.TrimSpace(reasoningItem.EncryptedContent),
			Raw:              raw,
		}},
		Reasoning: reasoning,
	}
	if id := strings.TrimSpace(reasoningItem.ID); id != "" {
		if encrypted := strings.TrimSpace(reasoningItem.EncryptedContent); encrypted != "" {
			parsed.ReasoningItems = append(parsed.ReasoningItems, ReasoningItem{ID: id, EncryptedContent: encrypted})
		}
	}
	return parsed
}

type compactionOutputItemParser struct{}

func (compactionOutputItemParser) Parse(item responses.ResponseOutputItemUnion) parsedResponseOutputItem {
	compactionItem := item.AsCompaction()
	return parsedResponseOutputItem{
		CanonicalItems: []ResponseItem{{
			Type:             ResponseItemTypeCompaction,
			ID:               strings.TrimSpace(compactionItem.ID),
			EncryptedContent: strings.TrimSpace(compactionItem.EncryptedContent),
			Raw:              json.RawMessage(item.RawJSON()),
		}},
	}
}

type assistantOutputSegment struct {
	Text        string
	Phase       MessagePhase
	OutputIndex int64
}

func resolveAssistantOutput(segments []assistantOutputSegment) (string, MessagePhase, int64, bool) {
	if len(segments) == 0 {
		return "", "", 0, false
	}
	sorted := append([]assistantOutputSegment(nil), segments...)
	slices.SortFunc(sorted, func(a, b assistantOutputSegment) int {
		return cmp.Compare(a.OutputIndex, b.OutputIndex)
	})
	last := len(sorted) - 1
	if sorted[last].Phase == "" {
		return sorted[last].Text, "", sorted[last].OutputIndex, true
	}
	phase := sorted[last].Phase
	start := last
	for start > 0 {
		if sorted[start-1].Phase != phase {
			break
		}
		start--
	}
	textParts := make([]string, 0, last-start+1)
	for i := start; i <= last; i++ {
		textParts = append(textParts, sorted[i].Text)
	}
	return strings.Join(textParts, ""), phase, sorted[start].OutputIndex, true
}
