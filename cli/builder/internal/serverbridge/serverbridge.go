package serverbridge

// Package serverbridge is the documented CLI binary composition bridge for
// local server startup/lifecycle wiring. Command handlers should depend on this
// package or shared client contracts instead of server packages directly.

import (
	"context"

	"builder/server/serve"
	"builder/server/sessionlifecycle"
	serverstartup "builder/server/startup"
	"builder/shared/client"
	"builder/shared/config"
)

type StartupRequest = serverstartup.Request
type StartupAuthHandler = serverstartup.AuthHandler
type StartupOnboardingHandler = serverstartup.OnboardingHandler

type ServeServer interface {
	Close() error
	Serve(ctx context.Context) error
}

func NewLocalSessionLifecycleClient(cfg config.App) client.SessionLifecycleClient {
	return client.NewLoopbackSessionLifecycleClient(sessionlifecycle.NewGlobalService(cfg.PersistenceRoot, nil, nil))
}

func StartServe(ctx context.Context, req serverstartup.Request, authHandler serverstartup.AuthHandler, onboardingHandler serverstartup.OnboardingHandler) (ServeServer, error) {
	return serve.Start(ctx, req, authHandler, onboardingHandler)
}

func NewHeadlessHandlers(lookupEnv func(string) string) (serverstartup.AuthHandler, serverstartup.OnboardingHandler) {
	return serverstartup.NewHeadlessHandlers(lookupEnv)
}
