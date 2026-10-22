package app

import (
	"context"
	"errors"
	"fmt"
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

func runContextualStartupPicker(ctx context.Context, model tea.Model) (tea.Model, error) {
	terminal := startupPickerTerminal{state: startupPickerTerminalInactive}
	if err := terminal.Enter(); err != nil {
		return nil, err
	}
	finalModel, runErr := tea.NewProgram(model, tea.WithContext(ctx)).Run()
	closeErr := terminal.Close()
	if runErr != nil && closeErr != nil {
		return nil, errors.Join(runErr, closeErr)
	}
	if runErr != nil {
		return nil, runErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return finalModel, nil
}

// Bubble Tea owns Alternate Screen here so its renderer and native-cursor
// coordinates agree. The terminal owner manages Alternate Scroll separately.
func runStartupAlternateScreen(ctx context.Context, model tea.Model, output io.Writer) (tea.Model, error) {
	terminal := startupPickerTerminal{state: startupPickerTerminalInactive}
	wrapped := &startupAlternateScreenModel{Model: model, terminal: &terminal}
	_, runErr := tea.NewProgram(wrapped, tea.WithAltScreen(), tea.WithContext(ctx), tea.WithOutput(output)).Run()
	return wrapped.Model, errors.Join(runErr, wrapped.entryErr, terminal.Close())
}

type startupAlternateScreenModel struct {
	tea.Model
	terminal *startupPickerTerminal
	entryErr error
}

func (m *startupAlternateScreenModel) Init() tea.Cmd {
	// Init runs after Bubble Tea enters Alternate Screen, before starting the
	// wrapped model's asynchronous work.
	if err := m.terminal.EnableAlternateScroll(); err != nil {
		m.entryErr = err
		return tea.Quit
	}
	return m.Model.Init()
}

func (m *startupAlternateScreenModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	next, command := m.Model.Update(msg)
	m.Model = next
	return m, command
}

type startupPickerTerminalState uint8

const (
	startupPickerTerminalInactive startupPickerTerminalState = iota + 1
	startupPickerTerminalAltScreen
	startupPickerTerminalAlternateScroll
	startupPickerTerminalScrollOnly
	startupPickerTerminalCleaned
)

type startupPickerTerminalError struct {
	Operation string
	Err       error
}

func (e startupPickerTerminalError) Error() string {
	return fmt.Sprintf("startup picker terminal %s failed: %v", e.Operation, e.Err)
}

func (e startupPickerTerminalError) Unwrap() error { return e.Err }

type startupPickerTerminal struct {
	state startupPickerTerminalState
}

func (t *startupPickerTerminal) Enter() error {
	if t == nil || t.state != startupPickerTerminalInactive {
		return errors.New("startup picker terminal must be inactive before entry")
	}
	if err := writeTerminalSequence("\x1b[?1049h"); err != nil {
		return startupPickerTerminalError{Operation: "enter alt-screen", Err: err}
	}
	t.state = startupPickerTerminalAltScreen
	if err := t.EnableAlternateScroll(); err != nil {
		cleanupErr := t.Close()
		if cleanupErr != nil {
			return errors.Join(err, cleanupErr)
		}
		return err
	}
	return nil
}

func (t *startupPickerTerminal) EnableAlternateScroll() error {
	var next startupPickerTerminalState
	switch t.state {
	case startupPickerTerminalInactive:
		next = startupPickerTerminalScrollOnly
	case startupPickerTerminalAltScreen:
		next = startupPickerTerminalAlternateScroll
	default:
		return errors.New("alternate scroll is already owned")
	}
	if err := writeTerminalSequence("\x1b[?1007h"); err != nil {
		return startupPickerTerminalError{Operation: "enable alternate scroll", Err: err}
	}
	t.state = next
	return nil
}

func (t *startupPickerTerminal) Close() error {
	if t == nil || t.state == startupPickerTerminalCleaned {
		return nil
	}
	var result error
	if t.state == startupPickerTerminalAlternateScroll || t.state == startupPickerTerminalScrollOnly {
		if err := writeTerminalSequence("\x1b[?1007l"); err != nil {
			result = startupPickerTerminalError{Operation: "disable alternate scroll", Err: err}
		}
	}
	switch t.state {
	case startupPickerTerminalAltScreen, startupPickerTerminalAlternateScroll:
		if err := writeTerminalSequence("\x1b[?1049l"); err != nil {
			exitErr := startupPickerTerminalError{Operation: "exit alt-screen", Err: err}
			if result != nil {
				result = errors.Join(result, exitErr)
			} else {
				result = exitErr
			}
		}
	case startupPickerTerminalInactive, startupPickerTerminalScrollOnly:
	default:
		panic(fmt.Sprintf("unknown startup picker terminal state %d", t.state))
	}
	t.state = startupPickerTerminalCleaned
	return result
}
