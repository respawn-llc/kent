package app

import (
	"strings"

	brand "core/shared/config"
)

const defaultSessionTitle = brand.Command

func sessionTitle(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return defaultSessionTitle
	}
	return trimmed
}
