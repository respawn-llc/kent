package app

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

import (
	"context"
	"errors"
	"strings"
	"time"

	"core/cli/app/internal/runtimeattach"
	"core/shared/clientui"
	chatpb "core/shared/protoapi/gen/kent/api/chat"
	"core/shared/runtimeinput"
	"core/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
)

type preSubmitQueuePosition uint8

const (
	preSubmitQueueBack preSubmitQueuePosition = iota
	preSubmitQueueFront
)

func (c uiInputController) startSubmissionWithPreSubmitQueuePosition(text string, queuePosition preSubmitQueuePosition, queuedID string) tea.Cmd {
	if cmd, rejected := c.rejectUnavailablePromptCommand(text); rejected {
		return cmd
	}
	return c.startTypedSubmissionWithPreSubmitQueuePosition(text, runtimeinput.Text(text), queuePosition, queuedID, activeSubmitOriginDirect)
}

func (c uiInputController) startSubmissionWithPreSubmitQueuePositionAndOrigin(text string, queuePosition preSubmitQueuePosition, queuedID string, origin activeSubmitOrigin) tea.Cmd {
	return c.startSubmissionWithPreSubmitQueuePositionAndOriginAndOrder(text, queuePosition, queuedID, origin, nil)
}

func (c uiInputController) startSubmissionWithPreSubmitQueuePositionAndOriginAndOrder(
	text string,
	queuePosition preSubmitQueuePosition,
	queuedID string,
	origin activeSubmitOrigin,
	submissionOrder *inputSubmissionOrder,
) tea.Cmd {
	return c.startTypedSubmissionWithPreSubmitQueuePositionAndOrder(
		text,
		runtimeinput.Text(text),
		queuePosition,
		queuedID,
		origin,
		submissionOrder,
	)
}

func (c uiInputController) startTypedSubmissionWithPreSubmitQueuePosition(text string, input runtimeinput.Input, queuePosition preSubmitQueuePosition, queuedID string, origin activeSubmitOrigin) tea.Cmd {
	return c.startTypedSubmissionWithPreSubmitQueuePositionAndOrder(text, input, queuePosition, queuedID, origin, nil)
}

func (c uiInputController) startTypedSubmissionWithPreSubmitQueuePositionAndOrder(
	text string,
	input runtimeinput.Input,
	queuePosition preSubmitQueuePosition,
	queuedID string,
	origin activeSubmitOrigin,
	submissionOrder *inputSubmissionOrder,
) tea.Cmd {
	m := c.model
	if m.usesDirectChatActions() {
		cmd := c.submitCmd(text, input, queuedID, origin, submissionOrder)
		return tea.Batch(cmd, m.reconcileSpinnerTicking(false))
	}
	if blocked, disconnectCmd := c.blockDisconnectedSubmission(true, text); blocked {
		return disconnectCmd
	}
	if blocked, blockCmd := c.blockInjectedQueueSubmission(); blocked {
		return blockCmd
	}
	c.startRuntimeOperationAffordance()
	command, isUserShell := parseUserShellCommand(text)
	if isUserShell {
		m.logf("step.user_shell.start command_chars=%d", len(command))
	} else {
		m.logf("step.start user_chars=%d", len(text))
	}
	if !m.hasRuntimeClient() && !isUserShell {
		m.conversationFreshness = runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED
	}
	m.layout().syncViewport()
	if isUserShell {
		return tea.Batch(c.submitUserShellCmd(text, command, origin, submissionOrder), m.reconcileSpinnerTicking(false))
	}
	return tea.Batch(c.submitCmd(text, input, queuedID, origin, submissionOrder), m.reconcileSpinnerTicking(false))
}

func (c uiInputController) startSubmissionWithPromptHistoryAndQueuePositionAndID(text string, queuePosition preSubmitQueuePosition, queuedID string) tea.Cmd {
	m := c.model
	if cmd, rejected := c.rejectUnavailablePromptCommand(text); rejected {
		return cmd
	}
	if blocked, disconnectCmd := c.blockDisconnectedSubmission(true, text); blocked {
		return disconnectCmd
	}
	if blocked, blockCmd := c.blockInjectedQueueSubmission(); blocked {
		return blockCmd
	}
	m.rememberPromptHistoryLocally(text)
	return c.startSubmissionWithPreSubmitQueuePositionAndOrigin(text, queuePosition, queuedID, activeSubmitOriginDirect)
}

