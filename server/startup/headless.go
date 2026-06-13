package startup

import (
	"context"
	"os"

	"core/server/auth"
	"core/server/authflow"
	"core/server/onboarding"
	"core/shared/config"
)

type headlessAuthHandler struct {
	lookupEnv func(string) string
}

func NewHeadlessHandlers(lookupEnv func(string) string) (AuthHandler, OnboardingHandler) {
	return headlessAuthHandler{lookupEnv: lookupEnv}, func(ctx context.Context, req OnboardingRequest) (config.App, error) {
		cfg, _, err := onboarding.EnsureReady(ctx, req.Config, req.AuthManager, false, req.ReloadConfig, nil)
		if err != nil {
			return config.App{}, err
		}
		return cfg, nil
	}
}

func (h headlessAuthHandler) WrapStore(base auth.Store) auth.Store {
	return authflow.WrapStoreWithEnvAPIKeyOverride(base, h.LookupEnv)
}

func (h headlessAuthHandler) NeedsInteraction(req authflow.InteractionRequest) bool {
	return req.AuthRequired && !req.Gate.Ready
}

func (h headlessAuthHandler) Interact(context.Context, authflow.InteractionRequest) (authflow.InteractionOutcome, error) {
	return authflow.InteractionOutcome{}, auth.ErrAuthNotConfigured
}

func (h headlessAuthHandler) LookupEnv(key string) string {
	if h.lookupEnv == nil {
		return os.Getenv(key)
	}
	return h.lookupEnv(key)
}
