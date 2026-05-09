package embeddedstartup

import (
	"context"

	"builder/server/auth"
	"builder/server/authflow"
	serverembedded "builder/server/embedded"
	serverstartup "builder/server/startup"
	"builder/shared/config"
)

type Server = serverembedded.Server
type AuthManager = auth.Manager
type AuthHandler interface {
	WrapStore(base auth.Store) auth.Store
	NeedsInteraction(req authflow.InteractionRequest) bool
	Interact(ctx context.Context, req authflow.InteractionRequest) (authflow.InteractionOutcome, error)
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
	return serverstartup.Start(ctx, buildStartupRequest(req), authHandler, adaptOnboardingHandler(onboardingHandler))
}

func buildStartupRequest(req Request) serverstartup.Request {
	return serverstartup.Request{
		WorkspaceRoot:         req.WorkspaceRoot,
		WorkspaceRootExplicit: req.WorkspaceRootExplicit,
		SessionID:             req.SessionID,
		OpenAIBaseURL:         req.OpenAIBaseURL,
		OpenAIBaseURLExplicit: req.OpenAIBaseURLExplicit,
		LoadOptions:           req.LoadOptions,
	}
}

func adaptOnboardingHandler(handler OnboardingHandler) serverstartup.OnboardingHandler {
	if handler == nil {
		return nil
	}
	return onboardingHandlerAdapter{inner: handler}
}

type onboardingHandlerAdapter struct {
	inner OnboardingHandler
}

func (h onboardingHandlerAdapter) EnsureOnboardingReady(ctx context.Context, req serverstartup.OnboardingRequest) (config.App, error) {
	return h.inner.EnsureOnboardingReady(ctx, OnboardingRequest{
		Config:       req.Config,
		AuthManager:  req.AuthManager,
		ReloadConfig: req.ReloadConfig,
	})
}