func (c uiInputController) submitCmd(text string, input runtimeinput.Input, queuedID string, origin activeSubmitOrigin, submissionOrder *inputSubmissionOrder) tea.Cmd {
	m := c.model
	if m.usesDirectChatActions() {
		intent := newSessionInputEnter
		if origin == activeSubmitOriginQueued {
			intent = newSessionInputQueue
		}
		return m.newSessionInputCmd(text, input, intent)
	}
	token := m.beginSubmitAttempt(text, queuedID, origin, submissionOrder)
	client := m.runtimeClient()
	sessionID := m.pendingWorkRefresh.sessionID
	return func() tea.Msg {
		doneMessage := func(message string, err error) submitDoneMsg {
			return newSubmitDoneMsg(token, message, text, err)
		}
		if client == nil {
			return doneMessage("", errors.New("runtime engine is not configured"))
		}
		submission, err := m.submitRuntimeInput(context.Background(), clientui.RuntimeSubmitRequest{
			Input: input,
		})
		if submission.SessionID != nil && *submission.SessionID != sessionID.String() {
			done := doneMessage("", err)
			done.diagnostic = submission.Diagnostic
			done.selectedSessionID = submission.SessionID
			return done
		}
		if err != nil {
			if !errors.Is(err, serverapi.ErrRuntimeCommandNotAccepted) && errors.Is(err, context.Canceled) {
				return doneMessage("", runtimeattach.ErrSubmissionInterrupted)
			}
			return doneMessage("", err)
		}
		message := ""
		if submission.Message != nil {
			message = *submission.Message
		}
		done := doneMessage(message, nil)
		done.diagnostic = submission.Diagnostic
		done.selectedSessionID = submission.SessionID
		done.queued = submission.Queued
		resultKind := submission.ResultKind
		done.resultKind = &resultKind
		return done
	}
}

func (c uiInputController) submitUserShellCmd(originalText, command string, origin activeSubmitOrigin, submissionOrder *inputSubmissionOrder) tea.Cmd {
	m := c.model
	if m.usesDirectChatActions() {
		return m.newSessionActionCmd(newSessionShellAction{command: command}, &originalText)
	}
	token := m.beginSubmitAttempt(originalText, "", origin, submissionOrder)
	client := m.runtimeClient()
	return func() tea.Msg {
		if client == nil {
			return newSubmitDoneMsg(token, "", originalText, errors.New("runtime engine is not configured"))
		}
		err := m.submitRuntimeShell(context.Background(), clientui.RuntimeShellRequest{Command: command})
		if err != nil {
			if !errors.Is(err, serverapi.ErrRuntimeCommandNotAccepted) && isRuntimeOperationInterrupted(err) {
				return newSubmitDoneMsg(token, "", originalText, runtimeattach.ErrSubmissionInterrupted)
			}
			return newSubmitDoneMsg(token, "", originalText, err)
		}
		return newSubmitDoneMsg(token, "", originalText, nil)
	}
}

func (m *uiModel) beginSubmitAttempt(
	text string,
	queuedID string,
	origin activeSubmitOrigin,
	submissionOrder *inputSubmissionOrder,
) uint64 {
	if m == nil {
		return 0
	}
	m.submitToken++
	if m.submitToken == 0 {
		m.submitToken++
	}
	var order inputSubmissionOrder
	if submissionOrder == nil {
		order = m.nextPendingInputSubmissionOrder()
	} else {
		order = *submissionOrder
	}
	m.activeSubmit = activeSubmitState{
		token:           m.submitToken,
		text:            text,
		queuedID:        queuedID,
		origin:          origin,
		submissionOrder: order,
	}
	return m.submitToken
}

