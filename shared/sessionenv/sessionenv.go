package sessionenv

import (
	"strings"

	brand "core/shared/config"
)

const SessionIDEnv = brand.SessionIDEnv
const ShellTokenEnv = brand.EnvPrefix + "SHELL_TOKEN"

func LookupSessionID(lookup func(string) (string, bool)) (string, bool) {
	if lookup == nil {
		return "", false
	}
	value, ok := lookup(SessionIDEnv)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}

func LookupShellToken(lookup func(string) (string, bool)) (string, bool) {
	if lookup == nil {
		return "", false
	}
	value, ok := lookup(ShellTokenEnv)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}
