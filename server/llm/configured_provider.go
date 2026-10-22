package llm

import (
	"fmt"
	"net/url"
	"strings"

	"core/server/session"
	"core/shared/config"
	"core/shared/textutil"
)

// ResolveEffectiveProviderCapabilities preserves the historical request
// contract independently of the selected connection's actual transport.
func ResolveEffectiveProviderCapabilities(locked *session.LockedContract, settings config.Settings) (ProviderCapabilities, error) {
	actual, err := ResolveRuntimeProviderCapabilities(settings)
	if err != nil {
		return ProviderCapabilities{}, err
	}
	contract, present := ProviderCapabilitiesFromLocked(locked)
	if !present {
		return actual, nil
	}
	return EffectiveProviderCapabilities(&contract, actual), nil
}

func EffectiveProviderCapabilities(contract *ProviderCapabilities, actual ProviderCapabilities) ProviderCapabilities {
	effective := actual
	if contract != nil {
		effective = *contract
		effective.SupportsNativeThinkingUpdates = actual.SupportsNativeThinkingUpdates
		effective.SupportsFastMode = actual.SupportsFastMode
		effective.ReasoningType = actual.ReasoningType
	}
	return effective
}

func ResolveRuntimeProviderCapabilities(settings config.Settings) (ProviderCapabilities, error) {
	connection, err := settings.SelectedConnection()
	if err != nil {
		return ProviderCapabilities{}, err
	}
	return ResolveConnectionCapabilities(connection)
}

func ResolveConnectionCapabilities(connection config.ProviderConnection) (ProviderCapabilities, error) {
	selected, err := ResolveConnectionVariant(connection)
	if err != nil {
		return ProviderCapabilities{}, err
	}
	if override, ok := ProviderCapabilitiesFromOverride(connection.Capabilities); ok {
		return selected.Variant.capabilitiesWithOverride(&override), nil
	}
	return selected.Variant.capabilitiesWithOverride(nil), nil
}

func (variant ProviderVariantContract) capabilitiesWithOverride(override *ProviderCapabilities) ProviderCapabilities {
	caps := variant.Capabilities
	if override != nil {
		caps = *override
	}
	caps.SupportsNativeThinkingUpdates = variant.Capabilities.SupportsNativeThinkingUpdates
	caps.ReasoningType = textutil.Pointer(variant.ReasoningType)
	return caps
}

func ResolveConnectionTokenEstimator(connection config.ProviderConnection) (TokenEstimator, error) {
	selected, err := ResolveConnectionVariant(connection)
	if err != nil {
		return nil, err
	}
	variant := selected.Variant
	if variant.TokenEstimator != nil {
		return variant.TokenEstimator, nil
	}
	return DefaultTokenEstimator{}, nil
}

func ResolveConnectionVariant(connection config.ProviderConnection) (ProviderVariantRegistration, error) {
	if err := connection.Validate(); err != nil {
		return ProviderVariantRegistration{}, err
	}
	switch connection.Protocol {
	case config.ConnectionGrokCLIProxy, config.ConnectionGrokOAuthAPI, config.ConnectionGrokAPIKey:
		registration, ok := lookupProviderVariantContract(string(connection.Protocol))
		if !ok {
			return ProviderVariantRegistration{}, fmt.Errorf("unregistered connection protocol %q", connection.Protocol)
		}
		return registration, nil
	}
	rawURL := ""
	if connection.Endpoint != nil {
		rawURL = *connection.Endpoint
	}
	endpoint, err := newProviderTransportEndpoint(rawURL, connection.Endpoint != nil)
	if err != nil {
		return ProviderVariantRegistration{}, err
	}
	variant, err := resolveProviderTransportVariant(ProviderOpenAI, endpoint, OpenAIAuthMode{IsOAuth: connection.Protocol == config.ConnectionChatGPT})
	if err != nil {
		return ProviderVariantRegistration{}, err
	}
	if connection.Protocol == config.ConnectionChatGPT {
		variant.BaseURL = textutil.Value(strings.TrimSuffix(codexResponsesEndpoint, "/responses"))
	} else {
		variant.BaseURL = textutil.Value(normalizeOpenAIBaseURL(endpoint.URL))
	}
	return ProviderVariantRegistration{Provider: ProviderOpenAI, Variant: variant}, nil
}

func newProviderTransportEndpoint(rawURL string, explicit bool) (ProviderTransportEndpoint, error) {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		if explicit {
			return ProviderTransportEndpoint{}, fmt.Errorf("explicit provider endpoint URL is empty")
		}
		return ProviderTransportEndpoint{Explicit: false}, nil
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		return ProviderTransportEndpoint{}, fmt.Errorf("parse provider endpoint URL: %w", err)
	}
	return ProviderTransportEndpoint{URL: parsed, Explicit: explicit}, nil
}
