package config

import (
	"maps"
	"sort"
	"strings"

	"core/shared/toolspec"
)

const BuiltInSubagentRoleFast = "fast"
const DefaultSubagentRole = "default"
const MaxSubagentDescriptionChars = 5000

var reservedSubagentRoleNames = map[string]bool{
	"none": true,
	"self": true,
}

func NormalizeSubagentRole(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return ""
	}
	if reservedSubagentRoleNames[normalized] {
		return ""
	}
	for _, r := range normalized {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '-', '_':
			continue
		default:
			return ""
		}
	}
	return normalized
}

func NormalizeSubagentSelector(raw string) string {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if reservedSubagentRoleNames[normalized] {
		return ""
	}
	return NormalizeSubagentRole(raw)
}

func IsReservedSubagentRoleName(raw string) bool {
	return reservedSubagentRoleNames[strings.ToLower(strings.TrimSpace(raw))]
}

func IsSubagentRoleNameShape(raw string) bool {
	normalized := strings.ToLower(strings.TrimSpace(raw))
	if normalized == "" {
		return false
	}
	for _, r := range normalized {
		if r >= 'a' && r <= 'z' {
			continue
		}
		if r >= '0' && r <= '9' {
			continue
		}
		switch r {
		case '-', '_':
			continue
		default:
			return false
		}
	}
	return true
}

func SubagentRoleCallable(role SubagentRole) bool {
	return !role.AgentCallableSet() || role.AgentCallable
}

func SubagentRoleWorkflowCallable(role SubagentRole) bool {
	return !role.WorkflowSubagentSet() || role.WorkflowSubagent
}

func (role SubagentRole) AgentCallableSet() bool {
	_, present := role.Sources["agent_callable"]
	return present
}

func (role SubagentRole) WorkflowSubagentSet() bool {
	_, present := role.Sources["workflow_subagent"]
	return present
}

type SubagentInvocationContext string

const (
	SubagentInvocationContextOrdinary SubagentInvocationContext = "ordinary"
	SubagentInvocationContextWorkflow SubagentInvocationContext = "workflow"
)

type SubagentRoleLookupStatus string

const (
	SubagentRoleLookupInvalid SubagentRoleLookupStatus = "invalid"
	SubagentRoleLookupMissing SubagentRoleLookupStatus = "missing"
	SubagentRoleLookupPresent SubagentRoleLookupStatus = "present"
)

type SubagentRoleLookup struct {
	Role               SubagentRole
	NormalizedSelector *string
	Status             SubagentRoleLookupStatus
}

// LookupSubagentRole resolves a subagent selector to configured role identity.
// It treats built-in roles as present and does not apply presentation filters
// such as meaningful-diff or callability checks.
func LookupSubagentRole(settings Settings, rawSelector string) SubagentRoleLookup {
	normalized := NormalizeSubagentSelector(rawSelector)
	if normalized == "" {
		return SubagentRoleLookup{Status: SubagentRoleLookupInvalid}
	}
	if normalized == BuiltInSubagentRoleFast || normalized == DefaultSubagentRole {
		return SubagentRoleLookup{
			Role:               settings.Subagents[normalized],
			NormalizedSelector: subagentRoleLookupSelector(normalized),
			Status:             SubagentRoleLookupPresent,
		}
	}
	role, ok := settings.Subagents[normalized]
	if !ok {
		return SubagentRoleLookup{
			NormalizedSelector: subagentRoleLookupSelector(normalized),
			Status:             SubagentRoleLookupMissing,
		}
	}
	return SubagentRoleLookup{
		Role:               role,
		NormalizedSelector: subagentRoleLookupSelector(normalized),
		Status:             SubagentRoleLookupPresent,
	}
}

func SubagentRoleHasCapabilityOverrides(role SubagentRole) bool {
	return hasAnyConfiguredSource(role.Sources, modelCapabilityKeys...) ||
		hasAnyConfiguredSource(role.Sources, providerCapabilityKeys...)
}

func OverlaySubagentRoleSettings(base Settings, sources map[string]Origin, role SubagentRole, allowModelOverride bool) (Settings, map[string]Origin) {
	return overlaySubagentRoleSettings(base, sources, role, func(key string) bool {
		return subagentRoleSessionSetting(key) && (allowModelOverride || key != "model")
	}, true)
}

func subagentRoleSessionSetting(key string) bool {
	return key != "prevent_sleep" &&
		!strings.HasPrefix(key, "worktrees.") &&
		!strings.HasPrefix(key, "workflow.")
}

func OverlaySubagentRoleProviderSettings(base Settings, sources map[string]Origin, role SubagentRole) Settings {
	settings, _ := overlaySubagentRoleSettings(base, sources, role, func(key string) bool {
		return key == "provider_override" ||
			key == "openai_base_url" ||
			strings.HasPrefix(key, "provider_capabilities.")
	}, false)
	return settings
}

func overlaySubagentRoleSettings(base Settings, sources map[string]Origin, role SubagentRole, include func(string) bool, includeDynamicSettings bool) (Settings, map[string]Origin) {
	return overlayDeclaredSettings(base, sources, role.Settings, role.Sources, func(key string, _ Origin) bool {
		return include(key) && !sources[key].OverridesRole(key)
	}, includeDynamicSettings)
}

// OverlayAgentOverrides restores already decoded environment/CLI declarations
// after role heuristics without treating inherited Supervisor values as declarations.
func OverlayAgentOverrides(base Settings, sources map[string]Origin, overrides Settings, origins map[string]Origin, allowModelOverride bool) (Settings, map[string]Origin) {
	return overlayDeclaredSettings(base, sources, overrides, origins, func(key string, origin Origin) bool {
		return (allowModelOverride || key != "model") && origin.OverridesRole(key)
	}, true)
}

