package app

import (
	"context"
	"errors"

	"core/cli/app/internal/embeddedattach"
	"core/cli/app/internal/onboarding"
	"core/shared/config"
)

func startEmbeddedServer(ctx context.Context, opts Options, interactor authInteractor, interactive bool) (*embeddedAppServer, error) {
	if interactor == nil {
		return nil, errors.New("auth interactor is required")
	}
	server, err := embeddedattach.Start(ctx, embeddedattach.StartupRequest{
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
			ConfigRoot:          opts.ConfigRoot,
		},
	}, interactor, func(ctx context.Context, req embeddedattach.OnboardingRequest) (config.App, error) {
		cfg, _, err := onboarding.Ensure(ctx, onboarding.Request{
			Config:       req.Config,
			AuthManager:  req.AuthManager,
			Interactive:  interactive,
			ReloadConfig: req.ReloadConfig,
			Runner: func(ctx context.Context, cfg config.App, authState onboarding.AuthState) (onboarding.Result, error) {
				result, err := runOnboardingFlow(cfg, authState)
				if err != nil {
					return onboarding.Result{}, err
				}
				return onboarding.Result{
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
