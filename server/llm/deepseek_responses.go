package llm

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"core/shared/config"

	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

type deepseekResponsesPolicy struct{}

func (deepseekResponsesPolicy) isHostedCustomToolCall(string) bool { return false }

func (deepseekResponsesPolicy) usage(raw responses.ResponseUsage, window int) (Usage, error) {
	return openAIResponsesPolicy{}.usage(raw, window)
}

func (deepseekResponsesPolicy) compact(context.Context, *HTTPTransport, ResponsesRequest, responsesDispatchPreparation, int) (ResponsesCompactionResponse, error) {
	return ResponsesCompactionResponse{}, fmt.Errorf("provider %s does not support remote compaction", config.ConnectionDeepSeekAPIKey)
}

func (deepseekResponsesPolicy) reasoningDelta(entry ReasoningEntry) ReasoningSummaryDelta {
	return ReasoningSummaryDelta{
		SourceCoordinate: CloneReasoningSourceCoordinate(entry.SourceCoordinate),
		ItemIdentity:     CloneReasoningItemIdentity(entry.ItemIdentity), Role: reasoningRoleSummary, Text: entry.Text,
	}
}

func (deepseekResponsesPolicy) reasoningEntries(entries []ReasoningEntry) []ReasoningEntry {
	return entries
}

func (deepseekResponsesPolicy) prepareDispatch(string, string, *CodexDispatchContext, *responses.ResponseNewParamsServiceTier, ProviderVariantContract) (*codexDispatchProjection, error) {
	return nil, nil
}

func (deepseekResponsesPolicy) requestHeaders(string, ResponsesRequest, responsesDispatchPreparation) http.Header {
	return http.Header{}
}

func (deepseekResponsesPolicy) configurePayload(_ responsesRequestPayloadBuilder, request ResponsesRequest, _ OpenAIAuthMode, out *responses.ResponseNewParams) error {
	effort := strings.TrimSpace(request.ReasoningEffort)
	out.Reasoning = shared.ReasoningParam{Effort: shared.ReasoningEffort(effort)}
	out.Include = nil
	applySamplingControls(request, out)
	return applyResponseTextConfig(request, "", out)
}

func newDeepSeekErrorReducer(providerID string) ProviderErrorReducer {
	return responsesErrorReducer{
		providerID: providerID, decode: decodeResponsesErrorPayload,
		classify: func(status *int, code, _ string) UnifiedErrorCode {
			if status != nil && *status == http.StatusPaymentRequired {
				return UnifiedErrorCodeUsageExhausted
			}
			return classifyResponsesErrorCode(status, code)
		},
	}
}
