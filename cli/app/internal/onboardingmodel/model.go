package onboardingmodel

import (
	"builder/cli/app/internal/serverbridge"
	"builder/shared/auth"
	"builder/shared/config"
	"builder/shared/modelcontract"
)

type ProviderCapabilities = modelcontract.ProviderCapabilities
type ModelMetadata = modelcontract.ModelMetadata

func ProviderCapabilitiesForSettings(authState auth.State, settings config.Settings) (ProviderCapabilities, error) {
	return serverbridge.ProviderCapabilitiesForSettings(authState, settings)
}

func LookupModelMetadata(model string) (ModelMetadata, bool) {
	return serverbridge.LookupModelMetadata(model)
}

func SupportsLargeContextWindowModel(model string) bool {
	return serverbridge.SupportsLargeContextWindowModel(model)
}

func SupportsReasoningEffortModel(model string) bool {
	return serverbridge.SupportsReasoningEffortModel(model)
}

func SupportedThinkingLevelsModel(model string) []string {
	return serverbridge.SupportedThinkingLevelsModel(model)
}

func SupportsVerbosityModel(model string) bool {
	return serverbridge.SupportsVerbosityModel(model)
}

func SupportedVerbosityLevelsModel(model string) []string {
	return serverbridge.SupportedVerbosityLevelsModel(model)
}

func ApplyDerivedModelContextBudget(settings *config.Settings, model string, baselineWindow int, baselineThreshold int) {
	serverbridge.ApplyDerivedModelContextBudget(settings, model, baselineWindow, baselineThreshold)
}
