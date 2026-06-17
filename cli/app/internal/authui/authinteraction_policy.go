package authui

func NeedsAuthEnvConflictResolution(req AuthInteractionRequest) bool {
	return req.Gate.Ready &&
		req.HasEnvAPIKey &&
		req.StoredState.EnvAPIKeyPreference == EnvAPIKeyPreferenceUnspecified &&
		req.StoredState.Method.Type == AuthMethodOAuth
}
