package onboardingmodel

import (
	"builder/server/auth"
	"builder/server/llm"
	"builder/shared/config"
)

type ProviderCapabilities = llm.ProviderCapabilities
type ModelMetadata = llm.ModelMetadata

func ProviderCapabilitiesForSettings(authState auth.State, settings config.Settings) (ProviderCapabilities, error) {
	return llm.ProviderCapabilitiesForSettings(authState, settings)
}

func LookupModelMetadata(model string) (ModelMetadata, bool) {
	return llm.LookupModelMetadata(model)
}

func SupportsLargeContextWindowModel(model string) bool {
	return llm.SupportsLargeContextWindowModel(model)
}

func SupportsReasoningEffortModel(model string) bool {
	return llm.SupportsReasoningEffortModel(model)
}

func SupportedThinkingLevelsModel(model string) []string {
	return llm.SupportedThinkingLevelsModel(model)
}

func SupportsVerbosityModel(model string) bool {
	return llm.SupportsVerbosityModel(model)
}

func SupportedVerbosityLevelsModel(model string) []string {
	return llm.SupportedVerbosityLevelsModel(model)
}

func ApplyDerivedModelContextBudget(settings *config.Settings, model string, baselineWindow int, baselineThreshold int) {
	llm.ApplyDerivedModelContextBudget(settings, model, baselineWindow, baselineThreshold)
}
