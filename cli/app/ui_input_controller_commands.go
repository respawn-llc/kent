package app

import chatsettingspb "core/shared/protoapi/gen/kent/api/chat_settings"

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

import (
	"strconv"
	"strings"

	"core/cli/app/commands"
	"core/shared/clientui"
	"core/shared/protoapi"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"

	tea "github.com/charmbracelet/bubbletea"
)

func (c uiInputController) applyCommandResult(
	commandResult commands.Result,
	submittedText string,
) (tea.Model, tea.Cmd) {
	m := c.model
	if commandResult.PromptCommand != nil {
		invocation := commandResult.PromptCommand
		canonical := runtimeinput.CanonicalCommandText(invocation.Name, invocation.Arguments)
		return m, c.startTypedSubmission(
			canonical,
			runtimeinput.Input{Kind: runtimeinput.KindPromptCommand, PromptCommand: invocation},
		)
	}
	if commandResult.SubmitUser {
		if blocked, disconnectCmd := c.blockDisconnectedSubmission(true, commandResult.User); blocked {
			return m, disconnectCmd
		}
	}
	if commandResult.SubmitUser && commandResult.FreshConversation && (m.isBusy() || m.currentConversationFreshness() != runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH) {
		previousSessionID, err := runtimeids.ParseSessionID(m.currentRuntimeSessionID())
		if err != nil {
			return m, c.model.appendLocalEntryWithNoticeID("error", "Current session identity is invalid: "+err.Error(), "")
		}
		m.nextSessionInitialPrompt = commandResult.User
		m.nextSessionInitialPromptHistoryRecorded = true
		m.nextPreviousSessionID = &previousSessionID
		m.exitAction = UIActionNewSession
		return m, tea.Quit
	}
	if commandResult.SubmitUser {
		return m, c.startSubmission(commandResult.User)
	}
	prefixCmd := tea.Cmd(nil)
	if commandResult.Text != "" {
		prefixCmd = c.model.appendLocalEntryWithNoticeID("system", commandResult.Text, "")
	}

	switch commandResult.Action {
	case commands.ActionExit:
		m.exitAction = UIActionExit
		return m, sequenceCmds(prefixCmd, tea.Quit)
	case commands.ActionNew:
		if m.newChatTarget() != nil {
			m.exitAction = UIActionNewSession
			return m, sequenceCmds(prefixCmd, tea.Quit)
		}
		previousSessionID, err := runtimeids.ParseSessionID(m.currentRuntimeSessionID())
		if err != nil {
			return m, sequenceCmds(prefixCmd, c.model.appendLocalEntryWithNoticeID("error", "Current session identity is invalid: "+err.Error(), ""))
		}
		m.nextPreviousSessionID = &previousSessionID
		m.exitAction = UIActionNewSession
		return m, sequenceCmds(prefixCmd, tea.Quit)
	case commands.ActionResume:
		next, cmd := c.handleResumeCommand()
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionBack:
		next, cmd := c.handleBackCommand()
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionLogin:
		if m.newSessionTarget() != nil {
			return m, sequenceCmds(prefixCmd, m.loginNewSessionCmd())
		}
		m.exitAction = UIActionLogout
		return m, sequenceCmds(prefixCmd, tea.Quit)
	case commands.ActionLogout:
		return m, sequenceCmds(prefixCmd, c.startConnectionRemovalFlowCmd())
	case commands.ActionSetName:
		next, cmd := c.handleSessionNameCommand(commandResult.Args)
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
	case commands.ActionSetQuestions:
		next, cmd := c.handleQuestionsCommand(commandResult.QuestionsMode)
		return next, sequenceCmds(prefixCmd, cmd)
	case commands.ActionCompact:
		return m, sequenceCmds(prefixCmd, c.startCompaction(submittedText, commandResult.Args))
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

func (c uiInputController) handleResumeCommand() (tea.Model, tea.Cmd) {
	m := c.model
	m.exitAction = UIActionResume
	return m, tea.Quit
}

func (c uiInputController) handleBackCommand() (tea.Model, tea.Cmd) {
	m := c.model
	if target := m.newChatTarget(); target != nil {
		if previous := target.GetOrigin().GetNavigationOnlySessionId(); previous != nil {
			m.nextSessionID = previous.GetSessionId()
			m.exitAction = UIActionOpenSession
			m.nextSessionInitialInput = nil
			return m, tea.Quit
		}
	}
	status := m.cachedRuntimeStatus()
	if status.NavigationTargetSessionId == nil {
		return m, c.model.appendLocalEntryWithNoticeID("system", "No parent session available", "")
	}
	if m.finalAnswerOperation != nil {
		return m, nil
	}
	return m, m.startFinalAnswerOperation(uiFinalAnswerOperationBack, *status.NavigationTargetSessionId)
}

func (c uiInputController) handleCopyCommand() (tea.Model, tea.Cmd) {
	m := c.model
	if m.newSessionTarget() != nil {
		return m, m.noFinalAnswerToCopyCmd()
	}
	if m.finalAnswerOperation != nil {
		return m, nil
	}
	return m, m.startFinalAnswerOperation(uiFinalAnswerOperationCopy, "")
}

func (c uiInputController) handleSessionNameCommand(sessionName string) (tea.Model, tea.Cmd) {
	m := c.model
	sessionName = strings.TrimSpace(sessionName)
	mutation := sessionNameCommandMutation(sessionName)
	if m.newSessionTarget() == nil {
		return m, m.runtimeControlCommand(runtimeControlSetSessionName, mutation)
	}
	if err := protoapi.Validate(mutation); err != nil {
		return m, m.sendTransientStatusWithNoticeID(err.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	name, err := protoapi.SessionNameFromMutation(mutation)
	if err != nil {
		return m, m.sendTransientStatusWithNoticeID(err.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	m.sessionName = name
	if target := m.newChatTarget(); target != nil {
		target.Name = name
	}
	return m, tea.SetWindowTitle(m.sessionTitle())
}

func (c uiInputController) handleThinkingLevelCommand(requested string) (tea.Model, tea.Cmd) {
	m := c.model
	requested = strings.TrimSpace(requested)
	if requested == "" {
		current := strings.TrimSpace(m.thinkingLevel)
		return m, c.model.sendThinkingLevelQueryStatus(current)
	}

	normalized, ok := clientui.NormalizeThinkingLevel(requested)
	if !ok {
		errText := "invalid thinking level " + strconv.Quote(requested) + " (expected none|low|medium|high|xhigh|max|ultra)"
		return m, m.sendTransientStatusWithNoticeID(errText, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	return m, m.chatSettingsMutationCommand(&chatsettingspb.MutationOperation{
		Operation: &chatsettingspb.MutationOperation_Thinking{Thinking: normalized},
	})
}

func (m *uiModel) sendThinkingLevelQueryStatus(level string) tea.Cmd {
	current := strings.TrimSpace(level)
	if current == "" {
		current = "unknown"
	}
	return m.sendTransientStatusWithNoticeID("Thinking level is "+current, uiStatusNoticeInfo, transientStatusDuration, uiStatusNoticeReplace, "")
}

func (c uiInputController) handleFastModeCommand(requested string) (tea.Model, tea.Cmd) {
	m := c.model
	requested = strings.ToLower(strings.TrimSpace(requested))
	available, currentEnabled := m.fastModeState()
	switch requested {
	case "status":
		if !available {
			return m, c.model.sendTransientStatusWithNoticeID("Fast mode is unavailable", uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
		}
		status := "off"
		if currentEnabled {
			status = "on"
		}
		return m, c.model.sendTransientStatusWithNoticeID("Fast: "+status, uiStatusNoticeInfo, transientStatusDuration, uiStatusNoticeReplace, "")
	case "", "on", "off":
		// supported
	default:
		errText := "Usage: /fast [on|off|status]"
		return m, c.model.sendTransientStatusWithNoticeID(errText, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	return m, m.chatSettingsToggleCommand(&chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_FastEnabled{}}, requested)
}

func (c uiInputController) handleSupervisorModeCommand(requested string) (tea.Model, tea.Cmd) {
	m := c.model
	requested = strings.ToLower(strings.TrimSpace(requested))
	switch requested {
	case "", "on", "off":
	default:
		errText := "invalid supervisor mode " + strconv.Quote(requested) + " (expected on|off)"
		return m, m.sendTransientStatusWithNoticeID(errText, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	if requested == "" || requested == "on" {
		return m, m.chatSettingsToggleCommand(&chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_Supervisor{}}, requested)
	}
	return m, m.chatSettingsMutationCommand(&chatsettingspb.MutationOperation{
		Operation: &chatsettingspb.MutationOperation_Supervisor{Supervisor: chatsettingspb.SupervisorValue_SUPERVISOR_VALUE_OFF},
	})
}

func (c uiInputController) handleQuestionsCommand(requested string) (tea.Model, tea.Cmd) {
	m := c.model
	requested = strings.ToLower(strings.TrimSpace(requested))
	switch requested {
	case "", "on", "off":
	default:
		errText := "invalid questions mode " + strconv.Quote(requested) + " (expected on|off)"
		return m, m.sendTransientStatusWithNoticeID(errText, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	return m, m.chatSettingsToggleCommand(&chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_QuestionsEnabled{}}, requested)
}

func (c uiInputController) handleAutoCompactionCommand(requested string) (tea.Model, tea.Cmd) {
	m := c.model
	requested = strings.ToLower(strings.TrimSpace(requested))
	switch requested {
	case "", "on", "off":
	default:
		errText := "invalid autocompaction mode " + strconv.Quote(requested) + " (expected on|off)"
		return m, m.sendTransientStatusWithNoticeID(errText, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	return m, m.chatSettingsToggleCommand(&chatsettingspb.MutationOperation{Operation: &chatsettingspb.MutationOperation_AutoCompactionEnabled{}}, requested)
}
