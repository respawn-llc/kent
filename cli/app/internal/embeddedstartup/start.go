package embeddedstartup

import (
	"context"

	"builder/cli/app/internal/serverbridge"
	"builder/shared/config"
)

type Server = serverbridge.Server
type AuthManager = serverbridge.AuthManager
type AuthHandler interface {
	WrapStore(base serverbridge.AuthStore) serverbridge.AuthStore
	NeedsInteraction(req serverbridge.InteractionRequest) bool
	Interact(ctx context.Context, req serverbridge.InteractionRequest) (serverbridge.InteractionOutcome, error)
	LookupEnv(key string) string
}

type Request struct {
	WorkspaceRoot         string
	WorkspaceRootExplicit bool
	SessionID             string
	OpenAIBaseURL         string
	OpenAIBaseURLExplicit bool
	LoadOptions           config.LoadOptions
}

type OnboardingRequest struct {
	Config       config.App
	AuthManager  *AuthManager
	ReloadConfig func() (config.App, error)
}

type OnboardingHandler interface {
	EnsureOnboardingReady(ctx context.Context, req OnboardingRequest) (config.App, error)
}

func Start(ctx context.Context, req Request, authHandler AuthHandler, onboardingHandler OnboardingHandler) (*Server, error) {
	return serverbridge.StartEmbedded(ctx, buildStartupRequest(req), authHandler, adaptOnboardingHandler(onboardingHandler))
}

func buildStartupRequest(req Request) serverbridge.StartupRequest {
	return serverbridge.StartupRequest{
		WorkspaceRoot:         req.WorkspaceRoot,
		WorkspaceRootExplicit: req.WorkspaceRootExplicit,
		SessionID:             req.SessionID,
		OpenAIBaseURL:         req.OpenAIBaseURL,
		OpenAIBaseURLExplicit: req.OpenAIBaseURLExplicit,
		LoadOptions:           req.LoadOptions,
	}
}

func adaptOnboardingHandler(handler OnboardingHandler) serverbridge.StartupOnboardingHandler {
	if handler == nil {
		return nil
	}
	return onboardingHandlerAdapter{inner: handler}
}

type onboardingHandlerAdapter struct {
	inner OnboardingHandler
}

func (h onboardingHandlerAdapter) EnsureOnboardingReady(ctx context.Context, req serverbridge.StartupOnboardingRequest) (config.App, error) {
	return h.inner.EnsureOnboardingReady(ctx, OnboardingRequest{
		Config:       req.Config,
		AuthManager:  req.AuthManager,
		ReloadConfig: req.ReloadConfig,
	})
}
