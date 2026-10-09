package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	openai "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/packages/param"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared"
)

// ErrCustomToolNameRequired is returned when a custom tool param omits its name.
// Callers match it via errors.Is.
var ErrCustomToolNameRequired = errors.New("custom tool name is required")

type responsesRequestPayloadBuilder struct {
	policy                 responsesPolicy
	store                  bool
	modelVerbosity         string
	capabilities           ProviderCapabilities
	connectionCapabilities ProviderCapabilities
}

type responsesPayloadToolControls struct {
	tools  []responses.ToolUnionParam
	choice responses.ToolChoiceOptions
}

func effectiveServiceTier(fastMode bool, capabilities ProviderCapabilities) *responses.ResponseNewParamsServiceTier {
	if fastMode && SupportsFastModeProvider(capabilities) {
		tier := responses.ResponseNewParamsServiceTierPriority
		return &tier
	}
	return nil
}

func (t *HTTPTransport) effectiveRequestCapabilities(connectionCapabilities ProviderCapabilities) ProviderCapabilities {
	if t.RequestCapabilities != nil {
		return *t.RequestCapabilities
	}
	return connectionCapabilities
}

func (t *HTTPTransport) requestPayloadBuilder(connectionCapabilities ProviderCapabilities, policy responsesPolicy) responsesRequestPayloadBuilder {
	return responsesRequestPayloadBuilder{
		policy: policy,
		store:  t.Store, modelVerbosity: strings.ToLower(strings.TrimSpace(t.ModelVerbosity)),
		capabilities: t.effectiveRequestCapabilities(connectionCapabilities), connectionCapabilities: connectionCapabilities,
	}
}

func (t *HTTPTransport) buildPayload(request ResponsesRequest, mode OpenAIAuthMode, capabilities ProviderCapabilities) (responses.ResponseNewParams, error) {
	variant, err := t.providerVariantForMode(mode)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	builder := t.requestPayloadBuilder(capabilities, variant.ResponsesPolicy)
	return builder.BuildResponse(request, mode)
}

func (t *HTTPTransport) buildDispatchPayload(
	request ResponsesRequest,
	mode OpenAIAuthMode,
	capabilities ProviderCapabilities,
	projection *codexDispatchProjection,
) (responses.ResponseNewParams, error) {
	payload, err := t.buildPayload(request, mode, capabilities)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	applyCodexClientMetadata(&payload, projection)
	return payload, nil
}

func (b responsesRequestPayloadBuilder) BuildResponse(request ResponsesRequest, mode OpenAIAuthMode) (responses.ResponseNewParams, error) {
	if err := validateModelReasoningEffort(request.Model, request.ReasoningEffort); err != nil {
		return responses.ResponseNewParams{}, err
	}
	if err := validateRetainedConnectionContext(request.Items, b.connectionCapabilities); err != nil {
		return responses.ResponseNewParams{}, err
	}
	input, err := buildResponsesInput(request.Items)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	toolControls, err := b.prepareToolControls(request)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}

	out := responses.ResponseNewParams{
		Model: request.Model,
		ToolChoice: responses.ResponseNewParamsToolChoiceUnion{
			OfToolChoiceMode: openai.Opt(toolControls.choice),
		},
	}
	if cacheKey := strings.TrimSpace(request.PromptCacheKey); cacheKey != "" && SupportsPromptCacheKeyProvider(b.capabilities) {
		out.PromptCacheKey = openai.String(cacheKey)
	}
	if len(input) > 0 {
		out.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: input}
	}
	if instructions := strings.TrimSpace(request.SystemPrompt); instructions != "" {
		out.Instructions = openai.String(instructions)
	}
	if len(toolControls.tools) > 0 {
		out.Tools = toolControls.tools
	}
	if request.FastMode && SupportsFastModeProvider(b.capabilities) {
		out.ServiceTier = responses.ResponseNewParamsServiceTierPriority
	}
	if err := b.policy.configurePayload(b, request, mode, &out); err != nil {
		return responses.ResponseNewParams{}, err
	}
	return out, nil
}