func (c uiInputController) startCompaction(submittedText, args string) tea.Cmd {
	m := c.model
	if m.usesDirectChatActions() {
		invocation := &chatpb.CompactionInvocation{Token: "/compact"}
		if guidance := optionalCompactionGuidance(args); guidance != nil {
			invocation.SeparatorWhitespace = " "
			invocation.RawGuidance = *guidance
		}
		return m.newSessionActionCmd(newSessionCompactionAction{invocation: invocation}, &submittedText)
	}
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
		len(m.queued) == 0 &&
		!m.injectedQueueBlocksDrain() &&
		!m.hasEnqueuedInjectedRuntimeWork() &&
		!m.ask.hasCurrent()
}

func (c uiInputController) handleSubmitDone(msg submitDoneMsg) (tea.Model, tea.Cmd) {
	m := c.model
	if msg.token == 0 && m.activeSubmit.token != 0 && strings.TrimSpace(msg.submittedText) != "" {
		return m, nil
	}
	if msg.token != 0 && msg.token != m.activeSubmit.token {
		return m, nil
	}
	pendingWorkRefreshCmd := m.requestPendingWorkRefreshIfSuccessful(m.pendingWorkRefresh.sessionID, msg.err == nil)
	submitOrigin := m.activeSubmit.origin
	if msg.selectedSessionID != nil && *msg.selectedSessionID != m.currentRuntimeSessionID() {
		m.nextSessionID = *msg.selectedSessionID
		m.exitAction = UIActionOpenSession
		if msg.diagnostic != nil {
			return m, tea.Sequence(m.appendLocalEntryWithNoticeID("error", msg.diagnostic.Error(), ""), tea.Quit)
		}
		return m, tea.Quit
	}
	pendingWorkRefreshCmd = tea.Batch(pendingWorkRefreshCmd, m.acceptedSubmissionDiagnosticCmd(msg.diagnostic))
	m.observeRuntimeRequestResult(msg.err)
	restoreSubmittedText := msg.err != nil && (msg.token == 0 || m.shouldRestoreSubmittedTextOnSubmitError(msg.err))
	activeQueuedID := m.activeSubmit.queuedID
	var queuedStateCmd tea.Cmd
	if msg.err == nil && msg.queued.ID != "" {
		queuedStateCmd = m.registerSubmittedQueuedUserMessage(msg.queued)
	}
	m.activeSubmit = activeSubmitState{}
	m.clearUnownedQueuedTerminalStatesWithoutPendingOwnership()
	c.finishRuntimeOperationAffordance()
	if msg.token == 0 || !m.hasRuntimeClient() {
		_ = m.applyRuntimeActivityProjection(&runtimepb.Activity{
			State:    runtimepb.ActivityState_RUNTIME_ACTIVITY_REGISTERED_IDLE,
			Reviewer: runtimepb.ReviewerActivity_REVIEWER_ACTIVITY_INACTIVE,
		})
	}
	m.discardQueuedInput(activeQueuedID)
	if msg.err != nil {
		if m.turnQueueHook != nil {
			m.turnQueueHook.OnTurnQueueAborted()
		}
		if isRuntimeOperationInterrupted(msg.err) &&
			!errors.Is(msg.err, serverapi.ErrRuntimeCommandNotAccepted) &&
			m.hasPendingInterrupt() {
			m.activity = uiActivityInterrupted
			m.logf("step.interrupted")
			m.layout().syncViewport()
			return m, m.interruptedStatusNoticeCmd()
		}
		restoreInjectedCmd := tea.Cmd(nil)
		if submitOrigin != activeSubmitOriginQueued {
			restoreInjectedCmd = c.restoreInjectedInputsIntoComposer()
		}
		if restoreSubmittedText {
			c.restoreSubmittedTextIntoInput(msg.submittedText)
		}
		c.restoreQueuedMessagesIntoInput()
		c.notifyTurnQueueDrainedIfIdle()
		if isRuntimeOperationInterrupted(msg.err) {
			m.activity = uiActivityInterrupted
			m.logf("step.interrupted")
			m.layout().syncViewport()
			return m, tea.Batch(restoreInjectedCmd, m.interruptedStatusNoticeCmd())
		}
		detailErr := runtimeattach.FormatSubmissionError(msg.err)
		if submitOrigin != activeSubmitOriginQueued {
			m.activity = uiActivityError
		}
		m.logf("step.error err=%q", detailErr)
		m.layout().syncViewport()
		statusCmd := m.sendTransientStatusWithNoticeID(detailErr, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
		if notFound, ok := promptCommandNotFound(msg.err); ok && notFound.Command != nil {
			if refreshCmd := m.startPromptCatalogRefresh(*notFound.Command); refreshCmd != nil {
				return m, tea.Batch(restoreInjectedCmd, statusCmd, refreshCmd)
			}
		}
		return m, tea.Batch(restoreInjectedCmd, statusCmd)
	}
	if !m.runtimeActivityBusy() {
		m.activity = uiActivityIdle
	}
	if msg.resultKind != nil &&
		*msg.resultKind == clientui.UserTurnResultKindSilentFinal &&
		m.turnQueueHook != nil {
		m.turnQueueHook.OnTurnQueueAborted()
	}
	m.conversationFreshness = runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED
	m.localConversationTurn = true
	m.logf("step.done assistant_chars=%d", len(msg.message))
	m.clearActiveAssistantStreamSource()
	if len(m.queued) > 0 {
		next, drainCmd := c.flushQueuedInputs(queueDrainAuto)
		c.notifyTurnQueueDrainedIfIdle()
		return next, tea.Batch(queuedStateCmd, drainCmd, pendingWorkRefreshCmd)
	}
	c.notifyTurnQueueDrainedIfIdle()
	m.layout().syncViewport()
	return m, tea.Batch(queuedStateCmd, pendingWorkRefreshCmd)
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
		restoreInjectedCmd := c.restoreInjectedInputsIntoComposer()
		if errors.Is(msg.err, serverapi.ErrPendingWorkCapacity) {
			c.restoreSubmittedTextIntoInput(msg.submittedText)
		}
		c.restoreQueuedMessagesIntoInput()
		if isRuntimeOperationInterrupted(msg.err) {
			m.activity = uiActivityInterrupted
			m.logf("step.interrupted")
			m.layout().syncViewport()
			return m, tea.Batch(restoreInjectedCmd, m.interruptedStatusNoticeCmd())
		}
		detailErr := runtimeattach.FormatSubmissionError(msg.err)
		m.activity = uiActivityError
		appendCmd := m.appendLocalEntryWithNoticeID(operatorErrorFeedbackRole, detailErr, "")
		m.logf("compaction.error err=%q", detailErr)
		m.layout().syncViewport()
		return m, tea.Batch(restoreInjectedCmd, appendCmd)
	}
	pendingWorkRefreshCmd := tea.Batch(m.requestPendingWorkRefresh(msg.sessionID), m.acceptedSubmissionDiagnosticCmd(msg.diagnostic))

	if !serverActiveBeforeCompletion {
		m.activity = uiActivityIdle
	}
	m.logf("compaction.scheduled")
	if len(m.queued) > 0 {
		next, cmd := c.flushQueuedInputs(queueDrainAuto)
		c.notifyTurnQueueDrainedIfIdle()
		return next, tea.Batch(cmd, pendingWorkRefreshCmd)
	}
	if m.injectedQueueBlocksDrain() || m.hasEnqueuedInjectedRuntimeWork() {
		m.layout().syncViewport()
		return m, pendingWorkRefreshCmd
	}
	m.layout().syncViewport()
	return m, pendingWorkRefreshCmd
}

func (m *uiModel) shouldRestoreSubmittedTextOnSubmitError(err error) bool {
	if m == nil || err == nil {
		return false
	}
	if errors.Is(err, serverapi.ErrRuntimeCommandNotAccepted) {
		return true
	}
	if !m.hasRuntimeClient() {
		return true
	}
	return false
}

func isRuntimeOperationInterrupted(err error) bool {
	return errors.Is(err, runtimeattach.ErrSubmissionInterrupted) ||
		errors.Is(err, context.Canceled)
}
