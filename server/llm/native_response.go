package llm

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"

	"core/server/sessionstore"
	"core/shared/modelcontract"
	"core/shared/textutil"

	"github.com/openai/openai-go/v3/responses"
)

// NativeResponse owns immutable accepted output. Presentation is derived once;
// its prepared items retain the original bytes, independent of local phase/UI.
type NativeResponse struct {
	body         session.NativeResponseBody
	items        []ResponseItem
	displayItems []ResponseItem
}

func (n *NativeResponse) Record() *session.NativeResponseBody {
	if n == nil {
		return nil
	}
	body := session.NativeResponseBody{Format: n.body.Format, Items: make([][]byte, len(n.body.Items)), ReasoningType: textutil.Pointer(n.body.ReasoningType)}
	body.ReasoningPresentation = cloneNativeReasoningPresentation(n.body.ReasoningPresentation)
	body.AssistantPresentation = append([]session.NativeAssistantPresentation(nil), n.body.AssistantPresentation...)
	for i := range body.AssistantPresentation {
		body.AssistantPresentation[i].TrailingWhitespace = append([]byte(nil), body.AssistantPresentation[i].TrailingWhitespace...)
	}
	for i, raw := range n.body.Items {
		body.Items[i] = append([]byte(nil), raw...)
	}
	return &body
}

// PresentationItems changes only local display text, never prepared native input.
func (n *NativeResponse) PresentationItems() []ResponseItem {
	return append([]ResponseItem(nil), n.displayItems...)
}

func newNativeResponse(body session.NativeResponseBody, items []ResponseItem) (*NativeResponse, error) {
	n := &NativeResponse{body: body, items: items}
	display := append([]ResponseItem(nil), items...)
	byIndex := make(map[int64]*ResponseItem, len(display))
	for i := range display {
		if display[i].Type == ResponseItemTypeMessage {
			byIndex[int64(i)] = &display[i]
		}
	}
	seen := make(map[int64]struct{}, len(n.body.AssistantPresentation))
	for _, fact := range n.body.AssistantPresentation {
		if _, duplicate := seen[fact.OutputIndex]; duplicate {
			return nil, fmt.Errorf("duplicate native assistant presentation coordinate")
		}
		seen[fact.OutputIndex] = struct{}{}
		source := byIndex[fact.OutputIndex]
		if source == nil || source.Content == nil || fact.LeadingBytes < 0 || fact.LeadingBytes > len(*source.Content) ||
			(fact.LeadingBytes == 0 && len(fact.TrailingWhitespace) == 0) || strings.TrimSpace(string(fact.TrailingWhitespace)) != "" {
			return nil, fmt.Errorf("invalid native assistant presentation source")
		}
		text := *source.Content
		if fact.LeadingBytes > 0 && len(text)-len(strings.TrimLeftFunc(text, unicode.IsSpace)) != fact.LeadingBytes {
			return nil, fmt.Errorf("invalid native assistant whitespace offset")
		}
		source.Content = textutil.Value(text[fact.LeadingBytes:] + string(fact.TrailingWhitespace))
	}
	n.displayItems = display
	return n, nil
}

// Presentation facts are stored at item positions; callers identify live stream sources.
func (n *NativeResponse) WithReasoningPresentation(presentation []session.NativeReasoningPresentation) (*NativeResponse, error) {
	cloned := *n
	cloned.body.ReasoningPresentation = cloneNativeReasoningPresentation(presentation)
	if len(presentation) == 0 {
		return &cloned, nil
	}
	positions := make(map[int64]int64, len(n.items))
	for index, item := range n.items {
		positions[item.OutputIndex] = int64(index)
	}
	for index := range cloned.body.ReasoningPresentation {
		fact := &cloned.body.ReasoningPresentation[index]
		position, exists := positions[fact.OutputIndex]
		if !exists {
			return nil, fmt.Errorf("native reasoning presentation has no output source %d", fact.OutputIndex)
		}
		fact.OutputIndex = position
	}
	return &cloned, nil
}

func (n *NativeResponse) ReasoningPresentation() []session.NativeReasoningPresentation {
	presentation := cloneNativeReasoningPresentation(n.body.ReasoningPresentation)
	for index := range presentation {
		presentation[index].OutputIndex = n.items[presentation[index].OutputIndex].OutputIndex
	}
	return presentation
}

func cloneNativeReasoningPresentation(source []session.NativeReasoningPresentation) []session.NativeReasoningPresentation {
	presentation := append([]session.NativeReasoningPresentation(nil), source...)
	for i := range presentation {
		presentation[i].DurationMs = textutil.Pointer(presentation[i].DurationMs)
		presentation[i].KentTraceID = textutil.Pointer(presentation[i].KentTraceID)
	}
	return presentation
}

func (n *NativeResponse) WithReasoningType(typ *modelcontract.ReasoningType) *NativeResponse {
	copy := *n
	cloned := &copy
	cloned.items = CloneResponseItems(n.items)
	cloned.body.ReasoningType = textutil.Pointer(typ)
	for i := range cloned.items {
		item := &cloned.items[i]
		if item.Type == ResponseItemTypeReasoning || item.Type == ResponseItemTypeCompaction {
			itemType := textutil.Pointer(typ)
			if item.EncryptedContent == nil {
				itemType = textutil.Value(modelcontract.ReasoningTypeUnencrypted)
			}
			item.Attribution = &modelcontract.ReasoningAttribution{Type: itemType}
		}
	}
	return cloned
}

func MessageFromNativeResponse(body session.NativeResponseBody) (Message, error) {
	var policy responsesPolicy
	switch body.Format {
	case session.NativeResponseOpenAI:
		policy = openAIResponsesPolicy{}
	case session.NativeResponseGrok:
		policy = grokResponsesPolicy{}
	default:
		return Message{}, fmt.Errorf("unknown native response format %q", body.Format)
	}
	output := make([]responses.ResponseOutputItemUnion, len(body.Items))
	for i, raw := range body.Items {
		if err := json.Unmarshal(raw, &output[i]); err != nil {
			return Message{}, fmt.Errorf("decode native response item %d: %w", i, err)
		}
	}
	items, text, _, phase, calls, _, reasoning, err := parseOutputItems(output, policy)
	if err != nil {
		return Message{}, err
	}
	coordinates := make(map[[2]int64]struct{})
	for _, item := range items {
		for _, summary := range item.ReasoningSummary {
			coordinates[[2]int64{item.OutputIndex, *summary.SourceCoordinate.PartIndex}] = struct{}{}
		}
	}
	for _, presentation := range body.ReasoningPresentation {
		key := [2]int64{presentation.OutputIndex, presentation.PartIndex}
		if _, exists := coordinates[key]; !exists {
			return Message{}, fmt.Errorf("native reasoning presentation has no unique summary source")
		}
		delete(coordinates, key)
	}
	projected, err := responseFromResponses(ResponsesResponse{
		AssistantText: text, ProviderPhase: phase, ToolCalls: calls, ReasoningItems: reasoning,
	})
	if err != nil {
		return Message{}, err
	}
	native, err := newNativeResponse(body, items)
	if err != nil {
		return Message{}, err
	}
	native.body = *native.Record()
	projected.Assistant.NativeResponse = native.WithReasoningType(body.ReasoningType)
	for i := range projected.Assistant.ReasoningItems {
		projected.Assistant.ReasoningItems[i].Attribution = &modelcontract.ReasoningAttribution{Type: textutil.Pointer(body.ReasoningType)}
	}
	return projected.Assistant, nil
}
