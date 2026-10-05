package llm

import (
	"fmt"
	"strings"
)

func InferProviderCapabilities(providerID string) (ProviderCapabilities, error) {
	normalizedID := strings.TrimSpace(providerID)
	contract, ok := LookupProviderCapabilityContract(normalizedID)
	if !ok {
		return ProviderCapabilities{}, fmt.Errorf("%w: unknown provider_id %q", ErrUnsupportedProvider, normalizedID)
	}
	return contract, nil
}

func SupportsFastModeProvider(caps ProviderCapabilities) bool {
	return caps.SupportsFastMode
}

func SupportsPromptCacheKeyProvider(caps ProviderCapabilities) bool {
	return caps.SupportsResponsesAPI && caps.SupportsPromptCacheKey
}
