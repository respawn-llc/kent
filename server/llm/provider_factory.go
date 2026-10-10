package llm

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"core/server/httpcompression"
	"core/shared/config"
	"core/shared/textutil"
)

var ErrUnsupportedProvider = errors.New("unsupported llm provider")

type Provider string

const (
	ProviderOpenAI    Provider = "openai"
	ProviderAnthropic Provider = "anthropic"
	ProviderGrok      Provider = "grok"
)

type ProviderClientOptions struct {
	Registration ProviderVariantRegistration
	Model        string
	ConnectionID *config.ConnectionID

	Auth                         DispatchAuthProvider
	HTTPClient                   *http.Client
	ModelVerbosity               string
	ProviderIdentifier           *string
	Store                        bool
	ContextWindowTokens          int
	ProviderCapabilitiesOverride *ProviderCapabilities
	RequestCapabilities          *ProviderCapabilities
}

type ProviderClientFactory func(opts ProviderClientOptions) (Client, error)

type ProviderErrorReducerFactory func(providerID string) ProviderErrorReducer

type ProviderModelMatcher func(model string) bool

type ProviderTransportEndpoint struct {
	URL      *url.URL
	Explicit bool
}

type ProviderTransportVariantResolver func(endpoint ProviderTransportEndpoint, mode OpenAIAuthMode) (string, error)

type ProviderVariantContract struct {
	ProviderID               string
	BaseURL                  *string
	ResponsesPolicy          responsesPolicy
	TokenEstimator           TokenEstimator
	RequestCompression       httpcompression.RequestContentCoding
	Capabilities             ProviderCapabilities
	RemoteCompactionProtocol remoteCompactionProtocol
	NewErrorReducer          ProviderErrorReducerFactory
}

type remoteCompactionProtocol uint8

const (
	remoteCompactionUnsupported remoteCompactionProtocol = iota
	remoteCompactionResponsesTriggerV2
	remoteCompactionStandardResponses
)

type ProviderContract struct {
	Provider                Provider
	MatchModel              ProviderModelMatcher
	ResolveTransportVariant ProviderTransportVariantResolver
	NewClient               ProviderClientFactory
	ProviderVariants        []ProviderVariantContract
	ModelContracts          []ModelCapabilityContract
}

type ProviderVariantRegistration struct {
	Provider Provider
	Variant  ProviderVariantContract
}

type modelCapabilityRegistration struct {
	Provider Provider
	Contract ModelCapabilityContract
}

type providerRegistry struct {
	contractsByProvider  map[Provider]ProviderContract
	providerVariantsByID map[string]ProviderVariantRegistration
	modelContractsByName map[string]modelCapabilityRegistration
	modelContracts       []ModelCapabilityContract
	modelMatchers        []ProviderContract
}

var globalProviderRegistry = mustBuildProviderRegistry(providerContracts())

