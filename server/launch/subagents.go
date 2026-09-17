package launch

import (
	"fmt"
	"maps"
	"reflect"
	"strings"

	"core/server/llm"
	"core/shared/config"
	"core/shared/textutil"
)

const fastRoleSameAsMainWarning = "Warning: user configuration for fast agents is the same as for other agents. Consider asking the user to edit their config to pick a faster, smaller model at the end of your task. More info at " + config.DocsURL

func resolveSubagentSettingsWithProviderID(base config.Settings, baseSource config.SourceReport, roleName string, providerID string, allowModelOverride bool, validate bool) (config.Settings, config.SourceReport, *string, error) {
	lookup := config.LookupSubagentRole(base, roleName)
	switch lookup.Status {
	case config.SubagentRoleLookupInvalid:
		return config.Settings{}, config.SourceReport{}, nil, fmt.Errorf("invalid subagent role %q", roleName)
	case config.SubagentRoleLookupMissing:
		return config.Settings{}, config.SourceReport{}, nil, fmt.Errorf("Unrecognized role %q. It may have been removed by the user during the session. Available roles: [%s]", *lookup.NormalizedSelector, strings.Join(config.AvailableSubagentRoleNames(base, false), ", "))
	}
	return resolveSubagentSettingsFromRole(
		base,
		baseSource,
		*lookup.NormalizedSelector,
		lookup.Role,
		strings.TrimSpace(providerID),
		allowModelOverride,
		validate,
	)
}

// ResolveConfiguredSubagentSettings resolves one configured role against the
// current base settings without creating a session or mutating configuration.
func ResolveConfiguredSubagentSettings(base config.Settings, roleName string) (config.Settings, error) {
	resolved, _, _, err := resolveSubagentSettingsWithProviderID(base, config.SourceReport{}, roleName, "", true, false)
	if err != nil {
		return config.Settings{}, err
	}
	return resolved, nil
}

func resolveSubagentSettingsFromRole(base config.Settings, baseSource config.SourceReport, selector string, role config.SubagentRole, providerID string, allowModelOverride bool, validate bool) (config.Settings, config.SourceReport, *string, error) {
	resolved := cloneSettings(base)
	_ = applyBuiltInRoleHeuristics(&resolved, selector, providerID, allowModelOverride)
	originalModel := strings.TrimSpace(resolved.Model)
	resolved = config.OverlaySubagentRoleSettings(resolved, role, allowModelOverride)
	applyDerivedModelContextBudgetOverrides(&resolved, role.Sources, originalModel, allowModelOverride)
	effectiveSource := sourceReportWithSubagentRoleSources(baseSource, role, allowModelOverride)
	effectiveSources := cloneMapOrEmpty(effectiveSource.Sources)
	applyReviewerInheritance(&resolved, effectiveSources)
	effectiveSource.Sources = effectiveSources
	if validate {
		if err := config.ValidateSettingsWithSources(resolved, effectiveSources); err != nil {
			return config.Settings{}, config.SourceReport{}, nil, fmt.Errorf("invalid subagent role %q: %w", selector, err)
		}
	}
	var warning *string
	if selector == config.BuiltInSubagentRoleFast && sameResolvedSubagentSettings(base, resolved) {
		warningValue := fastRoleSameAsMainWarning
		warning = &warningValue
	}
	return resolved, effectiveSource, warning, nil
}

func applyBuiltInRoleHeuristics(settings *config.Settings, roleName string, providerID string, allowModelOverride bool) bool {
	if settings == nil || roleName != config.BuiltInSubagentRoleFast {
		return false
	}
	if providerID != "openai" && providerID != "chatgpt-codex" {
		return false
	}
	settings.PriorityRequestMode = true
	if !allowModelOverride {
		return true
	}
	settings.Model = "gpt-5.6-terra"
	settings.ThinkingLevel = "low"
	llm.ApplyDerivedModelContextBudget(settings, settings.Model, settings.ModelContextWindow, settings.ContextCompactionThresholdTokens)
	settings.PreSubmitCompactionLeadTokens = config.DefaultPreSubmitRunwayTokens
	return true
}

