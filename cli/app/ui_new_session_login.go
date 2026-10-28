package app

import (
	"context"
	"errors"
	"io"

	tea "github.com/charmbracelet/bubbletea"
)

type newSessionLoginDoneMsg struct{ err error }

type newSessionLoginCommand struct {
	login    func(context.Context) error
	keyboard *uiTerminalKeyboardReportingOwner
}

func (command newSessionLoginCommand) Run() error {
	if err := command.keyboard.Suspend(); err != nil {
		return err
	}
	return command.login(context.Background())
}

// The existing connection manager owns its terminal streams.
func (newSessionLoginCommand) SetStdin(io.Reader)  {}
func (newSessionLoginCommand) SetStdout(io.Writer) {}
func (newSessionLoginCommand) SetStderr(io.Writer) {}

func (m *uiModel) loginNewSessionCmd() tea.Cmd {
	target := m.newSessionTarget()
	return tea.Exec(newSessionLoginCommand{login: target.login, keyboard: m.keyboardReporting}, func(err error) tea.Msg {
		return newSessionLoginDoneMsg{err: err}
	})
}

func (m *uiModel) applyNewSessionLoginDone(msg newSessionLoginDoneMsg) tea.Cmd {
	err := errors.Join(msg.err, m.keyboardReporting.Resume())
	read := m.loadNewSessionSettingsCmd(nil)
	if err != nil {
		return sequenceCmds(read, m.sendTransientStatusWithNoticeID(err.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, ""))
	}
	return read
}
