package llm

import (
	"encoding/json"
	"fmt"

	"github.com/openai/openai-go/v3/responses"
)

// OpenAIResponsesPayloadOptions mirrors the transport knobs that affect the
// serialized Responses API payload.
type OpenAIResponsesPayloadOptions struct {
	Store          bool
	ModelVerbosity string
	Capabilities   ProviderCapabilities
	OAuth          bool
}

// MarshalOpenAIResponsesRequestJSON renders the exact JSON body that Kent's
// OpenAI Responses transport would send for the provided request.
func MarshalOpenAIResponsesRequestJSON(request Request, options OpenAIResponsesPayloadOptions) ([]byte, error) {
	if err := request.Validate(); err != nil {
		return nil, err
	}
	payload, err := buildOpenAIResponsesRequestPayload(request, options)
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal openai responses payload: %w", err)
	}
	return data, nil
}

func buildOpenAIResponsesRequestPayload(request Request, options OpenAIResponsesPayloadOptions) (responses.ResponseNewParams, error) {
	builder := newOpenAIRequestPayloadBuilder(options.Store, options.ModelVerbosity, options.Capabilities)
	return builder.BuildResponse(openAIRequestFromRequest(request), openAIAuthMode{IsOAuth: options.OAuth})
}

func openAIRequestFromRequest(request Request) OpenAIRequest {
	return OpenAIRequest{
		Model:                   request.Model,
		Temperature:             request.Temperature,
		MaxTokens:               request.MaxTokens,
		ReasoningEffort:         request.ReasoningEffort,
		SupportsReasoningEffort: request.SupportsReasoningEffort,
		FastMode:                request.FastMode,
		EnableNativeWebSearch:   request.EnableNativeWebSearch,
		SystemPrompt:            request.SystemPrompt,
		PromptCacheKey:          request.PromptCacheKey,
		SessionID:               request.SessionID,
		Items:                   CloneResponseItems(request.Items),
		Tools:                   append([]Tool(nil), request.Tools...),
		StructuredOutput:        request.StructuredOutput,
	}
}