func applyDerivedModelContextBudgetOverrides(settings *config.Settings, explicitSources map[string]config.Origin, originalModel string, allowModelOverride bool) {
	if settings == nil || !allowModelOverride {
		return
	}
	if _, ok := explicitSources["model"]; !ok {
		return
	}
	if strings.TrimSpace(settings.Model) == "" || strings.TrimSpace(settings.Model) == originalModel {
		return
	}
	if _, ok := explicitSources["model_context_window"]; !ok {
		if meta, ok := llm.LookupModelMetadata(settings.Model); ok && meta.ContextWindowTokens > 0 {
			settings.ModelContextWindow = meta.ContextWindowTokens
		}
	}
	if _, ok := explicitSources["context_compaction_threshold_tokens"]; !ok && settings.ModelContextWindow > 0 {
		settings.ContextCompactionThresholdTokens = settings.ModelContextWindow * 95 / 100
	}
	if _, ok := explicitSources["pre_submit_compaction_lead_tokens"]; !ok {
		settings.PreSubmitCompactionLeadTokens = config.DefaultPreSubmitRunwayTokens
	}
}

func applyReviewerInheritance(settings *config.Settings, sources map[string]config.Origin) {
	if settings == nil {
		return
	}
	if sources["reviewer.model"].Inherited("reviewer.model") {
		settings.Reviewer.Model = settings.Model
	}
	if sources["reviewer.thinking_level"].Inherited("reviewer.thinking_level") {
		settings.Reviewer.ThinkingLevel = settings.ThinkingLevel
	}
	if sources["reviewer.model_verbosity"].Inherited("reviewer.model_verbosity") {
		settings.Reviewer.ModelVerbosity = settings.ModelVerbosity
	}
	reviewerProviderSourceDefault := sources["reviewer.provider_override"].Inherited("reviewer.provider_override")
	reviewerBaseURLSourceDefault := sources["reviewer.openai_base_url"].Inherited("reviewer.openai_base_url")
	if reviewerProviderSourceDefault || reviewerBaseURLSourceDefault {
		originalProviderOverride := settings.Reviewer.ProviderOverride
		originalOpenAIBaseURL := settings.Reviewer.OpenAIBaseURL
		if reviewerProviderSourceDefault {
			settings.Reviewer.ProviderOverride = ""
		}
		if reviewerBaseURLSourceDefault {
			settings.Reviewer.OpenAIBaseURL = ""
		}
		reviewerProvider := config.ResolveReviewerProviderSettings(*settings)
		if reviewerProviderSourceDefault {
			settings.Reviewer.ProviderOverride = reviewerProvider.ProviderOverride
		} else {
			settings.Reviewer.ProviderOverride = originalProviderOverride
		}
		if reviewerBaseURLSourceDefault {
			settings.Reviewer.OpenAIBaseURL = reviewerProvider.OpenAIBaseURL
		} else {
			settings.Reviewer.OpenAIBaseURL = originalOpenAIBaseURL
		}
	}
	if sources["reviewer.model_context_window"].Inherited("reviewer.model_context_window") {
		settings.Reviewer.ModelContextWindow = settings.ModelContextWindow
	}
	if sources["reviewer.auth"].Inherited("reviewer.auth") {
		settings.Reviewer.Auth = "inherit"
	}
	applyReviewerModelCapabilityInheritance(settings, sources)
	reviewerProviderSelectionExplicit := reviewerProviderSelectionExplicitForInheritance(settings, sources)
	applyReviewerProviderCapabilityInheritance(settings, sources, reviewerProviderSelectionExplicit)
}

func reviewerProviderSelectionExplicitForInheritance(settings *config.Settings, sources map[string]config.Origin) bool {
	if settings == nil {
		return false
	}
	candidate := *settings
	if sources["reviewer.provider_override"].Inherited("reviewer.provider_override") {
		candidate.Reviewer.ProviderOverride = ""
	}
	if sources["reviewer.openai_base_url"].Inherited("reviewer.openai_base_url") {
		candidate.Reviewer.OpenAIBaseURL = ""
	}
	return config.ReviewerUsesIndependentProviderSelection(candidate)
}

func applyReviewerModelCapabilityInheritance(settings *config.Settings, sources map[string]config.Origin) {
	if settings == nil {
		return
	}
	if sources["reviewer.model_capabilities.supports_reasoning_effort"].Inherited("reviewer.model_capabilities.supports_reasoning_effort") {
		settings.Reviewer.ModelCapabilities.SupportsReasoningEffort = settings.ModelCapabilities.SupportsReasoningEffort
	}
	if sources["reviewer.model_capabilities.supports_vision_inputs"].Inherited("reviewer.model_capabilities.supports_vision_inputs") {
		settings.Reviewer.ModelCapabilities.SupportsVisionInputs = settings.ModelCapabilities.SupportsVisionInputs
	}
}

