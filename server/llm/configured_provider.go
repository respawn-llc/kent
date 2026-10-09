package llm

import (
	"fmt"
	"net/url"
	"strings"

	"core/server/session"
	"core/shared/config"
)

// ResolveEffectiveProviderCapabilities preserves the historical request
// contract independently of the selected connection's actual transport.
func ResolveEffectiveProviderCapabilities(locked *session.LockedContract, settings config.Settings) (ProviderCapabilities, error) {
	actual, err := ResolveRuntimeProviderCapabilities(settings)
	if err != nil {
		return ProviderCapabilities{}, err
	}
	effective := actual
	if contract, present := ProviderCapabilitiesFromLocked(locked); present {
		effective = contract
		effective.SupportsNativeThinkingUpdates = actual.SupportsNativeThinkingUpdates
		effective.SupportsFastMode = actual.SupportsFastMode
	}
	return effective, nil
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
	variant := selected.Variant
	caps := variant.Capabilities
	if override, ok := ProviderCapabilitiesFromOverride(connection.Capabilities); ok {
		caps = override
		caps.SupportsNativeThinkingUpdates = variant.Capabilities.SupportsNativeThinkingUpdates
	}
	return caps, nil
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
	variant, err := resolveRuntimeTransportVariant(ProviderOpenAI, endpoint, OpenAIAuthMode{IsOAuth: connection.Protocol == config.ConnectionChatGPT})
	if err != nil {
		return ProviderVariantRegistration{}, err
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

func resolveRuntimeTransportVariant(provider Provider, endpoint ProviderTransportEndpoint, mode OpenAIAuthMode) (ProviderVariantContract, error) {
	if variant, err := resolveProviderTransportVariant(provider, endpoint, mode); err == nil {
		return variant, nil
	} else if provider == ProviderOpenAI {
		return ProviderVariantContract{}, err
	}
	providerID := strings.TrimSpace(string(provider))
	registration, ok := lookupProviderVariantContract(providerID)
	if !ok {
		return ProviderVariantContract{}, fmt.Errorf("%w: %s", ErrUnsupportedProvider, providerID)
	}
	if registration.Provider != provider {
		return ProviderVariantContract{}, fmt.Errorf("provider %q maps to provider_id %q owned by %q", provider, providerID, registration.Provider)
	}
	return registration.Variant, nil
}
