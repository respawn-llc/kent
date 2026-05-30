package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"builder/cli/app/internal/onboardingmodel"
	"builder/cli/app/internal/onboardingready"
	"builder/shared/config"
	tea "github.com/charmbracelet/bubbletea"
)

type interactiveOnboardingRunner struct{}

func (interactiveOnboardingRunner) RunInteractiveOnboarding(ctx context.Context, cfg config.App, authState onboardingready.AuthState) (onboardingready.Result, error) {
	result, err := runOnboardingFlow(cfg, authState)
	if err != nil {
		return onboardingready.Result{}, err
	}
	return onboardingready.Result{
		Completed:            result.Completed,
		CreatedDefaultConfig: result.CreatedDefaultConfig,
		SettingsPath:         result.SettingsPath,
	}, nil
}

func ensureOnboardingReady(ctx context.Context, cfg config.App, mgr *onboardingready.AuthManager, interactor authInteractor, reloadConfig func() (config.App, error)) (config.App, bool, error) {
	return onboardingready.Ensure(ctx, onboardingready.Request{
		Config:       cfg,
		AuthManager:  mgr,
		Interactive:  interactor != nil && interactor.Interactive(),
		ReloadConfig: reloadConfig,
		Runner:       interactiveOnboardingRunner{},
	})
}

func runOnboardingFlow(cfg config.App, authState onboardingready.AuthState) (onboardingResult, error) {
	providerCaps, err := onboardingProviderCapabilities(authState, cfg.Settings)
	if err != nil {
		return onboardingResult{}, err
	}
	state := onboardingFlowState{
		settings:             cfg.Settings,
		baselineSettings:     cfg.Settings,
		theme:                cfg.Settings.Theme,
		providerCapabilities: providerCaps,
		skillImport:          onboardingImportSelection{Mode: onboardingImportModeNone},
		commandImport:        onboardingImportSelection{Mode: onboardingImportModeNone},
	}
	model := newOnboardingModelForWorkspace(cfg.PersistenceRoot, cfg.WorkspaceRoot, state)
	terminalCursor := newUITerminalCursorState()
	model.terminalCursor = terminalCursor
	program := tea.NewProgram(model, tea.WithAltScreen(), tea.WithOutput(newUITerminalCursorWriter(os.Stdout, terminalCursor)))
	finalModel, err := program.Run()
	if err != nil {
		return onboardingResult{}, err
	}
	finalized, ok := finalModel.(*onboardingModel)
	if !ok {
		return onboardingResult{}, fmt.Errorf("unexpected onboarding model type %T", finalModel)
	}
	if finalized.canceled {
		return onboardingResult{}, errors.New("first-time setup canceled")
	}
	return finalized.result, nil
}

func onboardingProviderCapabilities(authState onboardingready.AuthState, settings config.Settings) (onboardingmodel.ProviderCapabilities, error) {
	return onboardingmodel.ProviderCapabilitiesForSettings(authState, settings)
}