func applyReviewerProviderCapabilityInheritance(settings *config.Settings, sources map[string]config.Origin, reviewerProviderSelectionExplicit bool) {
	if settings == nil || reviewerProviderSelectionExplicit {
		return
	}
	if sources["reviewer.provider_capabilities.provider_id"].Inherited("reviewer.provider_capabilities.provider_id") {
		settings.Reviewer.ProviderCapabilities.ProviderID = settings.ProviderCapabilities.ProviderID
	}
	if sources["reviewer.provider_capabilities.supports_responses_api"].Inherited("reviewer.provider_capabilities.supports_responses_api") {
		settings.Reviewer.ProviderCapabilities.SupportsResponsesAPI = settings.ProviderCapabilities.SupportsResponsesAPI
	}
	if sources["reviewer.provider_capabilities.supports_responses_compact"].Inherited("reviewer.provider_capabilities.supports_responses_compact") {
		settings.Reviewer.ProviderCapabilities.SupportsResponsesCompact = settings.ProviderCapabilities.SupportsResponsesCompact
	}
	if sources["reviewer.provider_capabilities.supports_prompt_cache_key"].Inherited("reviewer.provider_capabilities.supports_prompt_cache_key") {
		settings.Reviewer.ProviderCapabilities.SupportsPromptCacheKey = settings.ProviderCapabilities.SupportsPromptCacheKey
	}
	if sources["reviewer.provider_capabilities.supports_native_web_search"].Inherited("reviewer.provider_capabilities.supports_native_web_search") {
		settings.Reviewer.ProviderCapabilities.SupportsNativeWebSearch = settings.ProviderCapabilities.SupportsNativeWebSearch
	}
	if sources["reviewer.provider_capabilities.supports_reasoning_encrypted"].Inherited("reviewer.provider_capabilities.supports_reasoning_encrypted") {
		settings.Reviewer.ProviderCapabilities.SupportsReasoningEncrypted = settings.ProviderCapabilities.SupportsReasoningEncrypted
	}
	if sources["reviewer.provider_capabilities.supports_server_side_context_edit"].Inherited("reviewer.provider_capabilities.supports_server_side_context_edit") {
		settings.Reviewer.ProviderCapabilities.SupportsServerSideContextEdit = settings.ProviderCapabilities.SupportsServerSideContextEdit
	}
	if sources["reviewer.provider_capabilities.supports_provider_verbosity"].Inherited("reviewer.provider_capabilities.supports_provider_verbosity") {
		settings.Reviewer.ProviderCapabilities.SupportsProviderVerbosity = settings.ProviderCapabilities.SupportsProviderVerbosity
	}
	if sources["reviewer.provider_capabilities.is_openai_first_party"].Inherited("reviewer.provider_capabilities.is_openai_first_party") {
		settings.Reviewer.ProviderCapabilities.IsOpenAIFirstParty = settings.ProviderCapabilities.IsOpenAIFirstParty
	}
}

func cloneSettings(in config.Settings) config.Settings {
	out := in
	out.Shell.PostprocessHook = textutil.Pointer(in.Shell.PostprocessHook)
	out.SystemPromptFiles = append([]config.SystemPromptFile(nil), in.SystemPromptFiles...)
	out.EnabledTools = cloneMapOrEmpty(in.EnabledTools)
	out.SkillToggles = cloneMapOrEmpty(in.SkillToggles)
	out.Subagents = cloneSubagentRoles(in.Subagents)
	return out
}

func cloneMapOrEmpty[M ~map[K]V, K comparable, V any](in M) M {
	if in == nil {
		return make(M)
	}
	return maps.Clone(in)
}

func cloneSubagentRoles(in map[string]config.SubagentRole) map[string]config.SubagentRole {
	if len(in) == 0 {
		return map[string]config.SubagentRole{}
	}
	out := make(map[string]config.SubagentRole, len(in))
	for key, role := range in {
		copied := role
		copied.Settings = cloneSettings(role.Settings)
		copied.Sources = cloneMapOrEmpty(role.Sources)
		out[key] = copied
	}
	return out
}

func sameResolvedSubagentSettings(base config.Settings, resolved config.Settings) bool {
	left := normalizeComparableSettings(base)
	right := normalizeComparableSettings(resolved)
	return reflect.DeepEqual(left, right)
}

func normalizeComparableSettings(settings config.Settings) config.Settings {
	normalized := cloneSettings(settings)
	normalized.Subagents = nil
	if len(normalized.SkillToggles) == 0 {
		normalized.SkillToggles = nil
	}
	return normalized
}
