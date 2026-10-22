package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"core/cli/app/internal/runtimeattach"
	"core/shared/clientui"
	"core/shared/runtimeinput"
	"core/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
)

func (c uiInputController) startSubmission(text string) tea.Cmd {
	if cmd, rejected := c.rejectUnavailablePromptCommand(text); rejected {
		return cmd
	}
	return c.startTypedSubmission(text, runtimeinput.Text(text))
}

func (c uiInputController) startTypedSubmission(text string, input runtimeinput.Input) tea.Cmd {
	m := c.model
	if blocked, disconnectCmd := c.blockDisconnectedSubmission(true, text); blocked {
		return disconnectCmd
	}
	m.layout().syncViewport()
	return tea.Batch(m.enqueueInput(text, input, runtimeinput.PendingWorkLaneSteer), m.reconcileSpinnerTicking(false))
}

func (c uiInputController) startCompaction(submittedText, args string) tea.Cmd {
	m := c.model
	if m.isCompacting() {
		return nil
	}
	c.startRuntimeOperationAffordance()
	m.logf("compaction.start args_chars=%d", len(strings.TrimSpace(args)))
	m.layout().syncViewport()
	return tea.Batch(
		c.compactCmd(submittedText, optionalCompactionGuidance(args)),
		m.reconcileSpinnerTicking(false),
	)
}

func (c uiInputController) compactCmd(
	submittedText string,
	guidance *string,
) tea.Cmd {
	m := c.model
	client := m.runtimeClient()
	sessionID := m.pendingWorkRefresh.sessionID
	return func() tea.Msg {
		if client == nil {
			return compactDoneMsg{
				sessionID:     sessionID,
				submittedText: submittedText,
				err:           errors.New("runtime engine is not configured"),
			}
		}
		result, err := m.compactRuntimeInput(context.Background(), clientui.RuntimeCompactRequest{
			Admission: runtimeinput.ManualCompactionAdmission{
				Guidance: guidance,
			},
		})
		return compactDoneMsg{
			sessionID: sessionID, submittedText: submittedText,
			diagnostic: result.Diagnostic, err: err,
		}
	}
}

func optionalCompactionGuidance(value string) *string {
	normalized := runtimeinput.NormalizePendingWorkArgument(value)
	if normalized == "" {
		return nil
	}
	return &normalized
}

func (c uiInputController) startRuntimeOperationAffordance() {
	m := c.model
	m.clearActiveAssistantStreamSource()
}

func (c uiInputController) finishRuntimeOperationAffordance() {
	m := c.model
	m.spinnerFrame = 0
	if !m.shouldAnimateSpinner() {
		m.stopSpinnerTicking()
	}
}

func (c uiInputController) notifyTurnQueueDrainedIfIdle() {
	m := c.model
	if m.turnQueueHook == nil || !c.turnQueueDrained() {
		return
	}
	m.turnQueueHook.OnTurnQueueDrained()
}

func (c uiInputController) turnQueueDrained() bool {
	m := c.model
	return m != nil &&
		!m.blocksRuntimeInput() &&
		!m.ask.hasCurrent()
}

func (m *uiModel) acceptedSubmissionDiagnosticCmd(diagnostic error) tea.Cmd {
	if diagnostic == nil {
		return nil
	}
	m.logf("submission.accepted diagnostic=%q", diagnostic.Error())
	return m.sendTransientStatusWithNoticeID(diagnostic.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
}

func (c uiInputController) handleSpinnerTick(msg spinnerTickMsg) (tea.Model, tea.Cmd) {
	m := c.model
	if msg.token == 0 || msg.token != m.spinnerTickToken {
		return m, nil
	}
	if !m.shouldAnimateSpinner() {
		m.stopSpinnerTicking()
		return m, nil
	}
	frameCount := len(pendingToolSpinner.Frames)
	if frameCount <= 0 {
		frameCount = 1
	}
	tickAt := msg.at
	if tickAt.IsZero() {
		tickAt = m.spinnerClock.anchor
		if tickAt.IsZero() {
			tickAt = uiAnimationNow()
		}
		tickAt = tickAt.Add(time.Duration(m.spinnerFrame+1) * spinnerTickInterval)
	}
	m.spinnerFrame = m.spinnerClock.Frame(tickAt, frameCount, spinnerTickInterval)
	m.layout().syncViewport()
	return m, m.scheduleSpinnerTick(msg.token, tickAt)
}

func (c uiInputController) handleCompactDone(msg compactDoneMsg) (tea.Model, tea.Cmd) {
	m := c.model
	serverActiveBeforeCompletion := m.runtimeActivityBusy()
	m.observeRuntimeRequestResult(msg.err)
	c.finishRuntimeOperationAffordance()
	if msg.err != nil {
		if errors.Is(msg.err, serverapi.ErrPendingWorkCapacity) {
			c.restoreSubmittedTextIntoInput(msg.submittedText)
		}
		if isRuntimeOperationInterrupted(msg.err) {
			m.activity = uiActivityInterrupted
			m.logf("step.interrupted")
			m.layout().syncViewport()
			return m, m.interruptedStatusNoticeCmd()
		}
		detailErr := runtimeattach.FormatSubmissionError(msg.err)
		m.activity = uiActivityError
		appendCmd := m.appendLocalEntryWithNoticeID(operatorErrorFeedbackRole, detailErr, "")
		m.logf("compaction.error err=%q", detailErr)
		m.layout().syncViewport()
		return m, appendCmd
	}
	pendingWorkRefreshCmd := tea.Batch(m.requestPendingWorkRefresh(msg.sessionID), m.acceptedSubmissionDiagnosticCmd(msg.diagnostic))

	if !serverActiveBeforeCompletion {
		m.activity = uiActivityIdle
	}
	m.logf("compaction.scheduled")
	m.layout().syncViewport()
	return m, pendingWorkRefreshCmd
}

func isRuntimeOperationInterrupted(err error) bool {
	return errors.Is(err, runtimeattach.ErrSubmissionInterrupted) ||
		errors.Is(err, context.Canceled)
}
