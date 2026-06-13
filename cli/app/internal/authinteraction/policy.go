package authinteraction

import "core/cli/app/internal/authflowadapter"

type Request = authflowadapter.InteractionRequest

func NeedsEnvConflictResolution(req Request) bool {
	return req.Gate.Ready &&
		req.HasEnvAPIKey &&
		req.StoredState.EnvAPIKeyPreference == authflowadapter.EnvAPIKeyPreferenceUnspecified &&
		req.StoredState.Method.Type == authflowadapter.MethodOAuth
}
