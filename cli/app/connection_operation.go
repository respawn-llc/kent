package app

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
)

type connectionOperationDone[T any] struct {
	value T
	err   error
}

type connectionOperationModel[T any] struct {
	*onboardingModel
	operation func() (T, error)
	outcome   *connectionOperationDone[T]
}

func (m *connectionOperationModel[T]) Init() tea.Cmd {
	return tea.Batch(tickOnboardingSpinner(spinnerTickInterval), func() tea.Msg {
		value, err := m.operation()
		return connectionOperationDone[T]{value: value, err: err}
	})
}

func (m *connectionOperationModel[T]) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok && (key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC) {
		m.outcome = &connectionOperationDone[T]{err: ErrAuthCanceledByUser}
		return m, tea.Quit
	}
	if done, ok := msg.(connectionOperationDone[T]); ok {
		m.outcome = &done
		return m, tea.Quit
	}
	_, command := m.onboardingModel.Update(msg)
	return m, command
}

func runConnectionOperation[T any](ctx context.Context, selectedTheme string, label string, operation func() (T, error)) (T, error) {
	screen := &onboardingModel{
		width: defaultPickerWidth, height: defaultPickerHeight,
		styles:        newOnboardingStyles(selectedTheme),
		currentScreen: onboardingScreen{Kind: onboardingScreenLoading, Title: "Provider connections", LoadingText: label},
	}
	screen.spinnerClock.Start(uiAnimationNow())
	model := &connectionOperationModel[T]{onboardingModel: screen, operation: operation}
	_, err := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(ctx)).Run()
	var zero T
	if err != nil {
		return zero, err
	}
	if model.outcome == nil {
		return zero, errors.New("connection operation ended without a result")
	}
	return model.outcome.value, model.outcome.err
}
