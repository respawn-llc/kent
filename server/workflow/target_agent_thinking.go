package workflow

import (
	"fmt"
	"sort"
	"strings"
)

func (c ThinkingCapability) Canonical() ThinkingCapability {
	c.Levels = canonicalThinkingLevels(c.Levels)
	return c
}

func UnionTargetAgentThinkingCapabilities(roles []TargetAgentRole) ThinkingCapability {
	union := ThinkingCapability{}
	for _, role := range roles {
		capability := role.Thinking.Canonical()
		union.ReasoningCapable = union.ReasoningCapable || capability.ReasoningCapable
		if !capability.Finite {
			union.Finite = false
			continue
		}
		if len(roles) == 0 || (!union.Finite && len(union.Levels) == 0) {
			union.Finite = true
		}
		union.Levels = append(union.Levels, capability.Levels...)
	}
	if len(roles) > 0 {
		union.Finite = true
		for _, role := range roles {
			if !role.Thinking.Finite {
				union.Finite = false
				union.Levels = nil
				break
			}
		}
	}
	union.Levels = canonicalThinkingLevels(union.Levels)
	return union
}

func DefaultTargetAgentThinkingDescription(capability ThinkingCapability) string {
	capability = capability.Canonical()
	if !capability.Finite || len(capability.Levels) == 0 {
		return ""
	}
	return "Override the thinking level for the next node, available levels: " + strings.Join(capability.Levels, ", ")
}

func (p thinkingSelectionPolicy) selectThinking(role TargetAgentRole, enabled bool, submittedValue, authoredDescription string) (*ThinkingValue, error) {
	if !enabled || !p.applicability.Available {
		return materializeThinkingValue(role.ConfiguredThinking)
	}
	capability := p.capability
	targetCapability := role.Thinking.Canonical()
	switch p.applicability.Reason {
	case SelectorApplicabilityNoThinkingLevels:
		return materializeThinkingValue(role.ConfiguredThinking)
	case SelectorApplicabilitySoleThinkingLevel:
		if targetCapability.Finite && !containsThinkingLevel(targetCapability.Levels, capability.Levels[0]) {
			return materializeThinkingValue(role.ConfiguredThinking)
		}
		return materializeThinkingValue(capability.Levels[0])
	}
	value := strings.TrimSpace(submittedValue)
	if value == "" {
		return nil, TargetAgentSelectionError{Code: TargetAgentSelectionErrorThinkingRequired}
	}
	if !capability.Finite && strings.TrimSpace(authoredDescription) == "" {
		return nil, TargetAgentSelectionError{Code: TargetAgentSelectionErrorThinkingDescriptionRequired}
	}
	if capability.Finite && !containsThinkingLevel(capability.Levels, value) {
		return nil, TargetAgentSelectionError{Code: TargetAgentSelectionErrorUnsupportedThinking, Role: value}
	}
	if targetCapability.Finite && !containsThinkingLevel(targetCapability.Levels, value) {
		return nil, TargetAgentSelectionError{Code: TargetAgentSelectionErrorUnsupportedThinking, Role: value}
	}
	return materializeThinkingValue(value)
}

func canonicalThinkingLevels(levels []string) []string {
	seen := make(map[string]struct{}, len(levels))
	out := make([]string, 0, len(levels))
	for _, raw := range levels {
		level := strings.TrimSpace(raw)
		if level == "" {
			continue
		}
		if _, exists := seen[level]; exists {
			continue
		}
		seen[level] = struct{}{}
		out = append(out, level)
	}
	sort.Strings(out)
	return out
}

func containsThinkingLevel(levels []string, value string) bool {
	for _, level := range levels {
		if level == value {
			return true
		}
	}
	return false
}

func (e TargetAgentSelectionError) thinkingErrorMessage() string {
	switch e.Code {
	case TargetAgentSelectionErrorThinkingRequired:
		return "thinking level is required"
	case TargetAgentSelectionErrorThinkingDescriptionRequired:
		return "thinking parameter requires an authored description for an open model catalog"
	case TargetAgentSelectionErrorUnsupportedThinking:
		return fmt.Sprintf("thinking level %q is unsupported", e.Role)
	default:
		return string(e.Code)
	}
}
