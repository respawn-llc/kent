package onboardingready

import (
	"context"

	"builder/cli/app/internal/serverbridge"
	"builder/shared/auth"
	"builder/shared/config"
)

type AuthManager = serverbridge.AuthManager
type AuthState = auth.State
type Result = serverbridge.OnboardingResult

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
	return serverbridge.EnsureOnboardingReady(
		ctx,
		req.Config,
		req.AuthManager,
		req.Interactive,
		req.ReloadConfig,
		req.Runner,
	)
}
