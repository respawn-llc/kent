package app

import (
	"strings"

	"builder/cli/app/internal/worktreeselection"
	"builder/cli/app/internal/worktreeviewport"
	"builder/shared/serverapi"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *uiModel) worktreeRowCount() int {
	if m == nil {
		return 1
	}
	return worktreeselection.RowCount(m.worktrees.entries)
}

func (m *uiModel) clampWorktreeSelection() {
	if m == nil {
		return
	}
	m.worktrees.selection = worktreeselection.Clamp(m.worktrees.selection, m.worktrees.entries)
}

func (m *uiModel) moveWorktreeSelection(delta int) {
	if m == nil {
		return
	}
	m.worktrees.selection = worktreeselection.Move(m.worktrees.selection, m.worktrees.entries, delta)
}

func (m *uiModel) moveWorktreeSelectionPage(deltaPages int) {
	rows := m.worktreeRowsPerPage()
	m.moveWorktreeSelection(rows * deltaPages)
}

func (m *uiModel) worktreeRowsPerPage() int {
	return worktreeviewport.RowsPerPage(m.termHeight, worktreeOverlayHeaderLines, worktreeOverlayFooterLines, worktreeOverlayRowLines)
}

func (m *uiModel) selectFirstWorktreeRow() {
	if m == nil {
		return
	}
	m.worktrees.selection = 0
}

func (m *uiModel) selectLastWorktreeRow() {
	if m == nil {
		return
	}
	m.worktrees.selection = max(0, m.worktreeRowCount()-1)
}

func (m *uiModel) selectedWorktreeRow() (serverapi.WorktreeView, bool) {
	if m == nil {
		return serverapi.WorktreeView{}, false
	}
	return worktreeselection.SelectedWorktree(m.worktrees.entries, m.worktrees.selection)
}

func (m *uiModel) selectedWorktreeID() string {
	if m == nil {
		return worktreeCreateRowID
	}
	return worktreeselection.SelectedID(m.worktrees.entries, m.worktrees.selection)
}

func (m *uiModel) recordWorktreeSelection() {
	if m == nil {
		return
	}
	m.worktrees.selectedID = m.selectedWorktreeID()
}

func (m *uiModel) restoreWorktreeSelection() {
	if m == nil {
		return
	}
	m.worktrees.selection = worktreeselection.Restore(m.worktrees.entries, m.worktrees.selection, m.worktrees.selectedID)
}

func (c uiInputController) startWorktreeOverlayCmd(intent uiWorktreeOpenIntent) tea.Cmd {
	m := c.model
	m.openWorktreeOverlay(intent)
	refreshCmd := m.requestWorktreeListCmd()
	spinnerCmd := m.ensureSpinnerTicking()
	if overlayCmd := m.pushWorktreeOverlayIfNeeded(); overlayCmd != nil {
		return tea.Batch(overlayCmd, refreshCmd, spinnerCmd)
	}
	return tea.Batch(refreshCmd, spinnerCmd)
}

func (c uiInputController) stopWorktreeOverlayCmd() tea.Cmd {
	m := c.model
	if m.worktrees.switchPending {
		return nil
	}
	overlayCmd := m.popWorktreeOverlayIfNeeded()
	m.closeWorktreeOverlay()
	spinnerCmd := m.ensureSpinnerTicking()
	if overlayCmd != nil {
		return tea.Batch(overlayCmd, spinnerCmd)
	}
	return spinnerCmd
}

func (c uiInputController) handleWorktreeOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	m := c.model
	if m.worktrees.phase == uiWorktreeOverlayPhaseCreate {
		return c.handleWorktreeCreateDialogKey(msg)
	}
	if m.worktrees.phase == uiWorktreeOverlayPhaseDeleteConfirm {
		return c.handleWorktreeDeleteDialogKey(msg)
	}
	switch strings.ToLower(msg.String()) {
	case "ctrl+c":
		if m.busy {
			c.interruptBusyRuntime()
			return m, nil
		}
		m.exitAction = UIActionExit
		if overlayCmd := m.popWorktreeOverlayIfNeeded(); overlayCmd != nil {
			m.closeWorktreeOverlay()
			return m, tea.Sequence(overlayCmd, tea.Quit)
		}
		return m, tea.Quit
	case "esc", "q":
		return m, c.stopWorktreeOverlayCmd()
	case "up", "k":
		m.moveWorktreeSelection(-1)
		return m, nil
	case "down", "j":
		m.moveWorktreeSelection(1)
		return m, nil
	case "pgup":
		m.moveWorktreeSelectionPage(-1)
		return m, nil
	case "pgdown":
		m.moveWorktreeSelectionPage(1)
		return m, nil
	case "home":
		m.selectFirstWorktreeRow()
		return m, nil
	case "end":
		m.selectLastWorktreeRow()
		return m, nil
	case "r":
		return m, tea.Batch(m.requestWorktreeListCmd(), m.ensureSpinnerTicking())
	case "c", "n":
		return m, m.openCreateWorktreeDialog()
	case "d":
		target, ok := m.selectedWorktreeRow()
		if !ok {
			return m, c.showErrorStatus("Select a worktree to delete")
		}
		if target.IsMain {
			return m, c.showErrorStatus("Main workspace is not deletable")
		}
		m.worktrees.intent = uiWorktreeOpenIntent{OpenDelete: true, ConfirmDeleteTarget: target.WorktreeID}
		return m, tea.Batch(m.requestWorktreeListCmd(), m.ensureSpinnerTicking())
	case "x":
		target, ok := m.selectedWorktreeRow()
		if !ok {
			return m, c.showErrorStatus("Select a worktree to delete")
		}
		if target.IsMain {
			return m, c.showErrorStatus("Main workspace is not deletable")
		}
		m.worktrees.intent = uiWorktreeOpenIntent{OpenDelete: true, ConfirmDeleteTarget: target.WorktreeID, PreferDeleteBranch: true}
		return m, tea.Batch(m.requestWorktreeListCmd(), m.ensureSpinnerTicking())
	case "enter":
		if m.worktrees.selection == 0 {
			return m, m.openCreateWorktreeDialog()
		}
		target, ok := m.selectedWorktreeRow()
		if !ok {
			return m, nil
		}
		if target.IsCurrent {
			return m, c.showTransientStatus("Already current worktree")
		}
		return m, m.worktreeSwitchCmd(target)
	default:
		return m, nil
	}
}
