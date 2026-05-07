package config

import (
	"fmt"
	"strconv"
	"strings"

	"builder/shared/toolspec"
)

func inheritReviewerDefaults(settings *Settings) {
	inheritReviewerDefaultsWithSources(settings, nil)
}

func EffectiveReviewerSettings(settings Settings) ReviewerSettings {
	return settings.Reviewer
}

func inheritReviewerDefaultsWithSources(settings *Settings, sources map[string]string) {
	reviewerProviderSelectionExplicit := reviewerUsesIndependentProviderSelection(*settings)
	if strings.TrimSpace(settings.Reviewer.Model) == "" {
		settings.Reviewer.Model = settings.Model
	}
	if strings.TrimSpace(settings.Reviewer.ThinkingLevel) == "" {
		settings.Reviewer.ThinkingLevel = settings.ThinkingLevel
	}
	if strings.TrimSpace(string(settings.Reviewer.ModelVerbosity)) == "" {
		settings.Reviewer.ModelVerbosity = settings.ModelVerbosity
	}
	reviewerProvider := ResolveReviewerProviderSettings(*settings)
	settings.Reviewer.ProviderOverride = reviewerProvider.ProviderOverride
	settings.Reviewer.OpenAIBaseURL = reviewerProvider.OpenAIBaseURL
	inheritReviewerModelCapabilities(settings, sources)
	inheritReviewerProviderCapabilities(settings, sources, reviewerProviderSelectionExplicit)
	if settings.Reviewer.ModelContextWindow == 0 {
		settings.Reviewer.ModelContextWindow = settings.ModelContextWindow
	}
}

func reviewerUsesIndependentProviderSelection(settings Settings) bool {
	if strings.TrimSpace(settings.Reviewer.OpenAIBaseURL) != "" {
		return true
	}
	reviewerProvider := normalizeProviderOverride(settings.Reviewer.ProviderOverride)
	if reviewerProvider == "" {
		return false
	}
	mainProvider := normalizeProviderOverride(settings.ProviderOverride)
	if mainProvider == "" && reviewerProvider == "openai" {
		return false
	}
	return reviewerProvider != mainProvider
}

func ResolveReviewerProviderSettings(settings Settings) ReviewerProviderSettings {
	provider := strings.TrimSpace(settings.Reviewer.ProviderOverride)
	if provider == "" {
		provider = strings.TrimSpace(settings.ProviderOverride)
	}
	baseURL := strings.TrimSpace(settings.Reviewer.OpenAIBaseURL)
	if baseURL == "" && shouldInheritMainOpenAIBaseURL(provider) {
		baseURL = strings.TrimSpace(settings.OpenAIBaseURL)
	}
	return ReviewerProviderSettings{ProviderOverride: provider, OpenAIBaseURL: baseURL}
}

func shouldInheritMainOpenAIBaseURL(reviewerProvider string) bool {
	switch normalizeProviderOverride(reviewerProvider) {
	case "", "openai":
		return true
	default:
		return false
	}
}

func hasModelCapabilitiesOverride(override ModelCapabilitiesOverride) bool {
	return override.SupportsReasoningEffort || override.SupportsVisionInputs
}

func shouldInheritReviewerModelCapabilities(override ModelCapabilitiesOverride, sources map[string]string) bool {
	if sources == nil {
		return !hasModelCapabilitiesOverride(override)
	}
	return !hasConfiguredSource(sources, "reviewer.model_capabilities.supports_reasoning_effort") &&
		!hasConfiguredSource(sources, "reviewer.model_capabilities.supports_vision_inputs")
}

