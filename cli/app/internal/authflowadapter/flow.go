package authflowadapter

import (
	serverauth "core/server/auth"
	"core/server/authflow"
	"core/shared/auth"
)

type InteractionRequest = authflow.InteractionRequest
type InteractionOutcome = authflow.InteractionOutcome
type Store = serverauth.Store
type Method = auth.Method

const (
	MethodNone                     = auth.MethodNone
	MethodOAuth                    = auth.MethodOAuth
	EnvAPIKeyPreferenceUnspecified = auth.EnvAPIKeyPreferenceUnspecified
	EnvAPIKeyPreferencePreferEnv   = auth.EnvAPIKeyPreferencePreferEnv
	EnvAPIKeyPreferencePreferSaved = auth.EnvAPIKeyPreferencePreferSaved
)
