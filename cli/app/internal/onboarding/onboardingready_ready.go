package onboarding

import (
	"context"

	serverauth "core/server/auth"
	serverstartup "core/server/startup"
	sharedauth "core/shared/auth"
	"core/shared/config"
)

type AuthManager = serverauth.Manager
type AuthState = sharedauth.State
type Result = serverstartup.OnboardingResult

type InteractiveRunner = serverstartup.OnboardingInteractiveRunner

type Request struct {
	Config       config.App
	AuthManager  *AuthManager
	Interactive  bool
	ReloadConfig func() (config.App, error)
	Runner       InteractiveRunner
}

func Ensure(ctx context.Context, req Request) (config.App, bool, error) {
	return serverstartup.EnsureOnboardingReady(
		ctx,
		req.Config,
		req.AuthManager,
		req.Interactive,
		req.ReloadConfig,
		req.Runner,
	)
}
