package authflowadapter

import (
	"builder/server/auth"
	"builder/server/authflow"
)

type InteractionRequest = authflow.InteractionRequest
type InteractionOutcome = authflow.InteractionOutcome
type Store = auth.Store
type Method = auth.Method

const (
	MethodNone                     = auth.MethodNone
	MethodOAuth                    = auth.MethodOAuth
	EnvAPIKeyPreferenceUnspecified = auth.EnvAPIKeyPreferenceUnspecified
	EnvAPIKeyPreferencePreferEnv   = auth.EnvAPIKeyPreferencePreferEnv
	EnvAPIKeyPreferencePreferSaved = auth.EnvAPIKeyPreferencePreferSaved
)

func WrapStoreWithEnvAPIKeyOverride(base auth.Store, lookupEnv func(string) string) auth.Store {
	return authflow.WrapStoreWithEnvAPIKeyOverride(base, lookupEnv)
}

func EnsureEmptyStartupReady() error {
	return auth.EnsureStartupReady(auth.EmptyState())
}
