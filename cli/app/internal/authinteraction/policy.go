package authinteraction

import "builder/cli/app/internal/authflowadapter"

type Request = authflowadapter.InteractionRequest

func HeadlessNeedsInteraction(req Request) bool {
	return req.AuthRequired && !req.Gate.Ready
}

func InteractiveNeedsInteraction(req Request) bool {
	if !req.AuthRequired && !req.PromptOptional {
		return NeedsEnvConflictResolution(req)
	}
	return NeedsAuthMethodSelection(req) || NeedsEnvConflictResolution(req)
}

func NeedsAuthMethodSelection(req Request) bool {
	if !req.Gate.Ready {
		return true
	}
	return req.HasEnvAPIKey &&
		req.StoredState.EnvAPIKeyPreference == authflowadapter.EnvAPIKeyPreferenceUnspecified &&
		!req.StoredState.IsConfigured()
}

func NeedsEnvConflictResolution(req Request) bool {
	return req.Gate.Ready &&
		req.HasEnvAPIKey &&
		req.StoredState.EnvAPIKeyPreference == authflowadapter.EnvAPIKeyPreferenceUnspecified &&
		req.StoredState.Method.Type == authflowadapter.MethodOAuth
}

func ShouldClearOnSkip(req Request) bool {
	if req.StoredState.IsConfigured() {
		return true
	}
	return req.StoredState.EnvAPIKeyPreference != authflowadapter.EnvAPIKeyPreferenceUnspecified
}
