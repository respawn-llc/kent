package app

import (
	"context"
	"errors"

	"core/cli/app/internal/embeddedstartup"
	"core/cli/app/internal/onboardingready"
	"core/shared/config"
)

func startEmbeddedServer(ctx context.Context, opts Options, interactor authInteractor, interactive bool) (*embeddedAppServer, error) {
	if interactor == nil {
		return nil, errors.New("auth interactor is required")
	}
	server, err := embeddedstartup.Start(ctx, embeddedstartup.Request{
		WorkspaceRoot:         opts.WorkspaceRoot,
		WorkspaceRootExplicit: opts.WorkspaceRootExplicit,
		SessionID:             opts.SessionID,
		OpenAIBaseURL:         opts.OpenAIBaseURL,
		OpenAIBaseURLExplicit: opts.OpenAIBaseURLExplicit,
		LoadOptions: config.LoadOptions{
			Model:               opts.Model,
			ProviderOverride:    opts.ProviderOverride,
			ThinkingLevel:       opts.ThinkingLevel,
			Theme:               opts.Theme,
			ModelTimeoutSeconds: opts.ModelTimeoutSeconds,
			Tools:               opts.Tools,
		},
	}, interactor, func(ctx context.Context, req embeddedstartup.OnboardingRequest) (config.App, error) {
		cfg, _, err := onboardingready.Ensure(ctx, onboardingready.Request{
			Config:       req.Config,
			AuthManager:  req.AuthManager,
			Interactive:  interactive,
			ReloadConfig: req.ReloadConfig,
			Runner: func(ctx context.Context, cfg config.App, authState onboardingready.AuthState) (onboardingready.Result, error) {
				result, err := runOnboardingFlow(cfg, authState)
				if err != nil {
					return onboardingready.Result{}, err
				}
				return onboardingready.Result{
					Completed:            result.Completed,
					CreatedDefaultConfig: result.CreatedDefaultConfig,
					SettingsPath:         result.SettingsPath,
				}, nil
			},
		})
		if err != nil {
			return config.App{}, err
		}
		return cfg, nil
	})
	if err != nil {
		return nil, err
	}
	return newEmbeddedAppServer(server), nil
}
