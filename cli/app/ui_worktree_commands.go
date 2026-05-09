package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"unicode"

	"builder/shared/serverapi"
	"github.com/google/uuid"

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
	resp, err := runWorktreeMutation(m, func(ctx context.Context, leaseID string) (serverapi.WorktreeSwitchResponse, error) {
		return m.worktreeClient.SwitchWorktree(ctx, serverapi.WorktreeSwitchRequest{
			ClientRequestID:   uuid.NewString(),
			SessionID:         m.sessionID,
			ControllerLeaseID: leaseID,
			WorktreeID:        resolved.WorktreeID,
		})
	})
	if err != nil {
		errText := formatSubmissionError(err)
		return m, c.appendErrorFeedbackWithStatus(errText, c.showErrorStatus(errText))
	}
	status := "Switched to " + worktreeDisplayName(resp.Worktree)
	return m, tea.Batch(c.showSuccessStatus(status), m.requestRuntimeMainViewRefresh())
}

func (m *uiModel) listWorktreesForCurrentSession(includeDirtyCount bool) (serverapi.WorktreeListResponse, error) {
	if m == nil || m.worktreeClient == nil {
		return serverapi.WorktreeListResponse{}, fmt.Errorf("worktree client is unavailable")
	}
	client, ok := m.runtimeClient().(*sessionRuntimeClient)
	if !ok || client == nil {
		return serverapi.WorktreeListResponse{}, errors.New("controller lease is unavailable")
	}
	ctx, cancel := client.controlContext()
	defer cancel()
	return m.worktreeClient.ListWorktrees(ctx, serverapi.WorktreeListRequest{SessionID: m.sessionID, ControllerLeaseID: client.controllerLeaseIDValue(), IncludeDirtyCount: includeDirtyCount})
}

func (m *uiModel) resolveWorktreeToken(token string) (serverapi.WorktreeView, error) {
	resp, err := m.listWorktreesForCurrentSession(false)
	if err != nil {
		return serverapi.WorktreeView{}, err
	}
	return resolveWorktreeTokenFromEntries(resp.Worktrees, token)
}

func runWorktreeMutation[T any](m *uiModel, call func(ctx context.Context, controllerLeaseID string) (T, error)) (T, error) {
	var zero T
	client, ok := m.runtimeClient().(*sessionRuntimeClient)
	if !ok || client == nil {
		return zero, errors.New("controller lease is unavailable")
	}
	ctx, cancel := client.controlContext()
	defer cancel()
	return retryRuntimeControlCall(ctx, client.controllerLeaseIDValue, client.recoverControllerLease, func(controllerLeaseID string) (T, error) {
		return call(ctx, controllerLeaseID)
	})
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
	if trimmed := strings.TrimSpace(item.DisplayName); trimmed != "" {
		return trimmed
	}
	if item.IsMain {
		return "main"
	}
	if trimmed := strings.TrimSpace(item.BranchName); trimmed != "" {
		return trimmed
	}
	if trimmed := strings.TrimSpace(item.CanonicalRoot); trimmed != "" {
		return filepath.Base(trimmed)
	}
	return strings.TrimSpace(item.WorktreeID)
}

func sanitizeWorktreeBranchSuggestion(raw string) string {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return ""
	}
	var builder strings.Builder
	lastDash := false
	for _, r := range trimmed {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			builder.WriteRune(r)
			lastDash = false
		case r == '/' || r == '-' || r == '_':
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteRune('-')
			lastDash = true
		default:
			if builder.Len() == 0 || lastDash {
				continue
			}
			builder.WriteRune('-')
			lastDash = true
		}
	}
	result := strings.Trim(builder.String(), "-/")
	for strings.Contains(result, "--") {
		result = strings.ReplaceAll(result, "--", "-")
	}
	return result
}

func worktreeDeleteCanAutoDeleteBranch(item serverapi.WorktreeView) bool {
	return item.BuilderManaged && item.CreatedBranch && strings.TrimSpace(item.BranchName) != ""
}
