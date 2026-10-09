package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"core/shared/textutil"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

func (t *HTTPTransport) compactStandardResponses(ctx context.Context, request ResponsesRequest, preparation responsesDispatchPreparation, windowTokens int) (ResponsesCompactionResponse, error) {
	if err := validateRetainedConnectionContext(request.Items, preparation.providerCaps); err != nil {
		return ResponsesCompactionResponse{}, err
	}
	input, err := buildResponsesInput(request.Items)
	if err != nil {
		return ResponsesCompactionResponse{}, err
	}
	payload := responses.ResponseCompactParams{
		Model: responses.ResponseCompactParamsModel(request.Model),
		Input: responses.ResponseCompactParamsInputUnion{OfResponseInputItemArray: input},
	}
	if request.SystemPrompt != "" {
		payload.Instructions = openai.String(request.SystemPrompt)
	}
	service := responses.NewResponseService(
		option.WithBaseURL(t.serviceBaseURL(preparation.mode)),
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
	checkpoint, err := requireSingleEncryptedCompactionOutput(items)
	if err != nil {
		return ResponsesCompactionResponse{}, newOpenAIProviderContractError(preparation.variant.ProviderID, rawResponse, err)
	}
	evidence, err := providerUsageEvidenceFromResponse(response, items)
	if err != nil {
		return ResponsesCompactionResponse{}, err
	}
	evidence.ProviderID = textutil.Value(preparation.variant.ProviderID)
	evidence.RequestedModel = request.Model
	usage, err := preparation.variant.ResponsesPolicy.usage(response.Usage, windowTokens)
	if err != nil {
		return ResponsesCompactionResponse{}, err
	}
	return ResponsesCompactionResponse{
		Checkpoint: checkpoint, Usage: usage, ProviderEvidence: evidence,
	}, nil
}
