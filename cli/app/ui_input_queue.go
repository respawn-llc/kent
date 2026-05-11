package app

import (
	"strings"

	"builder/cli/app/commands"
	"builder/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/google/uuid"
)

type queueDrainMode uint8

const (
	queueDrainOne queueDrainMode = iota
	queueDrainAuto
)

type queuedInputItem struct {
	ID   string
	Text string
}

func (m *uiModel) queueInput(text string) {
	m.queued = append(m.queued, newQueuedInputItem(text))
	m.clearInput()
}

func newQueuedInputItem(text string) queuedInputItem {
	return queuedInputItem{ID: uuid.NewString(), Text: text}
}

func (m *uiModel) enqueueInjectedInput(text string) bool {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return false
	}
	item, err := m.queueRuntimeUserMessage(trimmed)
	if err != nil {
		m.observeRuntimeRequestResult(err)
		return false
	}
	m.pendingInjected = append(m.pendingInjected, item)
	return true
}

func (m *uiModel) queueInjectedInput(text string) {
	if !m.enqueueInjectedInput(text) {
		return
	}
	m.clearInput()
}

func (c uiInputController) queueOrStartSubmission(text string) (tea.Model, tea.Cmd) {
	m := c.model
	if m.isInputLocked() {
		return m, nil
	}
	if blocked, disconnectCmd := c.blockDisconnectedSubmission(false, ""); blocked {
		return m, disconnectCmd
	}
	draftText, draftCursor, restoreDraft := m.capturePromptHistoryDraftForReuse()
	m.queueInput(text)
	if c.preservePromptHistoryDraftForQueuedText(text) {
		m.restoreCapturedPromptHistoryDraft(draftText, draftCursor, restoreDraft)
	} else {
		m.resetPromptHistoryNavigation()
	}
	if m.isBusy() {
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

func (c uiInputController) blockDisconnectedSubmission(restoreHidden bool, submittedText string) (bool, tea.Cmd) {
	m := c.model
	if !m.runtimeDisconnectStatusVisible() {
		return false, nil
	}
	if restoreHidden {
		c.restorePendingInjectedIntoInput()
		c.restoreSubmittedTextIntoInput(submittedText)
		c.restoreQueuedMessagesIntoInput()
	}
	m.activity = uiActivityError
	m.syncViewport()
	return true, m.appendOperatorErrorFeedback(runtimeDisconnectedStatusMessage)
}

func (c uiInputController) restoreQueuedMessagesIntoInput() {
	m := c.model
	if len(m.queued) == 0 {
		return
	}
	joined := strings.Join(queuedInputTexts(m.queued), "\n\n")
	m.queued = nil
	newInput := joined
	if strings.TrimSpace(m.input) == "" {
		newInput = joined
	} else {
		newInput = strings.TrimRight(m.input, "\n") + "\n\n" + joined
	}
	m.replaceMainInput(newInput, -1)
}

func (c uiInputController) restoreSubmittedTextIntoInput(text string) {
	m := c.model
	submitted := strings.TrimSpace(text)
	if submitted == "" {
		return
	}
	newInput := submitted
	if strings.TrimSpace(m.input) != "" {
		newInput = strings.TrimRight(m.input, "\n") + "\n\n" + submitted
	}
	m.replaceMainInput(newInput, -1)
}

func (c uiInputController) restorePendingInjectedIntoInput() {
	m := c.model
	if len(m.pendingInjected) == 0 {
		return
	}
	pending := append([]clientui.QueuedUserMessage(nil), m.pendingInjected...)
	if m.hasRuntimeClient() {
		for _, item := range pending {
			m.discardQueuedRuntimeUserMessage(item.ID)
		}
	}
	joined := strings.Join(queuedUserMessageTexts(pending), "\n\n")
	m.pendingInjected = nil
	newInput := joined
	if strings.TrimSpace(m.input) == "" {
		newInput = joined
	} else {
		newInput = strings.TrimRight(m.input, "\n") + "\n\n" + joined
	}
	m.replaceMainInput(newInput, -1)
}

func (c uiInputController) unlockInputAfterSubmissionError() {
	c.releaseLockedInjectedInput(true)
}

func (c uiInputController) releaseLockedInjectedInput(discardEngineQueue bool) {
	m := c.model
	if !m.isInputSubmitLocked() {
		return
	}
	lockedID := strings.TrimSpace(m.lockedInjectID)
	if lockedID != "" {
		filtered := m.pendingInjected[:0]
		for _, pending := range m.pendingInjected {
			if pending.ID == lockedID {
				continue
			}
			filtered = append(filtered, pending)
		}
		m.pendingInjected = filtered
		if discardEngineQueue && m.hasRuntimeClient() {
			m.discardQueuedRuntimeUserMessage(lockedID)
		}
	}
	m.setInputSubmitLocked(false)
	m.lockedInjectText = ""
	m.lockedInjectID = ""
}

func (c uiInputController) flushQueuedInputs(mode queueDrainMode) (tea.Model, tea.Cmd) {
	m := c.model
	if len(m.queued) == 0 {
		return m, nil
	}
	if blocked, disconnectCmd := c.blockDisconnectedSubmission(true, ""); blocked {
		return m, disconnectCmd
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
	if m == nil || m.isBusy() || m.ask.hasCurrent() || len(m.queued) == 0 || m.isInputLocked() || m.pendingQueuedDrainAfterHydration {
		return nil
	}
	if m.hasRuntimeClient() && c.queuedDrainRequiresHydration() {
		m.pendingQueuedDrainAfterHydration = true
		m.queuedDrainReadyAfterHydration = false
		m.syncViewport()
		return m.requestRuntimeQueuedDrainTranscriptSync()
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
					return finalizeSlashCommandCmd(commandResult.Action, c.startQueuedCompaction(commandResult.Args), m.recordPromptHistory(text))
				}
				_, cmd := c.applyQueuedCommandResult(commandResult)
				return finalizeSlashCommandCmd(commandResult.Action, cmd, m.recordPromptHistory(text))
			}
		}
	}
	return c.startQueuedSubmissionWithPromptHistory(item)
}

func (m *uiModel) shouldContinueQueuedInputAutoDrain() bool {
	if len(m.queued) == 0 || m.isBusy() || m.isInputLocked() || m.exitAction != UIActionNone || m.ask.hasCurrent() {
		return false
	}
	if m.inputMode() != uiInputModeMain {
		return false
	}
	return strings.TrimSpace(m.input) == ""
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

func queuedUserMessageTexts(messages []clientui.QueuedUserMessage) []string {
	texts := make([]string, 0, len(messages))
	for _, message := range messages {
		texts = append(texts, message.Text)
	}
	return texts
}
