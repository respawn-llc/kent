package core

import (
	"strings"

	"builder/server/workflow"
	"builder/shared/config"
)

type configRoleResolver struct {
	settings config.Settings
}

func (r configRoleResolver) RoleExists(role string) bool {
	trimmed := strings.TrimSpace(role)
	if trimmed == "" {
		return false
	}
	if workflow.IsDefaultAgentRole(trimmed) {
		return true
	}
	for _, available := range config.AvailableSubagentRoleNames(r.settings, false) {
		if available == trimmed {
			return true
		}
	}
	return false
}
