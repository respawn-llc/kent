package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"core/shared/textutil"

	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

func (t *HTTPTransport) PrepareCompaction(request CompactionRequest) CompactionRequest {
	if t.registration.Variant.RemoteCompactionProtocol != remoteCompactionStandardResponses {
		return request
	}
	request.Tools = nil
	request.EnableNativeWebSearch = false
	request.ToolChoiceMode = ToolChoiceModeAutomatic
	if request.SystemPrompt != "" {
		prefix := PrepareResponsesInputItems(ItemsFromMessages([]Message{{
			Role: RoleSystem, Content: textutil.Value(request.SystemPrompt),
		}}))
		request.Items = append(prefix, request.Items...)
	}
	return request
}

func (t *HTTPTransport) compactStandardResponses(ctx context.Context, request ResponsesRequest, preparation responsesDispatchPreparation, windowTokens int) (ResponsesCompactionResponse, error) {
	// The standard SDK DTO is narrower than Grok's accepted request envelope.
	// Keep the shared policy's non-tool controls as SDK extension fields.
	generation, err := t.buildPayload(request, preparation.mode, preparation.providerCaps)
	if err != nil {
		return ResponsesCompactionResponse{}, err
	}
	raw, err := json.Marshal(generation)
	if err != nil {
		return ResponsesCompactionResponse{}, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return ResponsesCompactionResponse{}, err
	}
	delete(fields, "tool_choice")
	payload := responses.ResponseCompactParams{}
	extras := make(map[string]any, len(fields))
	for name, value := range fields {
		extras[name] = value
	}
	payload.SetExtraFields(extras)
	service := responses.NewResponseService(
		option.WithBaseURL(t.serviceBaseURL()),
		option.WithHTTPClient(t.Client), option.WithMaxRetries(0),
	)
	var rawResponse *http.Response
	options := append(t.buildRequestOptions(request, preparation), option.WithResponseInto(&rawResponse), requestCompressionOption(preparation.variant))
	compacted, err := service.Compact(ctx, payload, options...)
	if err != nil {
		return ResponsesCompactionResponse{}, newResponsesRequestErrorMapper(preparation.variant.ProviderID).Map(err, rawResponse, "compact responses")
	}
	// Compaction uses the same output/usage envelope as generation. Decode its
	// original JSON into that shared envelope so field presence and extensions survive.
	var response responses.Response
	if err := json.Unmarshal([]byte(compacted.RawJSON()), &response); err != nil {
		return ResponsesCompactionResponse{}, fmt.Errorf("decode compaction response: %w", err)
	}
	items, _, _, _, _, _, _, err := parseOutputItems(response.Output)
	if err != nil {
		return ResponsesCompactionResponse{}, err
	}
	evidence, err := providerUsageEvidenceFromResponse(response, items)
	if err != nil {
		return ResponsesCompactionResponse{}, err
	}
	evidence.ProviderID = textutil.Value(preparation.variant.ProviderID)
	evidence.RequestedModel = request.Model
	evidence.RequestedServiceTier = t.providerUsageRequestEvidence(request, preparation, generation).RequestedServiceTier
	usage, err := preparation.variant.ResponsesPolicy.usage(response.Usage, windowTokens)
	if err != nil {
		return ResponsesCompactionResponse{}, err
	}
	return ResponsesCompactionResponse{
		OutputItems: items, ContextPlacement: CompactionContextAfterOutput,
		Usage: usage, ProviderEvidence: evidence,
	}, nil
}
