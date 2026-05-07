package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"builder/server/auth"
	"builder/server/llm"
	serveronboarding "builder/server/onboarding"
	"builder/shared/config"
	tea "github.com/charmbracelet/bubbletea"
)

type interactiveOnboardingRunner struct{}

func (interactiveOnboardingRunner) RunInteractiveOnboarding(ctx context.Context, cfg config.App, authState auth.State) (serveronboarding.Result, error) {
	result, err := runOnboardingFlow(cfg, authState)
	if err != nil {
		return serveronboarding.Result{}, err
	}
	return serveronboarding.Result{
		Completed:            result.Completed,
		CreatedDefaultConfig: result.CreatedDefaultConfig,
		SettingsPath:         result.SettingsPath,
	}, nil
}

func ensureOnboardingReady(ctx context.Context, cfg config.App, mgr *auth.Manager, interactor authInteractor, reloadConfig func() (config.App, error)) (config.App, bool, error) {
	return serveronboarding.EnsureReady(ctx, cfg, mgr, interactor != nil && interactor.Interactive(), reloadConfig, interactiveOnboardingRunner{})
}

func runOnboardingFlow(cfg config.App, authState auth.State) (onboardingResult, error) {
	providerCaps, err := onboardingProviderCapabilities(authState, cfg.Settings)
	if err != nil {
		return onboardingResult{}, err
	}
	state := onboardingFlowState{
		settings:             cfg.Settings,
		baselineSettings:     cfg.Settings,
		theme:                cfg.Settings.Theme,
		authState:            authState,
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

func onboardingProviderCapabilities(authState auth.State, settings config.Settings) (llm.ProviderCapabilities, error) {
	return llm.ProviderCapabilitiesForSettings(authState, settings)
}

func filepathDir(path string) string {
	if strings.TrimSpace(path) == "" {
		return ""
	}
	return filepath.Dir(path)
}