func inheritReviewerModelCapabilities(settings *Settings, sources map[string]string) {
	if sources == nil {
		if !hasModelCapabilitiesOverride(settings.Reviewer.ModelCapabilities) {
			settings.Reviewer.ModelCapabilities = settings.ModelCapabilities
		}
		return
	}
	if !hasConfiguredSource(sources, "reviewer.model_capabilities.supports_reasoning_effort") {
		settings.Reviewer.ModelCapabilities.SupportsReasoningEffort = settings.ModelCapabilities.SupportsReasoningEffort
	}
	if !hasConfiguredSource(sources, "reviewer.model_capabilities.supports_vision_inputs") {
		settings.Reviewer.ModelCapabilities.SupportsVisionInputs = settings.ModelCapabilities.SupportsVisionInputs
	}
}

func hasProviderCapabilitiesOverride(override ProviderCapabilitiesOverride) bool {
	return strings.TrimSpace(override.ProviderID) != "" ||
		override.SupportsResponsesAPI ||
		override.SupportsResponsesCompact ||
		override.SupportsRequestInputTokenCount ||
		override.SupportsPromptCacheKey ||
		override.SupportsNativeWebSearch ||
		override.SupportsReasoningEncrypted ||
		override.SupportsServerSideContextEdit ||
		override.IsOpenAIFirstParty
}

func inheritReviewerProviderCapabilities(settings *Settings, sources map[string]string, reviewerProviderSelectionExplicit bool) {
	if sources == nil {
		if !hasProviderCapabilitiesOverride(settings.Reviewer.ProviderCapabilities) && !reviewerProviderSelectionExplicit {
			settings.Reviewer.ProviderCapabilities = settings.ProviderCapabilities
		}
		return
	}
	if !hasAnyConfiguredSource(sources, reviewerProviderCapabilityKeys...) {
		if !reviewerProviderSelectionExplicit {
			settings.Reviewer.ProviderCapabilities = settings.ProviderCapabilities
		}
		return
	}
	if reviewerProviderSelectionExplicit {
		return
	}
	if !hasConfiguredSource(sources, "reviewer.provider_capabilities.provider_id") {
		settings.Reviewer.ProviderCapabilities.ProviderID = settings.ProviderCapabilities.ProviderID
	}
	if !hasConfiguredSource(sources, "reviewer.provider_capabilities.supports_responses_api") {
		settings.Reviewer.ProviderCapabilities.SupportsResponsesAPI = settings.ProviderCapabilities.SupportsResponsesAPI
	}
	if !hasConfiguredSource(sources, "reviewer.provider_capabilities.supports_responses_compact") {
		settings.Reviewer.ProviderCapabilities.SupportsResponsesCompact = settings.ProviderCapabilities.SupportsResponsesCompact
	}
	if !hasConfiguredSource(sources, "reviewer.provider_capabilities.supports_request_input_token_count") {
		settings.Reviewer.ProviderCapabilities.SupportsRequestInputTokenCount = settings.ProviderCapabilities.SupportsRequestInputTokenCount
	}
	if !hasConfiguredSource(sources, "reviewer.provider_capabilities.supports_prompt_cache_key") {
		settings.Reviewer.ProviderCapabilities.SupportsPromptCacheKey = settings.ProviderCapabilities.SupportsPromptCacheKey
	}
	if !hasConfiguredSource(sources, "reviewer.provider_capabilities.supports_native_web_search") {
		settings.Reviewer.ProviderCapabilities.SupportsNativeWebSearch = settings.ProviderCapabilities.SupportsNativeWebSearch
	}
	if !hasConfiguredSource(sources, "reviewer.provider_capabilities.supports_reasoning_encrypted") {
		settings.Reviewer.ProviderCapabilities.SupportsReasoningEncrypted = settings.ProviderCapabilities.SupportsReasoningEncrypted
	}
	if !hasConfiguredSource(sources, "reviewer.provider_capabilities.supports_server_side_context_edit") {
		settings.Reviewer.ProviderCapabilities.SupportsServerSideContextEdit = settings.ProviderCapabilities.SupportsServerSideContextEdit
	}
	if !hasConfiguredSource(sources, "reviewer.provider_capabilities.is_openai_first_party") {
		settings.Reviewer.ProviderCapabilities.IsOpenAIFirstParty = settings.ProviderCapabilities.IsOpenAIFirstParty
	}
}

