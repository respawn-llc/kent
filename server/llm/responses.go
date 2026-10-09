package llm

import (
	"context"
	"fmt"
	"strings"

	"core/shared/modelcontract"
	"core/shared/textutil"
	"core/shared/transcript"
)

type ResponsesRequest struct {
	Model                   string
	Temperature             float64
	MaxTokens               int
	ReasoningEffort         string
	SupportsReasoningEffort bool
	FastMode                bool
	EnableNativeWebSearch   bool
	SystemPrompt            string
	PromptCacheKey          string
	SessionID               *string
	CodexDispatch           *CodexDispatchContext
	Items                   []ResponseItem
	Tools                   []Tool
	ToolChoiceMode          ToolChoiceMode
	StructuredOutput        *StructuredOutput
}

// RequestAsResponses projects the provider-agnostic Request into the Responses
// wire DTO. It is the single source of truth for that projection: the
// ResponsesClient request methods and the offline inspection seam both use it, so
// the wire shape stays identical between live generation and captured payloads.
func RequestAsResponses(request Request) ResponsesRequest {
	var structuredOutput *StructuredOutput
	if request.StructuredOutput != nil {
		cloned := *request.StructuredOutput
		structuredOutput = &cloned
	}
	return ResponsesRequest{
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
		CodexDispatch:           request.CodexDispatch,
		Items:                   CloneResponseItems(request.Items),
		Tools:                   append([]Tool(nil), request.Tools...),
		ToolChoiceMode:          request.ToolChoiceMode,
		StructuredOutput:        structuredOutput,
	}
}

type ResponsesResponse struct {
	AssistantText     *string
	ProviderPhase     *ProviderPhase
	ServedModel       *string
	ProviderEvidence  modelcontract.ProviderUsageEvidence
	ReasoningIncluded bool
	ToolCalls         []ToolCall
	Reasoning         []ReasoningEntry
	ReasoningItems    []ReasoningItem
	OutputItems       []ResponseItem
	Usage             Usage
}

type ResponsesCompactionResponse struct {
	Checkpoint       ResponseItem
	Usage            Usage
	ProviderEvidence modelcontract.ProviderUsageEvidence
}

type ResponsesTransport interface {
	Generate(ctx context.Context, request ResponsesRequest, callbacks StreamCallbacks) (ResponsesResponse, error)
	Compact(ctx context.Context, request ResponsesRequest) (ResponsesCompactionResponse, error)
}

type ResponsesModelContextWindowTransport interface {
	ResolveModelContextWindow(ctx context.Context, model string) (int, error)
}

type ResponsesProviderCapabilitiesTransport interface {
	ProviderCapabilities(ctx context.Context) (ProviderCapabilities, error)
}

type ResponsesClient struct {
	transport ResponsesTransport
}

func NewResponsesClient(transport ResponsesTransport) *ResponsesClient {
	return &ResponsesClient{transport: transport}
}

func (c *ResponsesClient) Generate(ctx context.Context, request Request, callbacks StreamCallbacks) (Response, error) {
	if c == nil || c.transport == nil {
		return Response{}, ErrMissingTransport
	}
	if err := request.Validate(); err != nil {
		return Response{}, err
	}

	providerReq := RequestAsResponses(request)

	providerResp, err := c.transport.Generate(ctx, providerReq, callbacks)
	if err != nil {
		return Response{}, fmt.Errorf("responses generate: %w", err)
	}

	return responseFromResponses(providerResp)
}

func responseFromResponses(providerResp ResponsesResponse) (Response, error) {
	if providerResp.ProviderPhase == nil {
		return Response{}, fmt.Errorf("responses response omitted authoritative provider phase fact")
	}
	assistantPhase := providerPhaseProjection(providerResp.ProviderPhase)
	var typedAssistantPhase *MessagePhase
	if assistantPhase != "" {
		typedAssistantPhase = textutil.Value(assistantPhase)
	}
	return Response{
		Assistant: Message{
			Role:           RoleAssistant,
			Content:        resolveAssistantContent(RoleAssistant, assistantPhase, providerResp.AssistantText),
			Phase:          typedAssistantPhase,
			ToolCalls:      append([]ToolCall(nil), providerResp.ToolCalls...),
			ReasoningItems: append([]ReasoningItem(nil), providerResp.ReasoningItems...),
		},
		ProviderPhase:     providerResp.ProviderPhase,
		ServedModel:       textutil.Pointer(providerResp.ServedModel),
		ProviderEvidence:  providerResp.ProviderEvidence.Clone(),
		ReasoningIncluded: providerResp.ReasoningIncluded,
		ToolCalls:         providerResp.ToolCalls,
		Reasoning:         append([]ReasoningEntry(nil), providerResp.Reasoning...),
		ReasoningItems:    append([]ReasoningItem(nil), providerResp.ReasoningItems...),
		OutputItems:       CloneResponseItems(providerResp.OutputItems),
		Usage:             providerResp.Usage,
	}, nil
}

func resolveAssistantContent(role Role, phase MessagePhase, content *string) *string {
	if content == nil {
		return nil
	}
	if strings.TrimSpace(*content) == "" &&
		!transcript.IsBlankAssistantFinal(transcript.AssistantFinalCandidate{
			IsAssistant: role == RoleAssistant,
			IsFinal:     phase == MessagePhaseFinal,
			Content:     content,
		}) {
		return nil
	}
	return textutil.Pointer(content)
}

func (c *ResponsesClient) Compact(ctx context.Context, request CompactionRequest) (CompactionResponse, error) {
	if c == nil || c.transport == nil {
		return CompactionResponse{}, ErrMissingTransport
	}
	if err := request.Validate(); err != nil {
		return CompactionResponse{}, err
	}

	providerReq := RequestAsResponses(request)
	providerResp, err := c.transport.Compact(ctx, providerReq)
	if err != nil {
		return CompactionResponse{}, fmt.Errorf("responses compact: %w", err)
	}
	return CompactionResponse{
		Checkpoint:       CloneResponseItems([]ResponseItem{providerResp.Checkpoint})[0],
		Usage:            providerResp.Usage,
		ProviderEvidence: providerResp.ProviderEvidence.Clone(),
	}, nil
}

func (c *ResponsesClient) ProviderCapabilities(ctx context.Context) (ProviderCapabilities, error) {
	if c == nil || c.transport == nil {
		return ProviderCapabilities{}, ErrMissingTransport
	}
	if transport, ok := c.transport.(ResponsesProviderCapabilitiesTransport); ok {
		return transport.ProviderCapabilities(ctx)
	}
	return ProviderCapabilities{}, fmt.Errorf("provider capabilities are not supported by Responses transport %T", c.transport)
}

func (c *ResponsesClient) ResolveModelContextWindow(ctx context.Context, model string) (int, error) {
	if c == nil || c.transport == nil {
		return 0, ErrMissingTransport
	}
	resolver, ok := c.transport.(ResponsesModelContextWindowTransport)
	if !ok {
		return 0, fmt.Errorf("model context window resolution is not supported by Responses transport")
	}
	return resolver.ResolveModelContextWindow(ctx, model)
}
