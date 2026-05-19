package serverbridge

// Package serverbridge is the documented CLI binary composition bridge for
// local server startup/lifecycle wiring. Command handlers should depend on this
// package or shared client contracts instead of server packages directly.

import (
	"context"
	"errors"

	"builder/server/serve"
	"builder/server/sessionlifecycle"
	serverstartup "builder/server/startup"
	"builder/shared/client"
	"builder/shared/config"
	"builder/shared/serverapi"
)

type StartupRequest = serverstartup.Request
type StartupAuthHandler = serverstartup.AuthHandler
type StartupOnboardingHandler = serverstartup.OnboardingHandler

type ServeServer interface {
	Close() error
	Serve(ctx context.Context) error
}

func NewLocalSessionLifecycleClient(cfg config.App) client.SessionLifecycleClient {
	client, err := sessionlifecycle.NewMetadataBackedLoopbackClient(cfg.PersistenceRoot, nil)
	if err != nil {
		return failingSessionLifecycleClient{err: err}
	}
	return client
}

type failingSessionLifecycleClient struct {
	err error
}

func (c failingSessionLifecycleClient) failure() error {
	if c.err == nil {
		return errors.New("session lifecycle client unavailable")
	}
	return c.err
}

func (c failingSessionLifecycleClient) Close() error {
	return nil
}

func (c failingSessionLifecycleClient) GetInitialInput(context.Context, serverapi.SessionInitialInputRequest) (serverapi.SessionInitialInputResponse, error) {
	return serverapi.SessionInitialInputResponse{}, c.failure()
}

func (c failingSessionLifecycleClient) PersistInputDraft(context.Context, serverapi.SessionPersistInputDraftRequest) (serverapi.SessionPersistInputDraftResponse, error) {
	return serverapi.SessionPersistInputDraftResponse{}, c.failure()
}

func (c failingSessionLifecycleClient) RetargetSessionWorkspace(context.Context, serverapi.SessionRetargetWorkspaceRequest) (serverapi.SessionRetargetWorkspaceResponse, error) {
	return serverapi.SessionRetargetWorkspaceResponse{}, c.failure()
}

func (c failingSessionLifecycleClient) ResolveTransition(context.Context, serverapi.SessionResolveTransitionRequest) (serverapi.SessionResolveTransitionResponse, error) {
	return serverapi.SessionResolveTransitionResponse{}, c.failure()
}

func StartServe(ctx context.Context, req StartupRequest, authHandler StartupAuthHandler, onboardingHandler StartupOnboardingHandler) (ServeServer, error) {
	return serve.Start(ctx, req, authHandler, onboardingHandler)
}

func NewHeadlessHandlers(lookupEnv func(string) string) (StartupAuthHandler, StartupOnboardingHandler) {
	return serverstartup.NewHeadlessHandlers(lookupEnv)
}
