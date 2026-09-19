package startup

import (
	"context"

	"core/shared/config"
)

func HeadlessOnboarding(_ context.Context, req OnboardingRequest) (config.App, error) {
	if !req.Config.Source.SettingsFileExists() {
		return config.App{}, ErrOnboardingRequired
	}
	return req.Config, nil
}
