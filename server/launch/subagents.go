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
func ResolveConfiguredSubagentSettings(base config.App, roleName string) (config.Settings, error) {
	resolved, _, _, err := resolveSubagentSettingsWithProviderID(base.Settings, base.Source, roleName, "", true, false)
	if err != nil {
		return config.Settings{}, err
	}
	return resolved, nil
}

func resolveSubagentSettingsFromRole(base config.Settings, baseSource config.SourceReport, selector string, role config.SubagentRole, providerID string, allowModelOverride bool, validate bool) (config.Settings, config.SourceReport, *string, error) {
	resolved := cloneSettings(base)
	_ = applyBuiltInRoleHeuristics(&resolved, selector, providerID, allowModelOverride)
	originalModel := strings.TrimSpace(resolved.Model)
	resolved, effectiveSources, err := config.OverlaySubagentRoleSettings(config.App{Settings: resolved, Source: baseSource}, role, allowModelOverride)
	if err != nil {
		return config.Settings{}, config.SourceReport{}, nil, err
	}
	resolved, effectiveSources = config.OverlayAgentOverrides(resolved, effectiveSources, base, baseSource.Sources, allowModelOverride)
	explicitSources := make(map[string]config.Origin, len(role.Sources))
	maps.Copy(explicitSources, role.Sources)
	for key, origin := range baseSource.Sources {
		if origin.OverridesRole(key) {
			explicitSources[key] = origin
		}
	}
	applyDerivedModelContextBudgetOverrides(&resolved, explicitSources, originalModel, allowModelOverride)
	effectiveSource := baseSource
	if !allowModelOverride && effectiveSources["model"].Kind == config.SourceDefault {
		effectiveSources["model"] = config.Origin{Kind: config.SourceSession, Property: config.PropertyAddress{Key: "model"}}
	}
	config.InheritReviewerSettings(&resolved, effectiveSources)
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

func cloneSettings(in config.Settings) config.Settings {
	out := in
	out.Shell.PostprocessHook = textutil.Pointer(in.Shell.PostprocessHook)
	out.SystemPromptFile = textutil.Pointer(in.SystemPromptFile)
	out.Reviewer.SystemPromptFile = textutil.Pointer(in.Reviewer.SystemPromptFile)
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
