package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"core/shared/textutil"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// Grok Build protocol version, maintained with the proxy integration.
const grokCLIProtocolVersion = "1.0.46"

type grokResponsesPolicy struct{}
type grokCLIResponsesPolicy struct{ grokResponsesPolicy }

func (grokResponsesPolicy) usage(raw responses.ResponseUsage, window int) (Usage, error) {
	usage := usageFromSDK(raw, window)
	// The OpenAI SDK has no typed Grok context_details field. Decode this
	// provider extension once to keep measured context separate from billing.
	field, present := raw.JSON.ExtraFields["context_details"]
	if !present {
		return usage, nil
	}
	var context struct {
		Input  *int `json:"input_tokens"`
		Output *int `json:"output_tokens"`
	}
	if err := json.Unmarshal([]byte(field.Raw()), &context); err != nil {
		return Usage{}, fmt.Errorf("decode Grok context usage: %w", err)
	}
	if context.Input != nil && context.Output != nil {
		usage.ContextUsage = &ContextUsage{Tokens: *context.Input + *context.Output, MeasurementPoint: ContextMeasurementCompletedResponse}
	}
	return usage, nil
}

func (grokResponsesPolicy) reasoningDelta(entry ReasoningEntry) ReasoningSummaryDelta {
	return ReasoningSummaryDelta{
		SourceCoordinate: CloneReasoningSourceCoordinate(entry.SourceCoordinate),
		ItemIdentity:     CloneReasoningItemIdentity(entry.ItemIdentity), Role: reasoningRoleSummary, Text: entry.Text,
	}
}

func (grokResponsesPolicy) reasoningEntries(entries []ReasoningEntry) []ReasoningEntry {
	return entries
}

func (grokResponsesPolicy) prepareDispatch(string, string, *CodexDispatchContext, *responses.ResponseNewParamsServiceTier, ProviderVariantContract) (*codexDispatchProjection, error) {
	return nil, nil
}

func (grokResponsesPolicy) requestOptions(string, ResponsesRequest, responsesDispatchPreparation) []option.RequestOption {
	return nil
}

func (grokCLIResponsesPolicy) requestOptions(identifier string, request ResponsesRequest, _ responsesDispatchPreparation) []option.RequestOption {
	opts := []option.RequestOption{
		option.WithHeader("x-xai-token-auth", "xai-grok-cli"),
		option.WithHeader("x-grok-client-version", grokCLIProtocolVersion),
		option.WithHeader("x-grok-model-override", request.Model),
		option.WithHeader("x-grok-agent-id", identifier),
	}
	if request.SessionID != nil {
		opts = append(opts,
			option.WithHeader("x-grok-conv-id", *request.SessionID),
			option.WithHeader("x-grok-session-id", *request.SessionID),
		)
	}
	return opts
}

func (grokResponsesPolicy) configurePayload(_ responsesRequestPayloadBuilder, request ResponsesRequest, _ OpenAIAuthMode, out *responses.ResponseNewParams) error {
	effort := strings.TrimSpace(request.ReasoningEffort)
	out.Reasoning = shared.ReasoningParam{Effort: shared.ReasoningEffort(effort), Summary: shared.ReasoningSummaryConcise}
	out.Include = []responses.ResponseIncludable{responses.ResponseIncludableReasoningEncryptedContent}
	applySamplingControls(request, out)
	return applyResponseTextConfig(request, "", out)
}

func (grokResponsesPolicy) compact(ctx context.Context, transport *HTTPTransport, request ResponsesRequest, preparation responsesDispatchPreparation, windowTokens int) (ResponsesCompactionResponse, error) {
	payload, err := transport.buildPayload(request, preparation.mode, preparation.providerCaps)
	if err != nil {
		return ResponsesCompactionResponse{}, err
	}
	// Only use the SDK's generic endpoint dispatcher. NewClient also imports
	// OpenAI environment credentials and headers, which cannot govern Grok.
	client := openai.Client{Options: []option.RequestOption{
		option.WithBaseURL(transport.serviceBaseURL()),
		option.WithHTTPClient(transport.Client), option.WithMaxRetries(0),
	}}
	var rawResponse *http.Response
	options := append(transport.buildRequestOptions(request, preparation), option.WithResponseInto(&rawResponse), requestCompressionOption(preparation.variant))
	var response responses.Response
	if err := client.Post(ctx, "responses/compact", payload, &response, options...); err != nil {
		return ResponsesCompactionResponse{}, newResponsesRequestErrorMapper(preparation.variant.ProviderID).Map(err, rawResponse, "compact responses")
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
	evidence.RequestedServiceTier = transport.providerUsageRequestEvidence(request, preparation, payload).RequestedServiceTier
	usage, err := preparation.variant.ResponsesPolicy.usage(response.Usage, windowTokens)
	if err != nil {
		return ResponsesCompactionResponse{}, err
	}
	return ResponsesCompactionResponse{
		OutputItems: items, ContextPlacement: CompactionContextAfterOutput,
		Usage: usage, ProviderEvidence: evidence,
	}, nil
}
