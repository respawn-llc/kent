package startup

import (
	"context"
	"errors"
	"sync"

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
	ClearActiveSession(sessionID string)
}

type EmbeddedServer struct {
	*core.Core

	rpcMu sync.Mutex
	rpc   *runningRPC
}

// ServeBackground binds the loopback control endpoints (configured TCP plus the
// derived same-machine Unix socket) and serves the embedded Core in the
// background so external clients — notably `kent run` subagents launched from an
// interactive session — can attach to the in-process server. The listeners live
// until Close, which tears them down before closing the Core; the subagent
// servers therefore stop when the owning session exits, which is intended. It is
// an error to call it more than once.
func (s *EmbeddedServer) ServeBackground() error {
	if s == nil || s.Core == nil {
		return errors.New("server core is required")
	}
	s.rpcMu.Lock()
	defer s.rpcMu.Unlock()
	if s.rpc != nil {
		return errors.New("embedded server is already serving")
	}
	rpc, err := startCoreRPC(s.Core)
	if err != nil {
		return err
	}
	s.rpc = rpc
	return nil
}

// Close stops the background control endpoints (if ServeBackground was called)
// and then closes the underlying Core.
func (s *EmbeddedServer) Close() error {
	if s == nil {
		return nil
	}
	s.rpcMu.Lock()
	rpc := s.rpc
	s.rpc = nil
	s.rpcMu.Unlock()
	if rpc != nil {
		rpc.shutdown()
		rpc.wait()
	}
	if s.Core == nil {
		return nil
	}
	return s.Core.Close()
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
