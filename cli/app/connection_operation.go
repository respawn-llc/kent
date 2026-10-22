package app

import (
	"context"
	"errors"
	"log"
	"os"

	"core/cli/app/internal/authui"

	tea "github.com/charmbracelet/bubbletea"
)

type connectionPresentationError struct{ cause error }

func (e *connectionPresentationError) Error() string {
	text, _ := authui.ConnectionFailureText(e.cause)
	return text
}

func (e *connectionPresentationError) Unwrap() error { return e.cause }

func presentConnectionError(err error) error {
	if _, known := authui.ConnectionFailureText(err); known {
		return &connectionPresentationError{cause: err}
	}
	return err
}

func connectionOperationErrorText(err error) string {
	if text, known := authui.ConnectionFailureText(err); known {
		return text
	}
	log.Printf("connection operation failed: %v", err)
	return "The connection operation failed. Check the diagnostics and try again."
}

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
	if key, ok := msg.(tea.KeyMsg); ok && (key.Type == tea.KeyEsc || key.Type == tea.KeyCtrlC || key.Type == tea.KeyShiftTab) {
		err := ErrAuthBack
		if key.Type == tea.KeyCtrlC {
			err = ErrAuthCanceledByUser
		}
		m.outcome = &connectionOperationDone[T]{err: err}
		return m, tea.Quit
	}
	if done, ok := msg.(connectionOperationDone[T]); ok {
		m.outcome = &done
		return m, tea.Quit
	}
	_, command := m.onboardingModel.Update(msg)
	return m, command
}

func runConnectionOperation[T any](ctx context.Context, selectedTheme string, operation func() (T, error)) (T, error) {
	return runConnectionOperationScreen(ctx, selectedTheme, onboardingScreen{Kind: onboardingScreenLoading}, operation)
}

func runConnectionOperationScreen[T any](ctx context.Context, selectedTheme string, presentation onboardingScreen, operation func() (T, error)) (T, error) {
	screen := &onboardingModel{
		width: defaultPickerWidth, height: defaultPickerHeight,
		styles:        newOnboardingStyles(selectedTheme),
		currentScreen: presentation,
	}
	screen.spinnerClock.Start(uiAnimationNow())
	model := &connectionOperationModel[T]{onboardingModel: screen, operation: operation}
	_, err := runStartupAlternateScreen(ctx, model, os.Stdout)
	var zero T
	if err != nil {
		return zero, err
	}
	if model.outcome == nil {
		return zero, errors.New("connection operation ended without a result")
	}
	return model.outcome.value, presentConnectionError(model.outcome.err)
}
