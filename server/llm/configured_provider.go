package llm

import (
	"strings"

	"core/server/auth"
	"core/shared/config"
)

func ProviderCapabilitiesForSettings(authState auth.State, settings config.Settings) (ProviderCapabilities, error) {
	if caps, ok := ProviderCapabilitiesFromOverride(settings.ProviderCapabilities); ok {
		return caps, nil
	}
	providerID := strings.TrimSpace(settings.ProviderOverride)
	if providerID == "" {
		switch authState.Method.Type {
		case auth.MethodOAuth:
			providerID = "chatgpt-codex"
		case auth.MethodAPIKey, auth.MethodNone:
			if strings.TrimSpace(settings.OpenAIBaseURL) != "" {
				if IsOpenAIFirstPartyBaseURL(settings.OpenAIBaseURL) {
					providerID = "openai"
				} else {
					providerID = "openai-compatible"
				}
			} else {
				providerID = "openai"
			}
		default:
			providerID = "openai"
		}
	}
	return InferProviderCapabilities(providerID)
}
