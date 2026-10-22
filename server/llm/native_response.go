package llm

import (
	"encoding/json"
	"fmt"

	"core/server/session"
	"core/shared/modelcontract"
	"core/shared/textutil"

	"github.com/openai/openai-go/v3/responses"
)

// NativeResponse owns immutable accepted output. Presentation is derived once;
// its prepared items retain the original bytes, independent of local phase/UI.
type NativeResponse struct {
	body  session.NativeResponseBody
	items []ResponseItem
}

func (n *NativeResponse) Record() *session.NativeResponseBody {
	if n == nil {
		return nil
	}
	body := session.NativeResponseBody{Format: n.body.Format, Items: make([][]byte, len(n.body.Items)), ReasoningType: textutil.Pointer(n.body.ReasoningType)}
	body.ReasoningPresentation = n.ReasoningPresentation()
	for i, raw := range n.body.Items {
		body.Items[i] = append([]byte(nil), raw...)
	}
	return &body
}

func (n *NativeResponse) WithReasoningPresentation(presentation []session.NativeReasoningPresentation) *NativeResponse {
	cloned := *n
	cloned.body.ReasoningPresentation = presentation
	cloned.body.ReasoningPresentation = cloned.ReasoningPresentation()
	return &cloned
}

func (n *NativeResponse) ReasoningPresentation() []session.NativeReasoningPresentation {
	presentation := append([]session.NativeReasoningPresentation(nil), n.body.ReasoningPresentation...)
	for i := range presentation {
		presentation[i].DurationMs = textutil.Pointer(presentation[i].DurationMs)
		presentation[i].KentTraceID = textutil.Pointer(presentation[i].KentTraceID)
	}
	return presentation
}

func (n *NativeResponse) WithReasoningType(typ *modelcontract.ReasoningType) *NativeResponse {
	cloned := &NativeResponse{body: n.body, items: CloneResponseItems(n.items)}
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
	native := &NativeResponse{body: body, items: items}
	native.body = *native.Record()
	projected.Assistant.NativeResponse = native.WithReasoningType(body.ReasoningType)
	for i := range projected.Assistant.ReasoningItems {
		projected.Assistant.ReasoningItems[i].Attribution = &modelcontract.ReasoningAttribution{Type: textutil.Pointer(body.ReasoningType)}
	}
	return projected.Assistant, nil
}
