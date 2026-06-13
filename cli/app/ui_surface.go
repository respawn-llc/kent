package app

import (
	"core/cli/tui"

	tea "github.com/charmbracelet/bubbletea"
)

type uiSurface string

const (
	uiSurfaceOngoingTranscript uiSurface = "ongoing"
	uiSurfaceTranscriptDetail  uiSurface = "transcript_detail"
	uiSurfaceStatus            uiSurface = "status"
	uiSurfaceGoal              uiSurface = "goal"
	uiSurfaceWorktree          uiSurface = "worktree"
	uiSurfaceProcessList       uiSurface = "process_list"
	uiSurfaceRollbackSelection uiSurface = "rollback_selection"
)

func (m *uiModel) surface() uiSurface {
	if m == nil || m.activeSurface == "" {
		return surfaceForTranscriptMode(m.transcriptMode())
	}
	return m.activeSurface
}

func (m *uiModel) transcriptMode() tui.Mode {
	if m == nil {
		return tui.ModeOngoing
	}
	return m.view.Mode()
}

func surfaceForTranscriptMode(mode tui.Mode) uiSurface {
	if mode == tui.ModeDetail {
		return uiSurfaceTranscriptDetail
	}
	return uiSurfaceOngoingTranscript
}

func (surface uiSurface) isTranscript() bool {
	return surface == uiSurfaceOngoingTranscript || surface == uiSurfaceTranscriptDetail || surface == ""
}

func (surface uiSurface) wantsAltScreen() bool {
	switch surface {
	case uiSurfaceTranscriptDetail, uiSurfaceStatus, uiSurfaceGoal, uiSurfaceWorktree, uiSurfaceProcessList, uiSurfaceRollbackSelection:
		return true
	default:
		return false
	}
}

func (m *uiModel) surfaceWantsAlternateScroll(surface uiSurface) bool {
	switch surface {
	case uiSurfaceTranscriptDetail:
		return true
	case uiSurfaceStatus, uiSurfaceGoal, uiSurfaceWorktree, uiSurfaceProcessList:
		return m.transcriptMode() == tui.ModeDetail
	default:
		return false
	}
}

func (m *uiModel) restoreTranscriptSurface() tea.Cmd {
	prev := m.surface()
	next := surfaceForTranscriptMode(m.view.Mode())
	transitionCmd := m.activateSurface(next)
	if !prev.isTranscript() && next == uiSurfaceOngoingTranscript {
		return sequenceCmds(transitionCmd, m.emitCurrentNativeScrollbackState(false))
	}
	return transitionCmd
}

func (m *uiModel) activateSurface(surface uiSurface) tea.Cmd {
	if surface == "" {
		surface = surfaceForTranscriptMode(m.view.Mode())
	}
	prev := m.surface()
	m.activeSurface = surface
	if prev == surface {
		return nil
	}
	return m.altScreenCmdForSurfaceTransition(prev, surface)
}

func (m *uiModel) altScreenCmdForSurfaceTransition(prev, next uiSurface) tea.Cmd {
	prevWantsAlt := prev.wantsAltScreen()
	nextWantsAlt := next.wantsAltScreen()
	if !prevWantsAlt && nextWantsAlt && !m.altScreenActive {
		m.altScreenActive = true
		if m.surfaceWantsAlternateScroll(next) {
			return tea.Sequence(tea.EnterAltScreen, enableAlternateScrollCmd())
		}
		return tea.EnterAltScreen
	}
	if prevWantsAlt && !nextWantsAlt && m.altScreenActive {
		m.altScreenActive = false
		if m.surfaceWantsAlternateScroll(prev) {
			return tea.Sequence(disableAlternateScrollCmd(), tea.ExitAltScreen)
		}
		return tea.ExitAltScreen
	}
	if prevWantsAlt && nextWantsAlt && m.altScreenActive {
		prevWantsAlternateScroll := m.surfaceWantsAlternateScroll(prev)
		nextWantsAlternateScroll := m.surfaceWantsAlternateScroll(next)
		if prevWantsAlternateScroll == nextWantsAlternateScroll {
			return nil
		}
		if nextWantsAlternateScroll {
			return enableAlternateScrollCmd()
		}
		return disableAlternateScrollCmd()
	}
	return nil
}
