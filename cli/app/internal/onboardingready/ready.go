package onboardingready

import (
	"context"

	serverauth "core/server/auth"
	serveronboarding "core/server/onboarding"
	sharedauth "core/shared/auth"
	"core/shared/config"
)

type AuthManager = serverauth.Manager
type AuthState = sharedauth.State
type Result = serveronboarding.Result

type InteractiveRunner = serveronboarding.InteractiveRunner

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
