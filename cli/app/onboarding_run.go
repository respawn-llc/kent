package app

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"core/cli/app/internal/onboarding"
	"core/server/llm"
	"core/shared/config"

	tea "github.com/charmbracelet/bubbletea"
)

func runOnboardingFlow(cfg config.App, authState onboarding.AuthState) (onboardingResult, error) {
	providerCaps, err := llm.ProviderCapabilitiesForSettings(authState, cfg.Settings)
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
	model.settingsPath = strings.TrimSpace(cfg.Source.HomeSettingsPath)
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
