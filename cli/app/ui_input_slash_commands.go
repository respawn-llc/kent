package app

import (
	"fmt"
	"strings"

	"builder/cli/app/commands"

	tea "github.com/charmbracelet/bubbletea"
)

func (c uiInputController) handleQueuedSlashCommandInput(text string) (bool, tea.Model, tea.Cmd) {
	m := c.model
	selection := m.resolveSlashCommandSelection(text)
	if selection.shouldAutocomplete() {
		m.replaceMainInput(selection.autocompleteText(), -1)
		return true, m, nil
	}
	if !selection.hasCommand || selection.commandText() == "" {
		return false, m, nil
	}
	if errText, blocked := m.blockedDeferredSlashCommand(selection.commandText()); blocked {
		return true, m, c.appendErrorFeedbackWithStatus(errText, c.showErrorStatus(errText))
	}
	next, cmd := c.queueOrStartSubmission(selection.commandText())
	return true, next, cmd
}

func (c uiInputController) handleEnteredSlashCommandInput(text string) (bool, tea.Model, tea.Cmd) {
	m := c.model
	selection := m.resolveSlashCommandSelection(text)
	if !selection.hasCommand {
		return false, m, nil
	}
	commandText := selection.commandText()
	if commandText == "" {
		return false, m, nil
	}
	command := selection.command
	if m.isBusy() && !command.RunWhileBusy {
		m.clearInput()
		return true, m, c.showErrorStatus(fmt.Sprintf("cannot run /%s while model is working", command.Name))
	}
	if commandResult := m.commandRegistry.Execute(commandText); commandResult.Handled {
		draftText, draftCursor, restoreDraft := m.capturePromptHistoryDraftForReuse()
		recordCmd := m.recordPromptHistory(commandText)
		m.clearCommandInput(command, draftText, draftCursor, restoreDraft)
		next, cmd := c.applyCommandResult(commandResult)
		return true, next, finalizeSlashCommandCmd(commandResult.Action, cmd, recordCmd)
	}
	return false, m, nil
}

func (m *uiModel) clearCommandInput(command commands.Command, draftText string, draftCursor int, restoreDraft bool) {
	m.clearInput()
	if command.PreservePromptHistoryDraft {
		m.restoreCapturedPromptHistoryDraft(draftText, draftCursor, restoreDraft)
		return
	}
	m.resetPromptHistoryNavigation()
}

func finalizeSlashCommandCmd(action commands.Action, primary tea.Cmd, record tea.Cmd) tea.Cmd {
	if action == commands.ActionStatus {
		return tea.Batch(primary, record)
	}
	return sequenceCmds(record, primary)
}

func (m *uiModel) blockedDeferredSlashCommand(commandText string) (string, bool) {
	if m.commandRegistry == nil {
		return "", false
	}
	commandResult := m.commandRegistry.Execute(commandText)
	if !commandResult.Handled {
		return "", false
	}
	switch commandResult.Action {
	case commands.ActionResume:
		if !m.resumeCommandAvailable() {
			return resumeCommandUnavailableMessage, true
		}
	case commands.ActionBack:
		if !m.hasParentSession() {
			return "No parent session available", true
		}
	case commands.ActionSetFast:
		available, _ := m.fastModeState()
		if !available {
			return "Fast mode is only available for OpenAI-based Responses providers", true
		}
	case commands.ActionProcesses:
		args := strings.Fields(strings.TrimSpace(commandResult.Args))
		if len(args) > 0 && m.processClient == nil {
			return "background process client is unavailable", true
		}
	case commands.ActionWorktree:
		if m.isBusy() {
			return "cannot run /worktree while model is working", true
		}
		if m.worktreeClient == nil {
			return "worktree client is unavailable", true
		}
	}
	return "", false
}
