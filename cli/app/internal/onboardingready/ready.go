package onboardingready

import (
	"context"

	"builder/server/auth"
	serveronboarding "builder/server/onboarding"
	"builder/shared/config"
)

type AuthManager = auth.Manager
type AuthState = auth.State
type Result = serveronboarding.Result

type InteractiveRunner interface {
	RunInteractiveOnboarding(ctx context.Context, cfg config.App, authState AuthState) (Result, error)
}

type Request struct {
	Config       config.App
	AuthManager  *AuthManager
	Interactive  bool
	ReloadConfig func() (config.App, error)
	Runner       InteractiveRunner
}

func Ensure(ctx context.Context, req Request) (config.App, bool, error) {
	return serveronboarding.EnsureReady(
		ctx,
		req.Config,
		req.AuthManager,
		req.Interactive,
		req.ReloadConfig,
		req.Runner,
	)
}