// OverlayCLIOverrides projects the decoded request options at the existing
// model/tool contract boundary.
func OverlayCLIOverrides(base Settings, sources map[string]Origin, overrides Settings, origins map[string]Origin, allowModelOverride, allowToolOverride bool) (Settings, map[string]Origin) {
	return overlayDeclaredSettings(base, sources, overrides, origins, func(key string, origin Origin) bool {
		return origin.Kind == SourceCLI && origin.OverridesRole(key) &&
			(allowModelOverride || key != "model") && (allowToolOverride || origin.Property.Key != "tools")
	}, true)
}

func overlayDeclaredSettings(base Settings, sources map[string]Origin, overlay Settings, declarations map[string]Origin, include func(string, Origin) bool, includeDynamicSettings bool) (Settings, map[string]Origin) {
	target := settingsState{Settings: base}
	overlayState := settingsState{Settings: overlay}
	resultSources := make(map[string]Origin, len(sources)+len(declarations))
	maps.Copy(resultSources, sources)
	for key, origin := range declarations {
		if !include(key, origin) {
			continue
		}
		setting, ok := configRegistry.subagentRoleValues[key]
		if !ok {
			continue
		}
		setting.applySubagentRoleValue(&target, overlayState)
		resultSources[key] = origin
		if key == "system_prompt_file" {
			target.Settings.SystemPromptFiles = append(
				append([]SystemPromptFile(nil), base.SystemPromptFiles...),
				overlay.SystemPromptFiles...,
			)
		}
	}
	if !includeDynamicSettings {
		return target.Settings, resultSources
	}
	target.Settings.EnabledTools = maps.Clone(base.EnabledTools)
	for _, id := range toolspec.CatalogIDs() {
		key := toolSourceKey(id)
		origin, exists := declarations[key]
		if !exists || !include(key, origin) {
			continue
		}
		if target.Settings.EnabledTools == nil {
			target.Settings.EnabledTools = map[toolspec.ID]bool{}
		}
		target.Settings.EnabledTools[id] = overlay.EnabledTools[id]
		resultSources[key] = origin
	}
	target.Settings.SkillToggles = maps.Clone(base.SkillToggles)
	for key, enabled := range overlay.SkillToggles {
		sourceKey := skillSourceKey(key)
		origin, exists := declarations[sourceKey]
		if !exists || !include(sourceKey, origin) {
			continue
		}
		if target.Settings.SkillToggles == nil {
			target.Settings.SkillToggles = map[string]bool{}
		}
		target.Settings.SkillToggles[key] = enabled
		resultSources[sourceKey] = origin
	}
	return target.Settings, resultSources
}

func subagentRoleLookupSelector(selector string) *string {
	return &selector
}

// AvailableSubagentRoleNames returns presentation-ready role names. It filters
// out the separately presented default role and configured roles that have no
// runtime diff from the base settings, and can optionally filter non-callable
// roles. Use LookupSubagentRole for existence.
func AvailableSubagentRoleNames(settings Settings, agentCallableOnly bool) []string {
	return availableSubagentRoleNames(settings, func(name string, _ SubagentRole) bool {
		if !agentCallableOnly {
			return true
		}
		lookup := LookupSubagentRole(settings, name)
		return lookup.Status == SubagentRoleLookupPresent && SubagentRoleCallable(lookup.Role)
	})
}

func availableSubagentRoleNames(settings Settings, include func(string, SubagentRole) bool) []string {
	names := []string{}
	if include(BuiltInSubagentRoleFast, settings.Subagents[BuiltInSubagentRoleFast]) {
		names = append(names, BuiltInSubagentRoleFast)
	}
	for name, role := range settings.Subagents {
		normalized := NormalizeSubagentRole(name)
		if normalized == "" || normalized == BuiltInSubagentRoleFast || normalized == DefaultSubagentRole {
			continue
		}
		if !SubagentRoleHasMeaningfulDiff(settings, role) {
			continue
		}
		if !include(normalized, role) {
			continue
		}
		names = append(names, normalized)
	}
	sort.Strings(names)
	for i, name := range names {
		if name == BuiltInSubagentRoleFast {
			copy(names[1:i+1], names[0:i])
			names[0] = BuiltInSubagentRoleFast
			break
		}
	}
	return names
}

func SubagentRoleHasMeaningfulDiff(base Settings, role SubagentRole) bool {
	for key := range role.Sources {
		if subagentSourceDiffers(base, role, key) {
			return true
		}
	}
	return false
}

func subagentSourceDiffers(base Settings, role SubagentRole, key string) bool {
	if subagentRoleSourceUsesValueComparison(key) {
		if setting, ok := configRegistry.subagentRoleValues[key]; ok {
			return setting.subagentRoleValueDiffers(
				settingsState{Settings: base},
				settingsState{Settings: role.Settings},
			)
		}
	}
	if strings.HasPrefix(key, "tools.") {
		toolName := strings.TrimPrefix(key, "tools.")
		if id, ok := toolspec.ParseConfigID(toolName); ok {
			return base.EnabledTools[id] != role.Settings.EnabledTools[id]
		}
		return true
	}
	if strings.HasPrefix(key, "skills.") {
		name := strings.TrimPrefix(key, "skills.")
		return base.SkillToggles[name] != role.Settings.SkillToggles[name]
	}
	return true
}

func subagentRoleSourceUsesValueComparison(key string) bool {
	switch key {
	case "theme", "notification_method", "debug", "server_host", "server_port", "store", "allow_non_cwd_edits":
		return false
	}
	return subagentRoleSessionSetting(key)
}
