package llm

import (
	"fmt"
	"slices"
	"strings"

	"core/shared/config"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// The registry selects protocol differences; the Responses engine owns ordered
// input serialization, streaming, cancellation, and tool-call reconciliation.
type responsesPolicy interface {
	configurePayload(responsesRequestPayloadBuilder, ResponsesRequest, OpenAIAuthMode, *responses.ResponseNewParams) error
	requestOptions(string, ResponsesRequest, responsesDispatchPreparation) []option.RequestOption
	prepareDispatch(string, string, *CodexDispatchContext, *responses.ResponseNewParamsServiceTier, ProviderVariantContract) (*codexDispatchProjection, error)
	reasoningDelta(ReasoningEntry) ReasoningSummaryDelta
	reasoningEntries([]ReasoningEntry) []ReasoningEntry
}

type openAIResponsesPolicy struct{}
type grokResponsesPolicy struct{}

func (openAIResponsesPolicy) reasoningDelta(entry ReasoningEntry) ReasoningSummaryDelta {
	return reasoningSummaryDeltaFromText(entry.SourceCoordinate, entry.ItemIdentity, reasoningRoleSummary, entry.Text)
}

func (openAIResponsesPolicy) reasoningEntries(entries []ReasoningEntry) []ReasoningEntry {
	return normalizeReasoningEntries(entries)
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

func (openAIResponsesPolicy) prepareDispatch(sessionID, model string, dispatch *CodexDispatchContext, tier *responses.ResponseNewParamsServiceTier, variant ProviderVariantContract) (*codexDispatchProjection, error) {
	return validateOpenAIDispatch(sessionID, model, dispatch, variant.ProviderID == "chatgpt-codex", tier)
}

func (grokResponsesPolicy) prepareDispatch(string, string, *CodexDispatchContext, *responses.ResponseNewParamsServiceTier, ProviderVariantContract) (*codexDispatchProjection, error) {
	return nil, nil
}

func (openAIResponsesPolicy) requestOptions(identifier string, request ResponsesRequest, preparation responsesDispatchPreparation) []option.RequestOption {
	opts := []option.RequestOption{option.WithHeader("originator", identifier)}
	if request.SessionID != nil {
		opts = append(opts, option.WithHeader("session-id", *request.SessionID))
	}
	if preparation.mode.IsOAuth && preparation.mode.AccountID != "" {
		opts = append(opts, option.WithHeader("ChatGPT-Account-Id", preparation.mode.AccountID))
	}
	if projection := preparation.projection; projection != nil {
		opts = append(opts, option.WithHeader("x-codex-routing-hint", projection.RoutingHint))
		if turnState, present := request.CodexDispatch.turnStateForRetry(); present {
			opts = append(opts, option.WithHeader(codexTurnStateHeader, turnState))
		}
	}
	return opts
}

func (grokResponsesPolicy) requestOptions(identifier string, request ResponsesRequest, preparation responsesDispatchPreparation) []option.RequestOption {
	if preparation.variant.ProviderID != string(config.ConnectionGrokCLIProxy) {
		return nil
	}
	return []option.RequestOption{
		option.WithHeader("x-xai-token-auth", "xai-grok-cli"),
		option.WithHeader("x-grok-client-version", "1.0.46"),
		option.WithHeader("x-grok-model-override", request.Model),
		option.WithHeader("x-grok-agent-id", identifier),
	}
}

func (openAIResponsesPolicy) configurePayload(b responsesRequestPayloadBuilder, request ResponsesRequest, mode OpenAIAuthMode, out *responses.ResponseNewParams) error {
	out.Store = openai.Bool(b.store)
	if len(out.Tools) > 0 {
		out.ParallelToolCalls = openai.Bool(true)
	}
	if request.EnableNativeWebSearch {
		out.Include = append(out.Include, responses.ResponseIncludableWebSearchCallResults, responses.ResponseIncludableWebSearchCallActionSources)
	}
	if shouldApplyReasoningEffort(request.SupportsReasoningEffort, request.Model, request.ReasoningEffort) {
		out.Reasoning = buildReasoningParam(request.Model, request.ReasoningEffort)
		out.Include = append(out.Include, responses.ResponseIncludableReasoningEncryptedContent)
	}
	if !mode.IsOAuth {
		applySamplingControls(request, out)
	}
	return applyResponseTextConfig(request, configuredTextVerbosity(request.Model, b.modelVerbosity, b.capabilities), out)
}

func (grokResponsesPolicy) configurePayload(_ responsesRequestPayloadBuilder, request ResponsesRequest, _ OpenAIAuthMode, out *responses.ResponseNewParams) error {
	effort := strings.TrimSpace(request.ReasoningEffort)
	out.Reasoning = shared.ReasoningParam{Effort: shared.ReasoningEffort(effort), Summary: shared.ReasoningSummaryConcise}
	out.Include = []responses.ResponseIncludable{responses.ResponseIncludableReasoningEncryptedContent}
	applySamplingControls(request, out)
	return applyResponseTextConfig(request, "", out)
}

func ValidateModelReasoningEffort(model, effort string) error {
	contract, known := LookupModelCapabilityContract(model)
	if !known || effort == "" {
		return nil
	}
	if slices.Contains(contract.SupportedReasoningEfforts, effort) {
		return nil
	}
	return fmt.Errorf("%w: model %q does not support reasoning effort %q", ErrInvalidRequest, model, effort)
}

func applySamplingControls(request ResponsesRequest, out *responses.ResponseNewParams) {
	if request.MaxTokens > 0 {
		out.MaxOutputTokens = openai.Int(int64(request.MaxTokens))
	}
	if request.Temperature != 0 {
		out.Temperature = openai.Float(request.Temperature)
	}
}

func applyResponseTextConfig(request ResponsesRequest, verbosity string, out *responses.ResponseNewParams) error {
	textConfig, ok, err := buildResponseTextConfig(request.StructuredOutput, verbosity)
	if err != nil {
		return err
	}
	if ok {
		out.Text = textConfig
	}
	return nil
}
