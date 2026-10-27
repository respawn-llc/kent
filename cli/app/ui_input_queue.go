package app

import transcriptpb "core/shared/protoapi/gen/kent/api/transcript"

import (
	"context"
	"errors"
	"slices"
	"sort"
	"strings"

	"core/cli/app/commands"
	"core/cli/app/internal/runtimeattach"
	"core/shared/clientui"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"google.golang.org/protobuf/proto"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

type queueDrainMode uint8

const (
	queueDrainOne queueDrainMode = iota
	queueDrainAuto
)

type queuedInputItem struct {
	ID              string
	Text            string
	submissionOrder inputSubmissionOrder
}

type inputSubmissionOrder struct {
	sequence uint64
}

type injectedRuntimeQueueState string

const (
	injectedRuntimeQueuePendingCreate        injectedRuntimeQueueState = "pendingCreate"
	injectedRuntimeQueueEnqueued             injectedRuntimeQueueState = "enqueued"
	injectedRuntimeQueueDiscardPending       injectedRuntimeQueueState = "discardPending"
	injectedRuntimeQueueCanceledBeforeCreate injectedRuntimeQueueState = "canceledBeforeCreate"
	injectedRuntimeQueueDiscardFailed        injectedRuntimeQueueState = "discardFailed"
)

type injectedRuntimeQueueItem struct {
	ServerID        *string
	Text            string
	State           injectedRuntimeQueueState
	submissionOrder inputSubmissionOrder
}

func (m *uiModel) queueInput(text string) {
	m.queued = append(m.queued, queuedInputItem{
		ID:              uuid.NewString(),
		Text:            text,
		submissionOrder: m.nextPendingInputSubmissionOrder(),
	})
	m.clearInput()
}

func (m *uiModel) nextPendingInputSubmissionOrder() inputSubmissionOrder {
	m.pendingInputSubmissionOrder++
	if m.pendingInputSubmissionOrder == 0 {
		panic("pending input submission order overflow")
	}
	return inputSubmissionOrder{sequence: m.pendingInputSubmissionOrder}
}

func (m *uiModel) registerSteeredQueuedUserMessage(queued clientui.QueuedUserMessage) bool {
	serverID := strings.TrimSpace(queued.ID)
	index := m.injectedQueueIndexByServerID(serverID)
	if index < 0 {
		return false
	}
	item := m.injectedQueue[index]
	item.ServerID = &serverID
	item.State = injectedRuntimeQueueEnqueued
	return true
}

func (m *uiModel) registerSubmittedQueuedUserMessage(queued clientui.QueuedUserMessage) tea.Cmd {
	if m.registerSteeredQueuedUserMessage(queued) {
		cmd, _ := m.applyUnownedQueuedTerminalState(queued.ID)
		return cmd
	}
	serverID := strings.TrimSpace(queued.ID)
	if strings.TrimSpace(m.activeSubmit.text) == "" {
		return nil
	}
	m.injectedQueue = append(m.injectedQueue, &injectedRuntimeQueueItem{
		ServerID:        &serverID,
		Text:            m.activeSubmit.text,
		State:           injectedRuntimeQueueEnqueued,
		submissionOrder: m.activeSubmit.submissionOrder,
	})
	cmd, _ := m.applyUnownedQueuedTerminalState(serverID)
	return cmd
}

func (m *uiModel) hasUnresolvedQueueOwnership() bool {
	if m == nil {
		return false
	}
	if m.activeSubmit.token != 0 {
		return true
	}
	for _, item := range m.injectedQueue {
		if item.State == injectedRuntimeQueuePendingCreate {
			return true
		}
	}
	return false
}

func (m *uiModel) retainUnownedQueuedTerminalState(state *transcriptpb.QueuedMessageState) {
	if m == nil || !m.hasUnresolvedQueueOwnership() ||
		state.Status == transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_ACCEPTED {
		return
	}
	serverID := strings.TrimSpace(state.QueueItemId)
	if m.unownedQueuedTerminalStates == nil {
		m.unownedQueuedTerminalStates = make(map[string]*transcriptpb.QueuedMessageState)
	}
	m.unownedQueuedTerminalStates[serverID] = proto.Clone(state).(*transcriptpb.QueuedMessageState)
}

func (m *uiModel) applyUnownedQueuedTerminalState(serverID string) (tea.Cmd, bool) {
	serverID = strings.TrimSpace(serverID)
	if m == nil || len(m.unownedQueuedTerminalStates) == 0 {
		return nil, false
	}
	state, ok := m.unownedQueuedTerminalStates[serverID]
	if !ok {
		return nil, false
	}
	delete(m.unownedQueuedTerminalStates, serverID)
	cmd := m.applyTranscriptQueuedMessageState(state)
	if !m.hasUnresolvedQueueOwnership() {
		m.unownedQueuedTerminalStates = nil
	}
	return cmd, true
}

func (m *uiModel) clearUnownedQueuedTerminalStatesWithoutPendingOwnership() {
	if m != nil && !m.hasUnresolvedQueueOwnership() {
		m.unownedQueuedTerminalStates = nil
	}
}

func (m *uiModel) enqueueInjectedInput(text string) tea.Cmd {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	submissionOrder := m.nextPendingInputSubmissionOrder()
	if m.newSessionTarget() != nil {
		m.injectedQueue = append(m.injectedQueue, &injectedRuntimeQueueItem{
			Text:            text,
			State:           injectedRuntimeQueueEnqueued,
			submissionOrder: submissionOrder,
		})
		return nil
	}
	pending := &injectedRuntimeQueueItem{
		Text:            text,
		State:           injectedRuntimeQueuePendingCreate,
		submissionOrder: submissionOrder,
	}
	m.injectedQueue = append(m.injectedQueue, pending)
	activation, err := chatActivationFromInput(runtimeinput.Text(text))
	if err != nil {
		panic(err)
	}
	execute := m.captureChatAction(chatInputAction{input: activation, queueItemID: runtimeids.NewQueueItemID()}, &text)
	return func() tea.Msg {
		submission, err := execute().submission(text)
		return injectedQueueCreateDoneMsg{
			pending:    pending,
			item:       submission.Queued,
			completed:  submission.Queued.ID == "",
			diagnostic: submission.Diagnostic,
			err:        err,
		}
	}
}

func (m *uiModel) queueInjectedInput(text string) tea.Cmd {
	cmd := m.enqueueInjectedInput(text)
	if strings.TrimSpace(text) == "" {
		return nil
	}
	m.clearInput()
	return cmd
}

func (c uiInputController) queueOrStartSubmission(text string) (tea.Model, tea.Cmd) {
	m := c.model
	if m.newSessionTarget() != nil {
		if blocked, cmd := m.gateNewSessionInput(text, newSessionInputQueue); blocked {
			return m, cmd
		}
		m.clearInput()
		return m, c.dispatchQueuedInput(queuedInputItem{
			Text: text, submissionOrder: m.nextPendingInputSubmissionOrder(),
		})
	}
	if blocked, blockCmd := c.blockInjectedQueueSubmission(); blocked {
		return m, blockCmd
	}
	if blocked, disconnectCmd := c.blockDisconnectedSubmission(false, ""); blocked {
		return m, disconnectCmd
	}
	draft := m.capturePromptHistoryDraftForReuse()
	m.queueInput(text)
	if c.preservePromptHistoryDraftForQueuedText(text) {
		m.restoreCapturedPromptHistoryDraft(draft)
	} else {
		m.resetPromptHistoryNavigation()
	}
	if m.blocksRuntimeInput() {
		return m, nil
	}
	return c.flushQueuedInputs(queueDrainOne)
}

func (c uiInputController) preservePromptHistoryDraftForQueuedText(text string) bool {
	if c.model == nil || c.model.commandRegistry == nil {
		return true
	}
	command, knownCommand := c.model.commandRegistry.Command(text)
	if !knownCommand {
		return true
	}
	return command.PreservePromptHistoryDraft
}

func (c uiInputController) blockInjectedQueueSubmission() (bool, tea.Cmd) {
	m := c.model
	if m == nil || !m.injectedQueueBlocksDrain() {
		return false, nil
	}
	detailErr := "queued runtime message is still pending; retry or discard it before submitting"
	m.activity = uiActivityError
	m.layout().syncViewport()
	return true, m.sendTransientStatusWithNoticeID(detailErr, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
}

func (c uiInputController) blockDisconnectedSubmission(restoreHidden bool, submittedText string) (bool, tea.Cmd) {
	m := c.model
	if !m.runtimeDisconnectStatusVisible() {
		return false, nil
	}
	if restoreHidden {
		restoreCmd := c.restoreInjectedInputsIntoComposer()
		c.restoreSubmittedTextIntoInput(submittedText)
		c.restoreQueuedMessagesIntoInput()
		m.activity = uiActivityError
		m.layout().syncViewport()
		return true, tea.Batch(restoreCmd, m.appendLocalEntryWithNoticeID(operatorErrorFeedbackRole, runtimeDisconnectedStatusMessage, ""))
	}
	m.activity = uiActivityError
	m.layout().syncViewport()
	return true, m.appendLocalEntryWithNoticeID(operatorErrorFeedbackRole, runtimeDisconnectedStatusMessage, "")
}

func (c uiInputController) blockDisconnectedQueuedSubmission() (bool, tea.Cmd) {
	m := c.model
	if !m.runtimeDisconnectStatusVisible() {
		return false, nil
	}
	c.restoreQueuedMessagesIntoInput()
	m.layout().syncViewport()
	statusCmd := m.sendTransientStatusWithNoticeID(runtimeDisconnectedStatusMessage, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	return true, statusCmd
}

func (c uiInputController) restoreQueuedMessagesIntoInput() {
	m := c.model
	if len(m.queued) == 0 {
		return
	}
	joined := strings.Join(queuedInputTexts(m.queued), "\n\n")
	m.queued = nil
	c.appendRestoredInputToComposer(joined)
}

func (c uiInputController) restoreSubmittedTextIntoInput(text string) {
	c.appendRestoredInputToComposer(text)
}

func (c uiInputController) appendRestoredInputToComposer(text string) {
	if strings.TrimSpace(text) == "" {
		return
	}
	m := c.model
	current := m.mainEditor.Text()
	if strings.TrimSpace(current) != "" {
		text = strings.TrimRight(current, "\n") + "\n\n" + text
	}
	m.replaceMainInputAtEnd(text)
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

func (c uiInputController) restoreInjectedInputsIntoComposer() tea.Cmd {
	m := c.model
	pending := m.restorableInjectedQueueItems()
	if len(pending) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(pending))
	for _, item := range pending {
		if cmd := m.markInjectedQueueDiscardRequested(item); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	texts := make([]string, 0, len(pending))
	for _, item := range pending {
		texts = append(texts, item.Text)
	}
	c.appendRestoredInputToComposer(strings.Join(texts, "\n\n"))
	return tea.Batch(cmds...)
}

func (m *uiModel) restorableInjectedQueueItems() []*injectedRuntimeQueueItem {
	if m == nil {
		return nil
	}
	items := make([]*injectedRuntimeQueueItem, 0, len(m.injectedQueue))
	for _, item := range m.injectedQueue {
		switch item.State {
		case injectedRuntimeQueuePendingCreate, injectedRuntimeQueueEnqueued:
			items = append(items, item)
		}
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].submissionOrder.sequence < items[j].submissionOrder.sequence
	})
	return items
}

type interruptedInputDraftPart struct {
	submissionOrder inputSubmissionOrder
	text            string
}

func (c uiInputController) restoreInterruptedInputsIntoComposer() tea.Cmd {
	m := c.model
	if m == nil {
		return nil
	}
	draft := m.mainEditor.Text()
	parts := make([]interruptedInputDraftPart, 0, len(m.injectedQueue)+len(m.queued))
	for _, item := range m.restorableInjectedQueueItems() {
		parts = append(parts, interruptedInputDraftPart{submissionOrder: item.submissionOrder, text: item.Text})
	}
	for _, queued := range m.queued {
		parts = append(parts, interruptedInputDraftPart{submissionOrder: queued.submissionOrder, text: queued.Text})
	}
	sort.SliceStable(parts, func(i, j int) bool {
		return parts[i].submissionOrder.sequence < parts[j].submissionOrder.sequence
	})
	texts := make([]string, 0, len(parts)+1)
	for _, part := range parts {
		texts = append(texts, part.text)
	}
	if draft != "" {
		texts = append(texts, draft)
	}
	m.injectedQueue = nil
	m.queued = nil
	if len(texts) == 0 {
		return nil
	}
	m.replaceMainInputAtEnd(strings.Join(texts, "\n\n"))
	m.logf("interrupt.restore pending_inputs=%d draft=%t", len(parts), draft != "")
	return nil
}

func (c uiInputController) flushQueuedInputs(mode queueDrainMode) (tea.Model, tea.Cmd) {
	m := c.model
	if len(m.queued) == 0 {
		return m, nil
	}
	if blocked, disconnectCmd := c.blockDisconnectedQueuedSubmission(); blocked {
		return m, disconnectCmd
	}
	if m.injectedQueueBlocksDrain() || m.hasEnqueuedInjectedRuntimeWork() {
		return m, nil
	}
	cmds := make([]tea.Cmd, 0, 2)
	for len(m.queued) > 0 {
		next := m.popQueued()
		if cmd := c.dispatchQueuedInput(next); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if mode == queueDrainOne || !m.shouldContinueQueuedInputAutoDrain() {
			break
		}
	}
	return m, tea.Batch(cmds...)
}

func (c uiInputController) resumeQueuedInputsAfterIdleRuntime() tea.Cmd {
	m := c.model
	if m == nil || m.blocksRuntimeInput() || m.ask.hasCurrent() ||
		m.processList.actionInFlight {
		return nil
	}
	if len(m.queued) == 0 {
		return nil
	}
	_, cmd := c.flushQueuedInputs(queueDrainAuto)
	c.notifyTurnQueueDrainedIfIdle()
	return cmd
}

func (c uiInputController) dispatchQueuedInput(item queuedInputItem) tea.Cmd {
	m := c.model
	text := item.Text
	if m.commandRegistry != nil {
		if _, knownCommand := m.commandRegistry.Command(text); knownCommand {
			if commandResult := m.commandRegistry.Execute(text); commandResult.Handled {
				if commandResult.Action == commands.ActionCompact {
					return finalizeSlashCommandCmd(commandResult.Action, c.startCompaction(text, commandResult.Args), m.recordPromptHistory(text))
				}
				_, cmd := c.applyCommandResultWithPreSubmitQueuePositionAndOriginAndOrder(
					commandResult,
					preSubmitQueueFront,
					activeSubmitOriginQueued,
					&item.submissionOrder,
					text,
				)
				var recordCmd tea.Cmd
				if commandResult.PromptCommand == nil {
					recordCmd = m.recordPromptHistory(text)
				}
				return finalizeSlashCommandCmd(commandResult.Action, cmd, recordCmd)
			}
		}
	}
	if cmd, rejected := c.rejectUnavailablePromptCommand(text); rejected {
		return cmd
	}
	m.rememberPromptHistoryLocally(item.Text)
	return c.startSubmissionWithPreSubmitQueuePositionAndOriginAndOrder(
		item.Text,
		preSubmitQueueFront,
		item.ID,
		activeSubmitOriginQueued,
		&item.submissionOrder,
	)
}

func (m *uiModel) shouldContinueQueuedInputAutoDrain() bool {
	if len(m.queued) == 0 || m.blocksRuntimeInput() ||
		m.exitAction != UIActionNone || m.ask.hasCurrent() || m.processList.actionInFlight {
		return false
	}
	if m.inputMode() != uiInputModeMain {
		return false
	}
	return strings.TrimSpace(m.mainEditor.Text()) == ""
}

func (m *uiModel) popQueued() queuedInputItem {
	if len(m.queued) == 0 {
		return queuedInputItem{}
	}
	next := m.queued[0]
	m.queued = m.queued[1:]
	return next
}

func (m *uiModel) discardQueuedInput(id string) bool {
	if strings.TrimSpace(id) == "" {
		return false
	}
	for i := 0; i < len(m.queued); i++ {
		if m.queued[i].ID != id {
			continue
		}
		m.queued = append(m.queued[:i], m.queued[i+1:]...)
		return true
	}
	return false
}

func queuedInputTexts(messages []queuedInputItem) []string {
	texts := make([]string, 0, len(messages))
	for _, message := range messages {
		texts = append(texts, message.Text)
	}
	return texts
}

func (m *uiModel) markInjectedQueueDiscardRequested(item *injectedRuntimeQueueItem) tea.Cmd {
	index := slices.Index(m.injectedQueue, item)
	if index < 0 {
		return nil
	}
	switch item.State {
	case injectedRuntimeQueuePendingCreate:
		item.State = injectedRuntimeQueueCanceledBeforeCreate
		return nil
	case injectedRuntimeQueueCanceledBeforeCreate, injectedRuntimeQueueDiscardPending:
		return nil
	case injectedRuntimeQueueEnqueued, injectedRuntimeQueueDiscardFailed:
		if item.ServerID == nil {
			m.removeInjectedQueueItemAt(index)
			return nil
		}
		item.State = injectedRuntimeQueueDiscardPending
		return m.discardInjectedRuntimeQueueCommand(item)
	default:
		return nil
	}
}

func (m *uiModel) discardInjectedRuntimeQueueCommand(item *injectedRuntimeQueueItem) tea.Cmd {
	if item.ServerID == nil {
		err := errors.New("accepted queued item requires a Runtime client and server identity")
		return m.handleFatalUIError(err.Error(), err)
	}
	serverID := *item.ServerID
	client, sessionID := m.pendingWorkClient, m.currentRuntimeSessionID()
	return func() tea.Msg {
		_, err := runtimeControlCall(func(ctx context.Context) (*runtimepb.RemovePendingWorkSuccess, error) {
			return client.RemovePendingWork(ctx, &runtimepb.RemovePendingWorkRequest{SessionId: sessionID, ItemId: serverID})
		})
		return injectedQueueDiscardDoneMsg{
			item:      item,
			discarded: err == nil,
		}
	}
}

func (m *uiModel) injectedQueueIndexByServerID(id string) int {
	id = strings.TrimSpace(id)
	for index, item := range m.injectedQueue {
		if item.ServerID != nil && *item.ServerID == id {
			return index
		}
	}
	return -1
}

func (m *uiModel) injectedQueueBlocksDrain() bool {
	if m == nil {
		return false
	}
	for _, item := range m.injectedQueue {
		switch item.State {
		case injectedRuntimeQueuePendingCreate, injectedRuntimeQueueCanceledBeforeCreate, injectedRuntimeQueueDiscardPending, injectedRuntimeQueueDiscardFailed:
			return true
		}
	}
	return false
}

func (m *uiModel) hasEnqueuedInjectedRuntimeWork() bool {
	if m == nil {
		return false
	}
	for _, item := range m.injectedQueue {
		if item.State == injectedRuntimeQueueEnqueued {
			return true
		}
	}
	return false
}

func (c uiInputController) handleInjectedQueueCreateDone(msg injectedQueueCreateDoneMsg) (tea.Model, tea.Cmd) {
	m := c.model
	index := slices.Index(m.injectedQueue, msg.pending)
	if index < 0 {
		return m, nil
	}
	item := m.injectedQueue[index]
	pendingWorkRefreshCmd := m.requestPendingWorkRefreshIfSuccessful(m.pendingWorkRefresh.sessionID, msg.err == nil)
	pendingWorkRefreshCmd = tea.Batch(pendingWorkRefreshCmd, m.acceptedSubmissionDiagnosticCmd(msg.diagnostic))
	m.observeRuntimeRequestResult(msg.err)
	if msg.err != nil {
		if item.State == injectedRuntimeQueuePendingCreate {
			c.restoreInjectedTextIntoInput(item.Text)
			detailErr := runtimeattach.FormatSubmissionError(msg.err)
			statusCmd := m.sendTransientStatusWithNoticeID(detailErr, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
			m.logf("queue_create.error err=%q", detailErr)
			m.removeInjectedQueueItemAt(index)
			m.clearUnownedQueuedTerminalStatesWithoutPendingOwnership()
			m.layout().syncViewport()
			return m, statusCmd
		}
		m.removeInjectedQueueItemAt(index)
		m.clearUnownedQueuedTerminalStatesWithoutPendingOwnership()
		return m, nil
	}
	if msg.completed {
		m.removeInjectedQueueItemAt(index)
		m.clearUnownedQueuedTerminalStatesWithoutPendingOwnership()
		if item.State != injectedRuntimeQueuePendingCreate {
			return m, pendingWorkRefreshCmd
		}
		m.rememberPromptHistoryLocally(item.Text)
		return m, pendingWorkRefreshCmd
	}
	serverID := strings.TrimSpace(msg.item.ID)
	item.ServerID = &serverID
	if item.State == injectedRuntimeQueuePendingCreate {
		m.rememberPromptHistoryLocally(item.Text)
	}
	if deferredCmd, consumed := m.applyUnownedQueuedTerminalState(serverID); consumed {
		return m, tea.Batch(deferredCmd, pendingWorkRefreshCmd)
	}
	switch item.State {
	case injectedRuntimeQueuePendingCreate:
		item.State = injectedRuntimeQueueEnqueued
	case injectedRuntimeQueueCanceledBeforeCreate:
		item.State = injectedRuntimeQueueDiscardPending
		return m, tea.Batch(
			m.discardInjectedRuntimeQueueCommand(item),
			pendingWorkRefreshCmd,
		)
	}
	m.clearUnownedQueuedTerminalStatesWithoutPendingOwnership()
	return m, pendingWorkRefreshCmd
}

func (c uiInputController) handleInjectedQueueDiscardDone(msg injectedQueueDiscardDoneMsg) (tea.Model, tea.Cmd) {
	m := c.model
	index := slices.Index(m.injectedQueue, msg.item)
	if index < 0 {
		return m, nil
	}
	item := m.injectedQueue[index]
	pendingWorkRefreshCmd := m.requestPendingWorkRefreshIfSuccessful(m.pendingWorkRefresh.sessionID, msg.discarded)
	if msg.discarded {
		m.removeInjectedQueueItemAt(index)
		return m, tea.Batch(
			c.resumeQueuedInputsAfterIdleRuntime(),
			pendingWorkRefreshCmd,
		)
	}
	item.State = injectedRuntimeQueueDiscardFailed
	detailErr := "failed to discard queued runtime user message"
	m.activity = uiActivityError
	appendCmd := m.appendLocalEntryWithNoticeID(operatorErrorFeedbackRole, detailErr, "")
	m.logf("queue_discard.error queue_item_id=%q", *item.ServerID)
	return m, appendCmd
}

func (c uiInputController) restoreInjectedTextIntoInput(text string) {
	c.appendRestoredInputToComposer(text)
}

func (m *uiModel) removeInjectedQueueItemAt(index int) {
	if index < 0 || index >= len(m.injectedQueue) {
		return
	}
	m.injectedQueue = append(m.injectedQueue[:index], m.injectedQueue[index+1:]...)
}

func (m *uiModel) removeInjectedQueueItemsByIDs(ids []string) {
	if len(ids) == 0 || len(m.injectedQueue) == 0 {
		return
	}
	filtered := m.injectedQueue[:0]
	for _, item := range m.injectedQueue {
		if item.ServerID != nil && containsInjectedQueueID(ids, *item.ServerID) {
			continue
		}
		filtered = append(filtered, item)
	}
	m.injectedQueue = filtered
}

func containsInjectedQueueID(values []string, target string) bool {
	target = strings.TrimSpace(target)
	for _, value := range values {
		if strings.TrimSpace(value) == target {
			return true
		}
	}
	return false
}
