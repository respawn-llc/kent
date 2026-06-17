package startup

import (
	"context"
	"errors"

	"core/server/auth"
	"core/server/authservice"
	serverbootstrap "core/server/bootstrap"
	"core/server/core"
	"core/server/runtime"
	"core/shared/config"
)

type EmbeddedAuthHandler interface {
	WrapStore(base auth.Store) auth.Store
	NeedsInteraction(req authservice.FlowInteractionRequest) bool
	Interact(ctx context.Context, req authservice.FlowInteractionRequest) (authservice.FlowInteractionOutcome, error)
}

type EmbeddedOnboardingHandler func(ctx context.Context, req EmbeddedOnboardingRequest) (config.App, error)

type EmbeddedOnboardingRequest struct {
	Config       config.App
	AuthManager  *auth.Manager
	ReloadConfig func() (config.App, error)
}

type EmbeddedStartHooks struct {
	Auth       EmbeddedAuthHandler
	Onboarding EmbeddedOnboardingHandler
}

type BackgroundRouter interface {
	SetActiveSession(sessionID string, engine *runtime.Engine)
	ClearActiveSession(sessionID string, engine *runtime.Engine)
}

type EmbeddedServer struct {
	*core.Core
}

func StartEmbedded(ctx context.Context, req serverbootstrap.Request, hooks EmbeddedStartHooks) (*EmbeddedServer, error) {
	if hooks.Auth == nil {
		return nil, errors.New("auth handler is required")
	}
	resolved, err := serverbootstrap.ResolveConfig(req)
	if err != nil {
		return nil, err
	}
	cfg := resolved.Config
	store := hooks.Auth.WrapStore(auth.NewFileStore(config.GlobalAuthConfigPath(cfg)))
	authSupport, err := serverbootstrap.BuildAuthSupport(store, req.LookupEnv, req.Now)
	if err != nil {
		return nil, err
	}
	if err := authservice.EnsureFlowReady(ctx, authSupport.AuthManager, authSupport.OAuthOptions, cfg.Settings.Theme, req.LookupEnv, authservice.StartupAuthRequired(cfg.Settings), false, hooks.Auth); err != nil {
		return nil, err
	}
	if hooks.Onboarding != nil {
		cfg, err = hooks.Onboarding(ctx, EmbeddedOnboardingRequest{
			Config:      cfg,
			AuthManager: authSupport.AuthManager,
			ReloadConfig: func() (config.App, error) {
				refreshed, err := serverbootstrap.ResolveConfig(req)
				if err != nil {
					return config.App{}, err
				}
				return refreshed.Config, nil
			},
		})
		if err != nil {
			return nil, err
		}
	}
	runtimeSupport, err := serverbootstrap.BuildRuntimeSupport(cfg)
	if err != nil {
		return nil, err
	}
	appCore, err := core.NewWithContext(ctx, cfg, authSupport, runtimeSupport)
	if err != nil {
		_ = runtimeSupport.Background.Close()
		return nil, err
	}
	return &EmbeddedServer{Core: appCore}, nil
}
