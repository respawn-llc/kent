package authpolicy

import (
	"strings"

	"core/server/llm"
	"core/shared/config"
)

func RequiresStartupAuth(settings config.Settings) bool {
	if baseURL := strings.TrimSpace(settings.OpenAIBaseURL); baseURL != "" {
		if llm.IsOpenAIFirstPartyBaseURL(baseURL) {
			return true
		}
		return false
	}
	if provider := strings.ToLower(strings.TrimSpace(settings.ProviderOverride)); provider != "" {
		return provider == string(llm.ProviderOpenAI)
	}
	provider, err := llm.InferProviderFromModel(settings.Model)
	if err != nil {
		return false
	}
	return provider == llm.ProviderOpenAI
}