func providerContracts() []ProviderContract {
	return []ProviderContract{
		{
			Provider:  ProviderGrok,
			NewClient: newResponsesProviderClient,
			ProviderVariants: []ProviderVariantContract{
				grokVariant(config.ConnectionGrokCLIProxy, "https://cli-chat-proxy.grok.com/v1"),
				grokVariant(config.ConnectionGrokOAuthAPI, "https://api.x.ai/v1"),
				grokVariant(config.ConnectionGrokAPIKey, "https://api.x.ai/v1"),
			},
			ModelContracts: []ModelCapabilityContract{
				grokModelContract("grok-4.6", time.February, nil),
				grokModelContract("grok-4.7", time.May, &ModelMetadata{ContextWindowTokens: 256_000, LargeContextWindowTokens: textutil.Value(500_000)}),
			},
		},
		{
			Provider: ProviderAnthropic,
			MatchModel: func(model string) bool {
				return strings.HasPrefix(strings.ToLower(strings.TrimSpace(model)), "claude")
			},
			NewClient: newUnsupportedProviderClientFactory(ProviderAnthropic),
			ProviderVariants: []ProviderVariantContract{
				{
					ProviderID:         "anthropic",
					RequestCompression: httpcompression.ContentCodingIdentity,
					Capabilities: ProviderCapabilities{
						ProviderID:                    "anthropic",
						SupportsResponsesAPI:          false,
						SupportsResponsesCompact:      false,
						SupportsNativeWebSearch:       false,
						SupportsReasoningEncrypted:    false,
						SupportsServerSideContextEdit: false,
						SupportsProviderVerbosity:     false,
						IsOpenAIFirstParty:            false,
					},
					NewErrorReducer: newOpaqueProviderErrorReducer,
				},
			},
		},
		{
			Provider:                ProviderOpenAI,
			MatchModel:              matchOpenAIModelFamily,
			ResolveTransportVariant: resolveOpenAITransportProviderVariant,
			NewClient:               newResponsesProviderClient,
			ProviderVariants: []ProviderVariantContract{
				{
					ProviderID:               "openai",
					ResponsesPolicy:          openAIResponsesPolicy{},
					TokenEstimator:           OpenAITokenEstimator{},
					RequestCompression:       httpcompression.ContentCodingIdentity,
					RemoteCompactionProtocol: remoteCompactionResponsesTriggerV2,
					Capabilities: ProviderCapabilities{
						ProviderID:                    "openai",
						SupportsNativeThinkingUpdates: true,
						SupportsResponsesAPI:          true,
						SupportsFastMode:              true,
						SupportsResponsesCompact:      true,
						SupportsPromptCacheKey:        true,
						SupportsNativeWebSearch:       true,
						SupportsReasoningEncrypted:    true,
						SupportsServerSideContextEdit: true,
						SupportsProviderVerbosity:     true,
						IsOpenAIFirstParty:            true,
					},
					NewErrorReducer: newOpenAICompatibleErrorReducer,
				},
				{
					ProviderID:         "openai-compatible",
					ResponsesPolicy:    openAIResponsesPolicy{},
					RequestCompression: httpcompression.ContentCodingIdentity,
					Capabilities: ProviderCapabilities{
						ProviderID:                    "openai-compatible",
						SupportsResponsesAPI:          true,
						SupportsResponsesCompact:      false,
						SupportsPromptCacheKey:        false,
						SupportsNativeWebSearch:       false,
						SupportsReasoningEncrypted:    false,
						SupportsServerSideContextEdit: false,
						SupportsProviderVerbosity:     false,
						IsOpenAIFirstParty:            false,
					},
					NewErrorReducer: newOpenAICompatibleErrorReducer,
				},
				{
					ProviderID:               "chatgpt-codex",
					ResponsesPolicy:          openAIResponsesPolicy{},
					TokenEstimator:           OpenAITokenEstimator{},
					RequestCompression:       httpcompression.ContentCodingZstd,
					RemoteCompactionProtocol: remoteCompactionResponsesTriggerV2,
					Capabilities: ProviderCapabilities{
						ProviderID:                    "chatgpt-codex",
						SupportsNativeThinkingUpdates: true,
						SupportsResponsesAPI:          true,
						SupportsFastMode:              true,
						SupportsResponsesCompact:      true,
						SupportsPromptCacheKey:        true,
						SupportsNativeWebSearch:       true,
						SupportsReasoningEncrypted:    true,
						SupportsServerSideContextEdit: true,
						SupportsProviderVerbosity:     true,
						IsOpenAIFirstParty:            true,
					},
					NewErrorReducer: newOpenAICompatibleErrorReducer,
				},
			},
			ModelContracts: []ModelCapabilityContract{
				gpt6ModelContract("gpt-6-astra", time.April, []string{"low", "medium", "high", "xhigh", "max"}),
				gpt6ModelContract("gpt-6.1-sol", time.April, []string{"low", "medium", "high", "xhigh", "max"}),
				gpt6ModelContract("gpt-6-sol", time.April, []string{"none", "low", "medium", "high", "xhigh", "max"}),
				gpt6ModelContract("gpt-6-luna", time.May, []string{"none", "low", "medium", "high", "xhigh", "max"}),
				{Model: "gpt-5.6-sol", ContextWindowTokens: 372_000, LargeContextWindowTokens: textutil.Value(372_000), KnowledgeCutoff: &ModelKnowledgeCutoff{Month: time.February, Year: 2026}, SupportsReasoningEffort: true, SupportedReasoningEfforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, SupportsReasoningSummary: true, SupportsVerbosity: true, SupportedVerbosityLevels: []string{"low", "medium", "high"}, SupportsVisionInputs: true},
				{Model: "gpt-5.6-terra", ContextWindowTokens: 372_000, LargeContextWindowTokens: textutil.Value(372_000), KnowledgeCutoff: &ModelKnowledgeCutoff{Month: time.February, Year: 2026}, SupportsReasoningEffort: true, SupportedReasoningEfforts: []string{"low", "medium", "high", "xhigh", "max", "ultra"}, SupportsReasoningSummary: true, SupportsVerbosity: true, SupportedVerbosityLevels: []string{"low", "medium", "high"}, SupportsVisionInputs: true},
				{Model: "gpt-5.6-luna", ContextWindowTokens: 372_000, LargeContextWindowTokens: textutil.Value(372_000), KnowledgeCutoff: &ModelKnowledgeCutoff{Month: time.February, Year: 2026}, SupportsReasoningEffort: true, SupportedReasoningEfforts: []string{"low", "medium", "high", "xhigh", "max"}, SupportsReasoningSummary: true, SupportsVerbosity: true, SupportedVerbosityLevels: []string{"low", "medium", "high"}, SupportsVisionInputs: true},
			},
		},
	}
}