func validateRetainedConnectionContext(items []ResponseItem, capabilities ProviderCapabilities) error {
	for _, item := range items {
		switch item.Type {
		case ResponseItemTypeReasoning:
			if capabilities.SupportsReasoningEncrypted {
				continue
			}
			encrypted := item.EncryptedContent
			if len(item.Raw) != 0 {
				var reasoning struct {
					EncryptedContent *string `json:"encrypted_content"`
				}
				if err := json.Unmarshal(item.Raw, &reasoning); err != nil {
					return fmt.Errorf("decode retained reasoning for compatibility validation: %w", err)
				}
				encrypted = reasoning.EncryptedContent
			}
			if encrypted != nil {
				return &RetainedContextCompatibilityError{ItemType: item.Type}
			}
		case ResponseItemTypeCompaction:
			// Native compaction support describes producing checkpoints, not
			// consuming every checkpoint format. Unknown compatibility is sent.
			if !capabilities.SupportsResponsesAPI {
				return &RetainedContextCompatibilityError{ItemType: item.Type}
			}
		}
	}
	return nil
}

func (b responsesRequestPayloadBuilder) prepareToolControls(request ResponsesRequest) (responsesPayloadToolControls, error) {
	if err := ValidateToolChoiceSupport(b.capabilities, request.ToolChoiceMode); err != nil {
		return responsesPayloadToolControls{}, err
	}
	tools, err := b.buildTools(request.Tools, request.EnableNativeWebSearch)
	if err != nil {
		return responsesPayloadToolControls{}, err
	}
	if request.ToolChoiceMode == ToolChoiceModeRequired && len(tools) == 0 {
		return responsesPayloadToolControls{}, fmt.Errorf("%w: required tool choice needs at least one materialized tool", ErrInvalidRequest)
	}
	choice, err := openAIToolChoice(request.ToolChoiceMode)
	if err != nil {
		return responsesPayloadToolControls{}, err
	}
	return responsesPayloadToolControls{tools: tools, choice: choice}, nil
}

func openAIToolChoice(mode ToolChoiceMode) (responses.ToolChoiceOptions, error) {
	switch mode {
	case ToolChoiceModeAutomatic:
		return responses.ToolChoiceOptionsAuto, nil
	case ToolChoiceModeRequired:
		return responses.ToolChoiceOptionsRequired, nil
	default:
		return "", fmt.Errorf("%w: unknown tool choice mode %q", ErrInvalidRequest, mode)
	}
}

func (b responsesRequestPayloadBuilder) BuildCompactV2(request ResponsesRequest, mode OpenAIAuthMode) (responses.ResponseNewParams, error) {
	out, err := b.BuildResponse(request, mode)
	if err != nil {
		return responses.ResponseNewParams{}, err
	}
	input := out.Input.OfInputItemList
	trigger := responses.NewResponseInputItemCompactionTriggerParam()
	input = append(input, responses.ResponseInputItemUnionParam{OfCompactionTrigger: &trigger})
	out.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: input}
	return out, nil
}

func applyCodexClientMetadata(payload *responses.ResponseNewParams, projection *codexDispatchProjection) {
	if projection == nil {
		return
	}
	payload.SetExtraFields(map[string]any{
		"client_metadata": map[string]string{
			"x-codex-turn-metadata": projection.TurnMetadataJSON,
		},
	})
}

func (b responsesRequestPayloadBuilder) buildTools(requestTools []Tool, enableNativeWebSearch bool) ([]responses.ToolUnionParam, error) {
	tools := make([]responses.ToolUnionParam, 0, len(requestTools)+1)
	for _, tool := range requestTools {
		toolParam, err := buildFunctionToolParam(tool)
		if err != nil {
			return nil, err
		}
		tools = append(tools, toolParam)
	}
	if enableNativeWebSearch {
		tools = append(tools, responses.ToolParamOfWebSearch(responses.WebSearchToolTypeWebSearch))
	}
	return tools, nil
}

