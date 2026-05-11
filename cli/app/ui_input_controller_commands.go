package app

import (
	"strconv"
	"strings"

	"builder/cli/app/commands"
	"builder/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

func (c uiInputController) applyCommandResult(commandResult commands.Result) (tea.Model, tea.Cmd) {
	return c.applyCommandResultWithPreSubmitQueuePosition(commandResult, preSubmitQueueBack)
}

func (c uiInputController) applyQueuedCommandResult(commandResult commands.Result) (tea.Model, tea.Cmd) {
	return c.applyCommandResultWithPreSubmitQueuePosition(commandResult, preSubmitQueueFront)
}

func (c uiInputController) applyCommandResultWithPreSubmitQueuePosition(commandResult commands.Result, queuePosition preSubmitQueuePosition) (tea.Model, tea.Cmd) {
	m := c.model
	if commandResult.SubmitUser {
		if blocked, disconnectCmd := c.blockDisconnectedSubmission(true, commandResult.User); blocked {
			return m, disconnectCmd
		}
	}
	if commandResult.SubmitUser && commandResult.FreshConversation && m.currentConversationFreshness() != clientui.ConversationFreshnessFresh {
		m.nextSessionInitialPrompt = commandResult.User
		m.nextParentSessionID = m.sessionID
		m.exitAction = UIActionNewSession
		return m, tea.Quit
	}
	if commandResult.SubmitUser {
		return m, c.startSubmissionWithPreSubmitQueuePosition(commandResult.User, queuePosition, "")
	}
	prefixCmd := tea.Cmd(nil)
	if commandResult.Text != "" {
		prefixCmd = c.appendSystemFeedback(commandResult.Text)
	}

	switch commandResult.Action {
	case commands.ActionExit:
		m.exitAction = UIActionExit
		return m, sequenceCmds(prefixCmd, tea.Quit)
	case commands.ActionNew:
		m.nextParentSessionID = m.sessionID
		m.exitAction = UIActionNewSession
		return m, sequenceCmds(prefixCmd, tea.Quit)
	case commands.ActionResume:
		next, cmd := c.handleResumeCommand()
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionBack:
		next, cmd := c.handleBackCommand()
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionLogout:
		m.exitAction = UIActionLogout
		return m, sequenceCmds(prefixCmd, tea.Quit)
	case commands.ActionSetName:
		next, cmd := c.handleSessionNameCommand(commandResult.SessionName)
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionSetThinking:
		next, cmd := c.handleThinkingLevelCommand(commandResult.ThinkingLevel)
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionSetFast:
		next, cmd := c.handleFastModeCommand(commandResult.FastMode)
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionSetSupervisor:
		next, cmd := c.handleSupervisorModeCommand(commandResult.SupervisorMode)
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionSetAutoCompaction:
		next, cmd := c.handleAutoCompactionCommand(commandResult.AutoCompactionMode)
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionCompact:
		return m, sequenceCmds(prefixCmd, c.startCompaction(commandResult.Args))
	case commands.ActionStatus:
		return m, sequenceCmds(prefixCmd, c.startStatusFlowCmd())
	case commands.ActionGoal:
		next, cmd := c.handleGoalCommand(commandResult.GoalMode, commandResult.GoalObjective)
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionProcesses:
		args := strings.Fields(strings.TrimSpace(commandResult.Args))
		if len(args) == 0 {
			return m, sequenceCmds(prefixCmd, c.startProcessListFlowCmd())
		}
		action := strings.ToLower(strings.TrimSpace(args[0]))
		id := ""
		if len(args) > 1 {
			id = strings.TrimSpace(args[1])
		}
		next, cmd := c.runProcessAction(action, id)
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionWorktree:
		next, cmd := c.handleWorktreeCommand(commandResult.Args)
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionCopy:
		next, cmd := c.handleCopyCommand()
		return next, sequenceCmds(prefixCmd, cmd)
	}
	return m, prefixCmd
}

const resumeCommandUnavailableMessage = "No other sessions available"

func (c uiInputController) handleResumeCommand() (tea.Model, tea.Cmd) {
	m := c.model
	if !m.resumeCommandAvailable() {
		return m, c.appendErrorFeedbackWithStatus(resumeCommandUnavailableMessage, c.showErrorStatus(resumeCommandUnavailableMessage))
	}
	m.exitAction = UIActionResume
	return m, tea.Quit
}

func (c uiInputController) handleBackCommand() (tea.Model, tea.Cmd) {
	m := c.model
	status := m.runtimeStatus()
	if strings.TrimSpace(status.ParentSessionID) == "" {
		return m, c.appendSystemFeedback("No parent session available")
	}
	m.nextSessionInitialInput = m.backTeleportInput()
	m.nextSessionID = strings.TrimSpace(status.ParentSessionID)
	m.exitAction = UIActionOpenSession
	return m, tea.Quit
}

func (m *uiModel) backTeleportInput() string {
	return m.latestAssistantFinalAnswer()
}

func (m *uiModel) latestAssistantFinalAnswer() string {
	return m.latestAssistantFinalAnswerFromStatus(true)
}

func (m *uiModel) cachedLatestAssistantFinalAnswer() string {
	return m.latestAssistantFinalAnswerFromStatus(false)
}

func (m *uiModel) latestAssistantFinalAnswerFromStatus(refresh bool) string {
	if m.hasRuntimeClient() {
		status := m.cachedRuntimeStatus()
		if refresh {
			status = m.refreshRuntimeStatus()
		}
		if answer := strings.TrimSpace(status.LastCommittedAssistantFinalAnswer); answer != "" {
			return status.LastCommittedAssistantFinalAnswer
		}
		return ""
	}
	return localLastCommittedAssistantFinalAnswer(m.transcriptEntries)
}

func (m *uiModel) hasAssistantFinalAnswerToCopy() bool {
	return strings.TrimSpace(m.cachedLatestAssistantFinalAnswer()) != ""
}

func (c uiInputController) handleCopyCommand() (tea.Model, tea.Cmd) {
	m := c.model
	text := m.latestAssistantFinalAnswer()
	if strings.TrimSpace(text) == "" {
		return m, c.showErrorStatus("No final answer available to copy")
	}
	return m, m.copyClipboardTextCmd(text)
}

func (c uiInputController) handleSessionNameCommand(sessionName string) (tea.Model, tea.Cmd) {
	m := c.model
	if err := m.setRuntimeSessionName(sessionName); err != nil {
		return m, c.appendErrorFeedback(formatSubmissionError(err))
	}
	m.sessionName = strings.TrimSpace(sessionName)
	return m, tea.SetWindowTitle(m.windowTitle())
}

func (c uiInputController) handleThinkingLevelCommand(requested string) (tea.Model, tea.Cmd) {
	m := c.model
	requested = strings.TrimSpace(requested)
	if requested == "" {
		current := strings.TrimSpace(m.thinkingLevel)
		if m.hasRuntimeClient() {
			current = m.runtimeStatus().ThinkingLevel
		}
		if current == "" {
			current = "unknown"
		}
		return m, c.appendSystemFeedback("Thinking level is " + current)
	}

	normalized, ok := clientui.NormalizeThinkingLevel(requested)
	if !ok {
		errText := "invalid thinking level " + strconv.Quote(requested) + " (expected low|medium|high|xhigh)"
		return m, c.appendErrorFeedback(errText)
	}
	if err := m.setRuntimeThinkingLevel(normalized); err != nil {
		return m, c.appendErrorFeedback(formatSubmissionError(err))
	}
	if m.hasRuntimeClient() {
		m.thinkingLevel = m.runtimeStatus().ThinkingLevel
		return m, c.appendSystemFeedback("Thinking level set to " + m.thinkingLevel)
	}
	m.thinkingLevel = normalized
	return m, c.appendSystemFeedback("Thinking level set to " + m.thinkingLevel)
}

func (c uiInputController) handleFastModeCommand(requested string) (tea.Model, tea.Cmd) {
	m := c.model
	available, currentEnabled := m.fastModeState()
	if !available {
		errText := "Fast mode is only available for OpenAI-based Responses providers"
		return m, c.appendErrorFeedbackWithStatus(errText, c.showErrorStatus(errText))
	}

	requested = strings.ToLower(strings.TrimSpace(requested))
	switch requested {
	case "status":
		status := "off"
		if currentEnabled {
			status = "on"
		}
		return m, c.appendSystemFeedback("Fast mode is " + status)
	case "", "on", "off":
		// supported
	default:
		errText := "Usage: /fast [on|off|status]"
		return m, c.appendErrorFeedbackWithStatus(errText, c.showErrorStatus(errText))
	}

	targetEnabled := currentEnabled
	switch requested {
	case "":
		targetEnabled = !currentEnabled
	case "on":
		targetEnabled = true
	case "off":
		targetEnabled = false
	}

	changed := currentEnabled != targetEnabled
	if m.hasRuntimeClient() {
		var err error
		changed, err = m.setRuntimeFastModeEnabled(targetEnabled)
		if err != nil {
			detailErr := formatSubmissionError(err)
			return m, c.appendErrorFeedbackWithStatus(detailErr, c.showErrorStatus(detailErr))
		}
		m.fastModeEnabled = m.runtimeStatus().FastModeEnabled
	} else {
		m.fastModeEnabled = targetEnabled
	}

	status := fastModeToggleStatusMessage(m.fastModeEnabled, changed)
	return m, c.appendSystemFeedbackWithMirroredStatus(status, uiStatusNoticeSuccess)
}

func (c uiInputController) handleSupervisorModeCommand(requested string) (tea.Model, tea.Cmd) {
	m := c.model
	requested = strings.ToLower(strings.TrimSpace(requested))
	currentEnabled, currentMode := m.reviewerInvocationState()
	targetEnabled := currentEnabled
	switch requested {
	case "":
		targetEnabled = !currentEnabled
	case "on":
		targetEnabled = true
	case "off":
		targetEnabled = false
	default:
		errText := "invalid supervisor mode " + strconv.Quote(requested) + " (expected on|off)"
		return m, c.appendErrorFeedback(errText)
	}

	changed := false
	nextMode := currentMode
	if m.hasRuntimeClient() {
		var err error
		changed, nextMode, err = m.setRuntimeReviewerEnabled(targetEnabled)
		if err != nil {
			return m, c.appendErrorFeedback(formatSubmissionError(err))
		}
	} else {
		nextMode = "off"
		if targetEnabled {
			nextMode = "edits"
		}
		changed = currentEnabled != targetEnabled
	}
	m.reviewerMode = nextMode
	m.reviewerEnabled = nextMode != "off"
	status := reviewerToggleStatusMessage(m.reviewerEnabled, nextMode, changed)
	return m, c.appendSystemFeedbackWithMirroredStatus(status, uiStatusNoticeNeutral)
}

func (c uiInputController) handleAutoCompactionCommand(requested string) (tea.Model, tea.Cmd) {
	m := c.model
	requested = strings.ToLower(strings.TrimSpace(requested))
	currentEnabled := m.autoCompactionState()
	currentCompactionMode := "native"
	if m.hasRuntimeClient() {
		currentCompactionMode = m.runtimeStatus().CompactionMode
	}
	targetEnabled := currentEnabled
	switch requested {
	case "":
		targetEnabled = !currentEnabled
	case "on":
		targetEnabled = true
	case "off":
		targetEnabled = false
	default:
		errText := "invalid autocompaction mode " + strconv.Quote(requested) + " (expected on|off)"
		return m, c.appendErrorFeedback(errText)
	}

	changed := false
	nextEnabled := currentEnabled
	if m.hasRuntimeClient() {
		var err error
		changed, nextEnabled, err = m.setRuntimeAutoCompactionEnabled(targetEnabled)
		if err != nil {
			errText := formatSubmissionError(err)
			return m, c.appendErrorFeedbackWithStatus(errText, c.showTransientStatus(errText))
		}
	} else {
		nextEnabled = targetEnabled
		changed = currentEnabled != targetEnabled
	}
	m.autoCompactionEnabled = nextEnabled
	status := autoCompactionToggleStatusMessage(nextEnabled, changed, currentCompactionMode)
	return m, c.appendSystemFeedbackWithMirroredStatus(status, uiStatusNoticeNeutral)
}