func grokVariant(protocol config.ConnectionProtocol, endpoint string) ProviderVariantContract {
	id := string(protocol)
	compaction := remoteCompactionStandardResponses
	if protocol == config.ConnectionGrokCLIProxy {
		compaction = remoteCompactionUnsupported
	}
	return ProviderVariantContract{
		ProviderID: id, BaseURL: textutil.Value(endpoint),
		ResponsesPolicy:          grokResponsesPolicy{},
		TokenEstimator:           GrokTokenEstimator{},
		RequestCompression:       httpcompression.ContentCodingIdentity,
		RemoteCompactionProtocol: compaction,
		Capabilities: ProviderCapabilities{
			ProviderID: id, SupportsResponsesAPI: true, SupportsReasoningEncrypted: true,
			SupportsPromptCacheKey: true, SupportsFastMode: true,
			SupportsResponsesCompact: compaction != remoteCompactionUnsupported, SupportsNativeWebSearch: true,
		},
		NewErrorReducer: newGrokErrorReducer,
	}
}

func grokModelContract(model string, cutoff time.Month, proxyContext *ModelMetadata) ModelCapabilityContract {
	return ModelCapabilityContract{
		Model: model, ContextWindowTokens: 500_000,
		VariantContexts:         map[string]*ModelMetadata{string(config.ConnectionGrokCLIProxy): proxyContext},
		KnowledgeCutoff:         &ModelKnowledgeCutoff{Month: cutoff, Year: 2026},
		SupportsReasoningEffort: true, SupportedReasoningEfforts: []string{"low", "medium", "high", "xhigh"},
		SupportsReasoningSummary: true, SupportsVisionInputs: true,
	}
}

func gpt6ModelContract(model string, cutoff time.Month, efforts []string) ModelCapabilityContract {
	return ModelCapabilityContract{
		Model:                         model,
		ContextWindowTokens:           272_000,
		LargeContextWindowTokens:      textutil.Value(1_050_000),
		VariantContexts:               map[string]*ModelMetadata{"chatgpt-codex": {ContextWindowTokens: 272_000, LargeContextWindowTokens: textutil.Value(872_000)}},
		KnowledgeCutoff:               &ModelKnowledgeCutoff{Month: cutoff, Year: 2026},
		SupportsReasoningEffort:       true,
		SupportsNativeThinkingUpdates: true,
		SupportedReasoningEfforts:     efforts,
		SupportsReasoningSummary:      true,
		SupportsVerbosity:             true,
		SupportedVerbosityLevels:      []string{"low", "medium", "high"},
		SupportsVisionInputs:          true,
	}
}

func mustBuildProviderRegistry(contracts []ProviderContract) providerRegistry {
	registry := providerRegistry{
		contractsByProvider:  make(map[Provider]ProviderContract, len(contracts)),
		providerVariantsByID: make(map[string]ProviderVariantRegistration),
		modelContractsByName: make(map[string]modelCapabilityRegistration),
		modelMatchers:        make([]ProviderContract, 0, len(contracts)),
	}

	for _, contract := range contracts {
		if contract.Provider == "" {
			panic("provider contract missing provider key")
		}
		if contract.NewClient == nil {
			panic(fmt.Sprintf("provider %q missing client factory", contract.Provider))
		}
		if len(contract.ProviderVariants) == 0 {
			panic(fmt.Sprintf("provider %q missing provider variants", contract.Provider))
		}
		if _, exists := registry.contractsByProvider[contract.Provider]; exists {
			panic(fmt.Sprintf("duplicate provider contract for %q", contract.Provider))
		}
		registry.contractsByProvider[contract.Provider] = contract
		if contract.MatchModel != nil {
			registry.modelMatchers = append(registry.modelMatchers, contract)
		}

		for _, variant := range contract.ProviderVariants {
			normalizedID := strings.ToLower(strings.TrimSpace(variant.ProviderID))
			if normalizedID == "" {
				panic(fmt.Sprintf("provider %q has empty provider_id variant", contract.Provider))
			}
			if variant.NewErrorReducer == nil {
				panic(fmt.Sprintf("provider %q missing reducer factory for provider_id %q", contract.Provider, normalizedID))
			}
			if strings.TrimSpace(variant.Capabilities.ProviderID) == "" {
				variant.Capabilities.ProviderID = normalizedID
			}
			if strings.ToLower(strings.TrimSpace(variant.Capabilities.ProviderID)) != normalizedID {
				panic(fmt.Sprintf("provider %q capabilities provider_id %q does not match variant key %q", contract.Provider, variant.Capabilities.ProviderID, normalizedID))
			}
			if _, exists := registry.providerVariantsByID[normalizedID]; exists {
				panic(fmt.Sprintf("duplicate provider variant registration for provider_id %q", normalizedID))
			}
			registry.providerVariantsByID[normalizedID] = ProviderVariantRegistration{Provider: contract.Provider, Variant: variant}
		}

		for _, modelContract := range contract.ModelContracts {
			normalizedModel := strings.ToLower(strings.TrimSpace(modelContract.Model))
			if normalizedModel == "" {
				panic(fmt.Sprintf("provider %q has empty model contract key", contract.Provider))
			}
			if _, exists := registry.modelContractsByName[normalizedModel]; exists {
				panic(fmt.Sprintf("duplicate model contract registration for %q", normalizedModel))
			}
			registry.modelContractsByName[normalizedModel] = modelCapabilityRegistration{Provider: contract.Provider, Contract: modelContract}
			registry.modelContracts = append(registry.modelContracts, modelContract)
		}
	}

	return registry
}

