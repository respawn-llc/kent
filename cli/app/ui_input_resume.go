package app

import (
	"context"
	"errors"

	tea "github.com/charmbracelet/bubbletea"
)

func (c uiInputController) startQueuedInjectionSubmission() tea.Cmd {
	m := c.model
	if blocked, disconnectCmd := c.blockDisconnectedSubmission(true, ""); blocked {
		return disconnectCmd
	}
	queuedRuntimeWork, err := m.hasQueuedRuntimeUserWork()
	if err != nil {
		c.restorePendingInjectedIntoInput()
		if isInterruptedRuntimeError(err) {
			m.activity = uiActivityInterrupted
			m.logf("step.interrupted")
			m.syncViewport()
			return nil
		}
		detailErr := formatSubmissionError(err)
		m.activity = uiActivityError
		appendCmd := m.appendOperatorErrorFeedback(detailErr)
		m.logf("queue_check.error err=%q", detailErr)
		m.syncViewport()
		return appendCmd
	}
	if !queuedRuntimeWork {
		return nil
	}
	c.startBusyActivity(false)
	m.logf("step.resume_queued_injected pending_injected=%d", len(m.pendingInjected))
	m.syncViewport()
	return tea.Batch(c.submitQueuedUserMessagesCmd(), c.model.ensureSpinnerTicking())
}

func (c uiInputController) submitQueuedUserMessagesCmd() tea.Cmd {
	m := c.model
	token := m.beginSubmitAttempt("", "")
	return func() tea.Msg {
		if !m.hasRuntimeClient() {
			return newSubmitDoneMsg(token, "", "", errors.New("runtime engine is not configured"))
		}
		msg, err := m.submitQueuedRuntimeUserMessages(context.Background())
		if err != nil {
			if errors.Is(err, context.Canceled) {
				return newSubmitDoneMsg(token, "", "", errSubmissionInterrupted)
			}
			return newSubmitDoneMsg(token, "", "", err)
		}
		return newSubmitDoneMsg(token, msg, "", nil)
	}
}
