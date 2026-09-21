package config

import (
	"fmt"
	"strconv"
	"strings"

	"core/shared/toolspec"
)

func InheritReviewerSettings(settings *Settings, sources map[string]Origin) {
	if settings.Reviewer.Connection == nil || sources["reviewer.connection"].Inherited("reviewer.connection") {
		settings.Reviewer.Connection = settings.Connection
		inheritSource(sources, "reviewer.connection", "connection")
	}
	if sources["reviewer.model"].Inherited("reviewer.model") || strings.TrimSpace(settings.Reviewer.Model) == "" {
		settings.Reviewer.Model = settings.Model
		inheritSource(sources, "reviewer.model", "model")
	}
	if sources["reviewer.thinking_level"].Inherited("reviewer.thinking_level") ||
		(strings.TrimSpace(settings.Reviewer.ThinkingLevel) == "" && !hasConfiguredSource(sources, "reviewer.thinking_level")) {
		settings.Reviewer.ThinkingLevel = settings.ThinkingLevel
		inheritSource(sources, "reviewer.thinking_level", "thinking_level")
	}
	if sources["reviewer.model_verbosity"].Inherited("reviewer.model_verbosity") || strings.TrimSpace(string(settings.Reviewer.ModelVerbosity)) == "" {
		settings.Reviewer.ModelVerbosity = settings.ModelVerbosity
		inheritSource(sources, "reviewer.model_verbosity", "model_verbosity")
	}
	inheritReviewerModelCapabilities(settings, sources)
	if sources["reviewer.model_context_window"].Inherited("reviewer.model_context_window") ||
		(settings.Reviewer.ModelContextWindow == 0 && !hasConfiguredSource(sources, "reviewer.model_context_window")) {
		settings.Reviewer.ModelContextWindow = settings.ModelContextWindow
		inheritSource(sources, "reviewer.model_context_window", "model_context_window")
	}
}

func inheritReviewerModelCapabilities(settings *Settings, sources map[string]Origin) {
	if sources == nil {
		if !settings.Reviewer.ModelCapabilities.SupportsReasoningEffort && !settings.Reviewer.ModelCapabilities.SupportsVisionInputs {
			settings.Reviewer.ModelCapabilities = settings.ModelCapabilities
		}
		return
	}
	if !hasConfiguredSource(sources, "reviewer.model_capabilities.supports_reasoning_effort") {
		settings.Reviewer.ModelCapabilities.SupportsReasoningEffort = settings.ModelCapabilities.SupportsReasoningEffort
		inheritSource(sources, "reviewer.model_capabilities.supports_reasoning_effort", "model_capabilities.supports_reasoning_effort")
	}
	if !hasConfiguredSource(sources, "reviewer.model_capabilities.supports_vision_inputs") {
		settings.Reviewer.ModelCapabilities.SupportsVisionInputs = settings.ModelCapabilities.SupportsVisionInputs
		inheritSource(sources, "reviewer.model_capabilities.supports_vision_inputs", "model_capabilities.supports_vision_inputs")
	}
}

var modelCapabilityKeys = []string{
	"model_capabilities.supports_reasoning_effort",
	"model_capabilities.supports_vision_inputs",
}

var providerCapabilityKeys = []string{
	"provider_capabilities.provider_id",
	"provider_capabilities.supports_responses_api",
	"provider_capabilities.supports_responses_compact",
	"provider_capabilities.supports_prompt_cache_key",
	"provider_capabilities.supports_native_web_search",
	"provider_capabilities.supports_reasoning_encrypted",
	"provider_capabilities.supports_server_side_context_edit",
	"provider_capabilities.supports_provider_verbosity",
	"provider_capabilities.is_openai_first_party",
}

func hasAnyConfiguredSource(sources map[string]Origin, keys ...string) bool {
	for _, key := range keys {
		if hasConfiguredSource(sources, key) {
			return true
		}
	}
	return false
}

func NormalizeSettingsForPersistenceWithSources(settings Settings, sources map[string]Origin) (Settings, error) {
	normalized := settings
	if normalized.EnabledTools == nil {
		normalized.EnabledTools = defaultEnabledToolMap()
	}
	if normalized.SkillToggles == nil {
		normalized.SkillToggles = map[string]bool{}
	}
	effectiveSources := cloneSourceMapOrDefault(sources)
	InheritReviewerSettings(&normalized, effectiveSources)
	if err := configRegistry.validate(settingsState{Settings: normalized}, effectiveSources, resolvedContextConstraints(normalized)); err != nil {
		return Settings{}, err
	}
	return normalized, nil
}

func cloneSourceMapOrDefault(sources map[string]Origin) map[string]Origin {
	if len(sources) == 0 {
		out := configRegistry.defaultSourceMap()
		out["model"] = Origin{Kind: SourceInput, Property: PropertyAddress{Key: "model"}}
		return out
	}
	out := make(map[string]Origin, len(sources)+1)
	for key, value := range sources {
		out[key] = value
	}
	if _, present := out["model"]; !present {
		out["model"] = Origin{Kind: SourceInput, Property: PropertyAddress{Key: "model"}}
	}
	return out
}

func ValidateSettingsWithSources(settings Settings, sources map[string]Origin) error {
	return configRegistry.validate(settingsState{Settings: settings}, sources, resolvedContextConstraints(settings))
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

func parsePositiveIntString(raw string, envName string) (*int, error) {
	parsed, err := strconv.Atoi(raw)
	if err != nil || parsed <= 0 {
		return nil, fmt.Errorf("invalid %s: %q", envName, raw)
	}
	return &parsed, nil
}