func newUnsupportedProviderClientFactory(provider Provider) ProviderClientFactory {
	return func(_ ProviderClientOptions) (Client, error) {
		return nil, fmt.Errorf("%w: %s (not implemented)", ErrUnsupportedProvider, provider)
	}
}

func newResponsesProviderClient(opts ProviderClientOptions) (Client, error) {
	if opts.Auth == nil {
		return nil, fmt.Errorf("Responses auth provider is required")
	}
	transport, err := newResponsesHTTPTransport(opts)
	if err != nil {
		return nil, err
	}
	return newIdleWatchdogClient(NewResponsesClient(transport), transport.Client.Timeout), nil
}

func newResponsesHTTPTransport(opts ProviderClientOptions) (*HTTPTransport, error) {
	transport, err := NewHTTPTransport(opts.Auth, opts.Registration)
	if err != nil {
		return nil, err
	}
	transport.ConnectionID = opts.ConnectionID
	if opts.HTTPClient != nil {
		transport.Client = opts.HTTPClient
	}
	if opts.HTTPClient == nil {
		transport.Client = NewProviderHTTPClient(transport.serviceBaseURL(), transport.Client.Timeout)
	}
	transport.ModelVerbosity = strings.ToLower(strings.TrimSpace(opts.ModelVerbosity))
	if opts.ProviderIdentifier != nil {
		transport.ProviderIdentifier = *opts.ProviderIdentifier
	}
	if opts.ContextWindowTokens > 0 {
		transport.ContextWindowTokens = opts.ContextWindowTokens
	}
	if opts.ProviderCapabilitiesOverride != nil {
		caps := *opts.ProviderCapabilitiesOverride
		transport.ProviderCapabilitiesOverride = &caps
	}
	if opts.RequestCapabilities != nil {
		caps := *opts.RequestCapabilities
		transport.RequestCapabilities = &caps
	}
	transport.Store = opts.Store
	return transport, nil
}

func NewProviderClient(opts ProviderClientOptions) (Client, error) {
	if opts.Registration.Provider == "" || opts.Registration.Variant.ProviderID == "" {
		return nil, fmt.Errorf("%w: resolved provider registration is required", ErrUnsupportedProvider)
	}
	provider := opts.Registration.Provider
	if opts.ContextWindowTokens <= 0 {
		if model, known := LookupModelCapabilityContract(opts.Model); known {
			if meta := model.ContextMetadata(opts.Registration.Variant.ProviderID); meta != nil {
				opts.ContextWindowTokens = meta.ContextWindowTokens
			}
		}
	}
	contract, ok := globalProviderRegistry.contractsByProvider[provider]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedProvider, provider)
	}
	return contract.NewClient(opts)
}

func InferProviderFromModel(model string) (Provider, error) {
	normalizedModel := strings.TrimSpace(model)
	if normalizedModel == "" {
		return "", fmt.Errorf("%w: model is required to infer provider", ErrUnsupportedProvider)
	}
	if model, present := globalProviderRegistry.modelContractsByName[strings.ToLower(normalizedModel)]; present {
		return model.Provider, nil
	}
	for _, contract := range globalProviderRegistry.modelMatchers {
		if contract.MatchModel(normalizedModel) {
			return contract.Provider, nil
		}
	}
	return "", fmt.Errorf("%w: no provider contract matches model %q", ErrUnsupportedProvider, normalizedModel)
}

func matchOpenAIModelFamily(model string) bool {
	normalizedModel := strings.ToLower(strings.TrimSpace(model))
	if normalizedModel == "" {
		return false
	}
	if strings.HasPrefix(normalizedModel, "gpt-") {
		return true
	}
	return false
}
