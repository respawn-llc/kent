package onboarding

import (
	"context"
	"errors"
	"strings"

	"core/server/auth"
	"core/shared/config"
)

var ErrOnboardingCanceled = errors.New("first-time setup canceled")

// ErrAuthManagerRequired and ErrInteractiveRunnerRequired guard interactive
// onboarding readiness. Callers and tests match these with errors.Is rather
// than comparing rendered message text.
var (
	ErrAuthManagerRequired       = errors.New("auth manager is required for onboarding")
	ErrInteractiveRunnerRequired = errors.New("interactive onboarding runner is required")
)

type Result struct {
	Completed            bool
	CreatedDefaultConfig bool
	SettingsPath         string
}

type InteractiveRunner func(ctx context.Context, cfg config.App, authState auth.State) (Result, error)

func EnsureReady(ctx context.Context, cfg config.App, mgr *auth.Manager, interactive bool, reloadConfig func() (config.App, error), runner InteractiveRunner) (config.App, bool, error) {
	if cfg.Source.SettingsFileExists {
		return cfg, false, nil
	}
	if reloadConfig == nil {
		return cfg, false, errors.New("reload config is required")
	}
	if !interactive {
		path, created, err := config.WriteDefaultSettingsFile()
		if err != nil {
			return cfg, false, err
		}
		reloaded, err := reloadConfig()
		if err != nil {
			return cfg, false, err
		}
		reloaded.Source.CreatedDefaultConfig = created
		reloaded.Source.SettingsPath = path
		reloaded.Source.SettingsFileExists = true
		return reloaded, true, nil
	}
	if mgr == nil {
		return cfg, false, ErrAuthManagerRequired
	}
	if runner == nil {
		return cfg, false, ErrInteractiveRunnerRequired
	}
	state, err := mgr.Load(ctx)
	if err != nil {
		return cfg, false, err
	}
	result, err := runner(ctx, cfg, state)
	if err != nil {
		return cfg, false, err
	}
	if !result.Completed {
		return cfg, false, ErrOnboardingCanceled
	}
	reloaded, err := reloadConfig()
	if err != nil {
		return cfg, false, err
	}
	reloaded.Source.CreatedDefaultConfig = result.CreatedDefaultConfig
	reloaded.Source.SettingsFileExists = true
	if strings.TrimSpace(result.SettingsPath) != "" {
		reloaded.Source.SettingsPath = result.SettingsPath
	}
	return reloaded, true, nil
}
