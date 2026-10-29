package app

import (
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/textutil"

	tea "github.com/charmbracelet/bubbletea"
)

type newSessionActionDoneMsg struct {
	chatActionResult
	origin        *uiModel
	submittedText *string
}

func (m *uiModel) newSessionInputCmd(text string, input runtimeinput.Input, intent newSessionInputIntent) tea.Cmd {
	if command, shell := parseUserShellCommand(text); shell {
		input = runtimeinput.Shell(command)
	}
	activation, err := chatActivationFromInput(input)
	if err != nil {
		return m.sendTransientStatusWithNoticeID(err.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	return m.newSessionActionCmd(chatInputAction{input: activation, intent: intent, queueItemID: runtimeids.NewQueueItemID()}, &text)
}

func (m *uiModel) newSessionActionCmd(action chatAction, submittedText *string) tea.Cmd {
	if m.newSessionTarget() != nil && !m.newSessionConnectionReady() {
		if submittedText != nil {
			m.inputController().restoreSubmittedTextIntoInput(*submittedText)
		}
		return nil
	}
	execute := m.captureChatAction(action, submittedText)
	m.pendingNewChatActions++
	result := newSessionActionDoneMsg{
		origin: m, submittedText: textutil.Pointer(submittedText),
	}
	return func() tea.Msg {
		result.chatActionResult = execute()
		return result
	}
}
