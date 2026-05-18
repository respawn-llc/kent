package core

import (
	"strings"

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
	for _, available := range config.AvailableSubagentRoleNames(r.settings, false) {
		if available == trimmed {
			return true
		}
	}
	return false
}