func buildFunctionToolParam(tool Tool) (responses.ToolUnionParam, error) {
	if tool.Custom != nil {
		name := strings.TrimSpace(tool.Name)
		if name == "" {
			return responses.ToolUnionParam{}, ErrCustomToolNameRequired
		}
		format := shared.CustomToolInputFormatUnionParam{}
		switch strings.TrimSpace(tool.Custom.Type) {
		case "grammar":
			definition := tool.Custom.Definition
			if strings.TrimSpace(definition) == "" {
				return responses.ToolUnionParam{}, fmt.Errorf("custom tool grammar definition is required for %s", name)
			}
			syntax := strings.TrimSpace(tool.Custom.Syntax)
			if syntax == "" {
				syntax = "lark"
			}
			format = shared.CustomToolInputFormatParamOfGrammar(definition, syntax)
		case "", "text":
			text := shared.NewCustomToolInputFormatTextParam()
			format = shared.CustomToolInputFormatUnionParam{OfText: &text}
		default:
			return responses.ToolUnionParam{}, fmt.Errorf("unsupported custom tool format %q for %s", tool.Custom.Type, name)
		}
		custom := responses.CustomToolParam{Name: name, Format: format}
		if description := strings.TrimSpace(tool.Description); description != "" {
			custom.Description = openai.String(description)
		}
		return responses.ToolUnionParam{OfCustom: &custom}, nil
	}
	if !tool.Schema.Prepared() {
		return responses.ToolUnionParam{}, fmt.Errorf("tool schema is not prepared for %s", tool.Name)
	}
	raw, err := json.Marshal(struct {
		Type        string          `json:"type"`
		Name        string          `json:"name"`
		Description string          `json:"description,omitempty"`
		Parameters  json.RawMessage `json:"parameters"`
		Strict      bool            `json:"strict"`
	}{
		Type:        "function",
		Name:        tool.Name,
		Description: strings.TrimSpace(tool.Description),
		Parameters:  json.RawMessage(tool.Schema.JSON()),
		Strict:      tool.Schema.Strict(),
	})
	if err != nil {
		return responses.ToolUnionParam{}, fmt.Errorf("serialize tool schema for %s: %w", tool.Name, err)
	}
	function := param.Override[responses.FunctionToolParam](json.RawMessage(raw))
	return responses.ToolUnionParam{OfFunction: &function}, nil
}

func buildResponseTextConfig(output *StructuredOutput, verbosity string) (responses.ResponseTextConfigParam, bool, error) {
	text := responses.ResponseTextConfigParam{}
	if verbosity != "" {
		text.Verbosity = responses.ResponseTextConfigVerbosity(verbosity)
	}
	if output == nil {
		return text, text.Verbosity != "", nil
	}
	if !output.Schema.Prepared() {
		return responses.ResponseTextConfigParam{}, false, errors.New("structured output schema is not prepared")
	}
	format, err := buildStructuredOutputFormat(output)
	if err != nil {
		return responses.ResponseTextConfigParam{}, false, err
	}
	text.Format = format
	return text, true, nil
}

func buildStructuredOutputFormat(
	output *StructuredOutput,
) (responses.ResponseFormatTextConfigUnionParam, error) {
	raw, err := json.Marshal(struct {
		Type        string          `json:"type"`
		Name        string          `json:"name"`
		Schema      json.RawMessage `json:"schema"`
		Strict      bool            `json:"strict"`
		Description string          `json:"description,omitempty"`
	}{
		Type:        "json_schema",
		Name:        strings.TrimSpace(output.Name),
		Schema:      json.RawMessage(output.Schema.JSON()),
		Strict:      output.Schema.Strict(),
		Description: strings.TrimSpace(output.Description),
	})
	if err != nil {
		return responses.ResponseFormatTextConfigUnionParam{}, fmt.Errorf(
			"serialize structured output schema for %s: %w",
			output.Name,
			err,
		)
	}
	format := param.Override[responses.ResponseFormatTextJSONSchemaConfigParam](json.RawMessage(raw))
	return responses.ResponseFormatTextConfigUnionParam{OfJSONSchema: &format}, nil
}

func shouldApplyReasoningEffort(contractSupport bool, model, effort string) bool {
	effort = strings.TrimSpace(effort)
	if effort == "" {
		return false
	}
	if contractSupport {
		return true
	}
	return SupportsReasoningEffortModel(model)
}

func buildReasoningParam(model, effort string) shared.ReasoningParam {
	param := shared.ReasoningParam{Effort: shared.ReasoningEffort(strings.TrimSpace(effort))}
	if SupportsReasoningSummaryModel(model) {
		param.Summary = shared.ReasoningSummaryConcise
	}
	return param
}

func configuredTextVerbosity(model, configured string, providerCaps ProviderCapabilities) string {
	normalized := strings.ToLower(strings.TrimSpace(configured))
	switch normalized {
	case "low", "medium", "high":
	default:
		return ""
	}
	support := VerbositySupportForModelAndProvider(model, providerCaps)
	if !support.Supported {
		return ""
	}
	for _, level := range support.Levels {
		if level == normalized {
			return normalized
		}
	}
	return ""
}
