package sessionenv

import (
	"strings"

	brand "core/shared/config"
)

const SessionIDEnv = brand.SessionIDEnv
const StepIDEnv = brand.EnvPrefix + "STEP_ID"

func LookupSessionID(lookup func(string) (string, bool)) (string, bool) {
	return lookupTrimmed(lookup, SessionIDEnv)
}

func LookupStepID(lookup func(string) (string, bool)) (string, bool) {
	return lookupTrimmed(lookup, StepIDEnv)
}

func lookupTrimmed(lookup func(string) (string, bool), key string) (string, bool) {
	if lookup == nil {
		return "", false
	}
	value, ok := lookup(key)
	if !ok {
		return "", false
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", false
	}
	return trimmed, true
}