var reviewerProviderCapabilityKeys = []string{
	"reviewer.provider_capabilities.provider_id",
	"reviewer.provider_capabilities.supports_responses_api",
	"reviewer.provider_capabilities.supports_responses_compact",
	"reviewer.provider_capabilities.supports_request_input_token_count",
	"reviewer.provider_capabilities.supports_prompt_cache_key",
	"reviewer.provider_capabilities.supports_native_web_search",
	"reviewer.provider_capabilities.supports_reasoning_encrypted",
	"reviewer.provider_capabilities.supports_server_side_context_edit",
	"reviewer.provider_capabilities.is_openai_first_party",
}

func hasAnyConfiguredSource(sources map[string]string, keys ...string) bool {
	for _, key := range keys {
		if hasConfiguredSource(sources, key) {
			return true
		}
	}
	return false
}

func NormalizeSettingsForPersistence(settings Settings) (Settings, error) {
	return NormalizeSettingsForPersistenceWithSources(settings, nil)
}

func NormalizeSettingsForPersistenceWithSources(settings Settings, sources map[string]string) (Settings, error) {
	normalized := settings
	if normalized.EnabledTools == nil {
		normalized.EnabledTools = defaultEnabledToolMap()
	}
	if normalized.SkillToggles == nil {
		normalized.SkillToggles = map[string]bool{}
	}
	effectiveSources := cloneSourceMapOrDefault(sources)
	inheritReviewerDefaultsWithSources(&normalized, effectiveSources)
	if err := validateSettings(normalized, effectiveSources); err != nil {
		return Settings{}, err
	}
	return normalized, nil
}

func cloneSourceMapOrDefault(sources map[string]string) map[string]string {
	if len(sources) == 0 {
		out := configRegistry.defaultSourceMap()
		out["model"] = "file"
		return out
	}
	out := make(map[string]string, len(sources)+1)
	for key, value := range sources {
		out[key] = value
	}
	if strings.TrimSpace(out["model"]) == "" {
		out["model"] = "file"
	}
	return out
}

func ValidateSettingsWithSources(settings Settings, sources map[string]string) error {
	return validateSettings(settings, sources)
}

func parseEnabledToolsCSV(raw string) ([]toolspec.ID, error) {
	parts := strings.Split(raw, ",")
	seen := map[toolspec.ID]bool{}
	out := make([]toolspec.ID, 0, len(parts))
	for _, part := range parts {
		name := strings.TrimSpace(part)
		if name == "" {
			continue
		}
		id, ok := toolspec.ParseConfigID(name)
		if !ok {
			return nil, fmt.Errorf("unknown tool %q", name)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

func resetEnabledToolMap(enabled []toolspec.ID) map[toolspec.ID]bool {
	out := make(map[toolspec.ID]bool, len(toolspec.CatalogIDs()))
	for _, id := range toolspec.CatalogIDs() {
		out[id] = false
	}
	for _, id := range enabled {
		out[id] = true
	}
	return out
}

func DisabledSkillToggles(settings Settings) map[string]bool {
	if len(settings.SkillToggles) == 0 {
		return nil
	}
	disabled := make(map[string]bool, len(settings.SkillToggles))
	for name, enabled := range settings.SkillToggles {
		if enabled {
			continue
		}
		normalized := normalizeSkillToggleKey(name)
		if normalized == "" {
			continue
		}
		disabled[normalized] = true
	}
	if len(disabled) == 0 {
		return nil
	}
	return disabled
}

func parseBoolString(raw string, envName string) (*bool, error) {
	parsed, err := strconv.ParseBool(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid %s: %q", envName, raw)
	}
	return &parsed, nil
}

func parsePositiveIntString(raw string, envName string) (*int, error) {
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return nil, fmt.Errorf("invalid %s: %q", envName, raw)
	}
	return &parsed, nil
}
