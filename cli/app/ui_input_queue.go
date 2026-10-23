package app

import (
	"context"
	"errors"
	"strings"

	"core/cli/app/internal/runtimeattach"
	"core/shared/clientui"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"

	tea "github.com/charmbracelet/bubbletea"
)

type injectedRuntimeQueueState string

const (
	injectedRuntimeQueuePendingCreate injectedRuntimeQueueState = "pendingCreate"
	injectedRuntimeQueueEnqueued      injectedRuntimeQueueState = "enqueued"
)

// Admission records correlate transport and transcript results; they never
// schedule, retry, or own accepted Runtime work.
type injectedRuntimeQueueItem struct {
	Request   clientui.RuntimeSubmitRequest
	SessionID string
	Text      string
	State     injectedRuntimeQueueState
	Terminal  *transcriptpb.QueuedMessageState
}

func (m *uiModel) enqueueInput(text string, input runtimeinput.Input, intent runtimeinput.PendingWorkLane) tea.Cmd {
	if command, shell := parseUserShellCommand(text); shell {
		input = runtimeinput.Shell(command)
	}
	pending := &injectedRuntimeQueueItem{
		Request: clientui.RuntimeSubmitRequest{
			Input: input, Intent: intent, QueueItemID: runtimeids.NewQueueItemID(),
		},
		SessionID: m.sessionID,
		Text:      text, State: injectedRuntimeQueuePendingCreate,
	}
	m.injectedQueue = append(m.injectedQueue, pending)
	client := m.runtimeClient()
	command := func() tea.Msg {
		if client == nil {
			return injectedQueueCreateDoneMsg{pending: pending, err: errors.New("runtime engine is not configured")}
		}
		submission, err := client.SubmitRuntimeInput(context.Background(), pending.Request)
		return injectedQueueCreateDoneMsg{
			pending: pending, selectedSessionID: submission.SessionID,
			item: submission.Queued, diagnostic: submission.Diagnostic, err: err,
		}
	}
	return tea.Batch(command, m.reconcileSpinnerTicking(false))
}

func (m *uiModel) hasPendingInputAdmission() bool {
	for _, item := range m.injectedQueue {
		if item.State == injectedRuntimeQueuePendingCreate {
			return true
		}
	}
	return false
}

func (c uiInputController) queueOrStartSubmission(text string) (tea.Model, tea.Cmd) {
	m := c.model
	if blocked, cmd := c.blockDisconnectedSubmission(false, ""); blocked {
		return m, cmd
	}
	draft := m.capturePromptHistoryDraftForReuse()
	m.clearInput()
	m.restoreCapturedPromptHistoryDraft(draft)
	return m, m.enqueueInput(text, runtimeinput.Text(text), runtimeinput.PendingWorkLaneQueue)
}

func (c uiInputController) blockDisconnectedSubmission(restoreHidden bool, submittedText string) (bool, tea.Cmd) {
	m := c.model
	if !m.runtimeDisconnectStatusVisible() {
		return false, nil
	}
	if restoreHidden {
		c.restoreSubmittedTextIntoInput(submittedText)
	}
	m.activity = uiActivityError
	m.layout().syncViewport()
	return true, m.appendLocalEntryWithNoticeID(operatorErrorFeedbackRole, runtimeDisconnectedStatusMessage, "")
}

func (c uiInputController) restoreSubmittedTextIntoInput(text string) {
	c.appendRestoredInputToComposer(text)
}

func (c uiInputController) appendRestoredInputToComposer(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	current := c.model.mainEditor.Text()
	if strings.TrimSpace(current) != "" {
		text = strings.TrimRight(current, "\n") + "\n\n" + text
	}
	c.model.replaceMainInputAtEnd(text)
}

func (c uiInputController) restoreServerOrderedTextBeforeComposer(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	current := c.model.mainEditor.Text()
	if strings.TrimSpace(current) != "" {
		text = strings.TrimRight(text, "\n") + "\n\n" + current
	}
	c.model.replaceMainInputAtEnd(text)
}

func (c uiInputController) handleInjectedQueueCreateDone(msg injectedQueueCreateDoneMsg) (tea.Model, tea.Cmd) {
	m := c.model
	index := m.injectedQueueIndexByServerID(msg.pending.Request.QueueItemID.String())
	if index < 0 {
		return m, nil
	}
	item := m.injectedQueue[index]
	if item.SessionID != m.sessionID {
		m.removeInjectedQueueItemAt(index)
		return m, nil
	}
	m.observeRuntimeRequestResult(msg.err)
	if msg.err != nil {
		var terminalCmd tea.Cmd
		if item.Terminal != nil {
			item.State = injectedRuntimeQueueEnqueued
			terminalCmd = m.applyTranscriptQueuedMessageState(item.Terminal)
		} else {
			c.restoreSubmittedTextIntoInput(item.Text)
			m.removeInjectedQueueItemAt(index)
		}
		detail := runtimeattach.FormatSubmissionError(msg.err)
		m.logf("input_admission.error err=%q", detail)
		status := m.sendTransientStatusWithNoticeID(detail, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
		if notFound, ok := promptCommandNotFound(msg.err); ok && notFound.Command != nil {
			return m, tea.Batch(terminalCmd, status, m.startPromptCatalogRefresh(*notFound.Command))
		}
		return m, tea.Batch(terminalCmd, status)
	}
	if msg.selectedSessionID != nil && *msg.selectedSessionID != m.sessionID {
		m.removeInjectedQueueItemAt(index)
		m.nextSessionID = *msg.selectedSessionID
		m.exitAction = UIActionOpenSession
		if msg.diagnostic != nil {
			return m, tea.Sequence(m.appendLocalEntryWithNoticeID("error", msg.diagnostic.Error(), ""), tea.Quit)
		}
		return m, tea.Quit
	}
	if msg.item.ID != item.Request.QueueItemID.String() {
		err := errors.New("accepted input changed its Queue Item identity")
		return m, m.handleFatalUIError(err.Error(), err)
	}
	item.State = injectedRuntimeQueueEnqueued
	m.rememberAcceptedInput(item)
	var terminalCmd tea.Cmd
	if item.Terminal != nil {
		terminalCmd = m.applyTranscriptQueuedMessageState(item.Terminal)
	}
	return m, tea.Batch(terminalCmd, m.requestPendingWorkRefreshIfSuccessful(m.pendingWorkRefresh.sessionID, true),
		m.acceptedSubmissionDiagnosticCmd(msg.diagnostic), m.reconcileSpinnerTicking(false))
}

func (m *uiModel) rememberAcceptedInput(item *injectedRuntimeQueueItem) {
	switch item.Request.Input.Kind {
	case runtimeinput.KindText:
		m.rememberPromptHistoryLocally(item.Text)
		m.conversationFreshness = runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED
	case runtimeinput.KindPromptCommand:
		m.rememberPromptCommandHistoryLocally(item.Request.Input.CanonicalHistoryText())
		m.conversationFreshness = runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED
	case runtimeinput.KindShell:
	}
}

func (m *uiModel) injectedQueueIndexByServerID(id string) int {
	for index, item := range m.injectedQueue {
		if item.Request.QueueItemID.String() == id {
			return index
		}
	}
	return -1
}

func (m *uiModel) removeInjectedQueueItemAt(index int) {
	m.injectedQueue = append(m.injectedQueue[:index], m.injectedQueue[index+1:]...)
}
