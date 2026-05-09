package app

import (
	"strings"

	"builder/cli/app/internal/worktreemutation"
	"builder/cli/app/internal/worktreeview"
	"builder/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
)

func (c uiInputController) handleWorktreeCommand(args string) (tea.Model, tea.Cmd) {
	m := c.model
	if m.worktreeClient == nil {
		errText := "worktree client is unavailable"
		return m, c.appendErrorFeedbackWithStatus(errText, c.showErrorStatus(errText))
	}
	parts := strings.Fields(strings.TrimSpace(args))
	if len(parts) == 0 {
		return m, c.startWorktreeOverlayCmd(uiWorktreeOpenIntent{})
	}
	subcommand := strings.ToLower(strings.TrimSpace(parts[0]))
	switch subcommand {
	case "status":
		if len(parts) != 1 {
			return m, c.appendErrorFeedbackWithStatus(worktreeUsage(), c.showErrorStatus(worktreeUsage()))
		}
		return m, c.startWorktreeOverlayCmd(uiWorktreeOpenIntent{})
	case "new", "create":
		if len(parts) != 1 {
			return m, c.appendErrorFeedbackWithStatus(worktreeUsage(), c.showErrorStatus(worktreeUsage()))
		}
		return m, c.startWorktreeOverlayCmd(uiWorktreeOpenIntent{OpenCreate: true})
	case "switch":
		if len(parts) != 2 {
			return m, c.appendErrorFeedbackWithStatus(worktreeUsage(), c.showErrorStatus(worktreeUsage()))
		}
		return c.handleWorktreeSwitchCommand(parts[1])
	case "delete", "remove", "rm":
		if len(parts) > 2 {
			return m, c.appendErrorFeedbackWithStatus(worktreeUsage(), c.showErrorStatus(worktreeUsage()))
		}
		token := ""
		if len(parts) == 2 {
			token = parts[1]
		}
		return m, c.startWorktreeOverlayCmd(uiWorktreeOpenIntent{OpenDelete: true, ConfirmDeleteTarget: token})
	default:
		return m, c.appendErrorFeedbackWithStatus(worktreeUsage(), c.showErrorStatus(worktreeUsage()))
	}
}

func (c uiInputController) handleWorktreeSwitchCommand(token string) (tea.Model, tea.Cmd) {
	m := c.model
	resolved, err := m.resolveWorktreeToken(token)
	if err != nil {
		errText := formatSubmissionError(err)
		return m, c.appendErrorFeedbackWithStatus(errText, c.showErrorStatus(errText))
	}
	resp, err := m.worktreeMutationService().Switch(resolved.WorktreeID)
	if err != nil {
		errText := formatSubmissionError(err)
		return m, c.appendErrorFeedbackWithStatus(errText, c.showErrorStatus(errText))
	}
	status := "Switched to " + worktreeDisplayName(resp.Worktree)
	return m, tea.Batch(c.showSuccessStatus(status), m.requestRuntimeMainViewRefresh())
}

func (m *uiModel) listWorktreesForCurrentSession(includeDirtyCount bool) (serverapi.WorktreeListResponse, error) {
	if m == nil {
		return serverapi.WorktreeListResponse{}, worktreemutation.ErrClientUnavailable
	}
	return m.worktreeMutationService().List(includeDirtyCount)
}

func (m *uiModel) resolveWorktreeToken(token string) (serverapi.WorktreeView, error) {
	if m == nil {
		return serverapi.WorktreeView{}, worktreemutation.ErrClientUnavailable
	}
	return m.worktreeMutationService().ResolveToken(token)
}

func (m *uiModel) suggestedWorktreeSessionName() string {
	if trimmed := strings.TrimSpace(m.sessionName); trimmed != "" {
		return trimmed
	}
	if client := m.runtimeClient(); client != nil {
		return strings.TrimSpace(client.SessionView().SessionName)
	}
	return ""
}

func isWorktreeMutationCommand(command string) bool {
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "new", "create", "switch", "delete", "remove", "rm":
		return true
	default:
		return false
	}
}

func worktreeUsage() string {
	return "Usage: /wt | /wt status | /wt create | /wt new | /wt delete [target] | /wt remove [target] | /wt rm [target] | /wt switch <target>"
}

func worktreeDisplayName(item serverapi.WorktreeView) string {
	return worktreeview.DisplayName(item)
}

func sanitizeWorktreeBranchSuggestion(raw string) string {
	return worktreeview.SanitizeBranchSuggestion(raw)
}

func worktreeDeleteCanAutoDeleteBranch(item serverapi.WorktreeView) bool {
	return worktreeview.DeleteCanAutoDeleteBranch(item)
}
