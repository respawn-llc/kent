package authui

import (
	serverauth "core/server/auth"
	"core/server/authservice"
	"core/shared/auth"
)

type AuthInteractionRequest = authservice.FlowInteractionRequest
type AuthInteractionOutcome = authservice.FlowInteractionOutcome
type AuthStore = serverauth.Store
type AuthMethod = auth.Method

const (
	AuthMethodNone                 = auth.MethodNone
	AuthMethodOAuth                = auth.MethodOAuth
	EnvAPIKeyPreferenceUnspecified = auth.EnvAPIKeyPreferenceUnspecified
	EnvAPIKeyPreferencePreferEnv   = auth.EnvAPIKeyPreferencePreferEnv
	EnvAPIKeyPreferencePreferSaved = auth.EnvAPIKeyPreferencePreferSaved
)
