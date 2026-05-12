package authflowadapter

import (
	"builder/cli/app/internal/serverbridge"
	"builder/shared/auth"
)

type InteractionRequest = serverbridge.InteractionRequest
type InteractionOutcome = serverbridge.InteractionOutcome
type Store = serverbridge.AuthStore
type Method = auth.Method

const (
	MethodNone                     = auth.MethodNone
	MethodOAuth                    = auth.MethodOAuth
	EnvAPIKeyPreferenceUnspecified = auth.EnvAPIKeyPreferenceUnspecified
	EnvAPIKeyPreferencePreferEnv   = auth.EnvAPIKeyPreferencePreferEnv
	EnvAPIKeyPreferencePreferSaved = auth.EnvAPIKeyPreferencePreferSaved
)

func WrapStoreWithEnvAPIKeyOverride(base Store, lookupEnv func(string) string) Store {
	return serverbridge.WrapStoreWithEnvAPIKeyOverride(base, lookupEnv)
}

func EnsureEmptyStartupReady() error {
	return serverbridge.EnsureEmptyStartupReady()
}
