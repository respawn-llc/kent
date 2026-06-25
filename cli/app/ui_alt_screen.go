package app

import (
	"core/cli/tui"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

var writeTerminalSequence = func(sequence string) {
	_, _ = os.Stdout.WriteString(sequence)
}

func (m *uiModel) toggleTranscriptMode() tea.Cmd {
	target := tui.ModeDetail
	if m.view.Mode() == tui.ModeDetail {
		target = tui.ModeOngoing
	}
	return m.transitionTranscriptModeWithOptions(transcriptModeTransitionOptions{
		target:           target,
		skipDetailWarmup: false,
		emitNativeReplay: true,
	})
}

func (m *uiModel) toggleTranscriptModeWithNativeReplay(emitNativeReplay bool) tea.Cmd {
	target := tui.ModeDetail
	if m.view.Mode() == tui.ModeDetail {
		target = tui.ModeOngoing
	}
	return m.transitionTranscriptModeWithOptions(transcriptModeTransitionOptions{
		target:           target,
		skipDetailWarmup: false,
		emitNativeReplay: emitNativeReplay,
	})
}

type transcriptModeTransitionOptions struct {
	target            tui.Mode
	skipDetailWarmup  bool
	emitNativeReplay  bool
	suppressAltScreen bool
	preserveSurface   bool
}

func (m *uiModel) transitionTranscriptModeWithOptions(options transcriptModeTransitionOptions) tea.Cmd {
	prevMode := m.view.Mode()
	m.forwardToView(tui.SetModeMsg{Mode: options.target, SkipDetailWarmup: options.skipDetailWarmup})
	nextMode := m.view.Mode()
	if prevMode != nextMode && nextMode == tui.ModeDetail {
		m.primeDetailTranscriptFromCurrentTail()
	}
	if nextMode != tui.ModeOngoing {
		m.helpVisible = false
	} else if prevMode != nextMode && m.inputMode() == uiInputModeMain {
		m.restorePrimaryInputMode()
	}
	if prevMode != nextMode && nextMode == tui.ModeOngoing {
		m.syncRecentTailViewFromRuntimeState()
	}
	if !options.preserveSurface && (nextMode == tui.ModeOngoing || nextMode == tui.ModeDetail) {
		m.activeSurface = surfaceForTranscriptMode(nextMode)
	}
	clearCmd := m.clearCmdForModeTransition(prevMode, nextMode)
	transitionCmd := tea.Cmd(nil)
	if !options.suppressAltScreen {
		transitionCmd = m.altScreenCmdForModeTransition(prevMode, nextMode)
	}
	nativeReplayCmd := m.nativeReplayCmdForModeTransition(prevMode, nextMode, options.emitNativeReplay)
	detailLoadCmd := m.detailLoadCmdForModeTransition(prevMode, nextMode)
	if clearCmd == nil && transitionCmd == nil && nativeReplayCmd == nil && detailLoadCmd == nil {
		return nil
	}
	return sequenceCmds(clearCmd, transitionCmd, nativeReplayCmd, detailLoadCmd)
}

func (m *uiModel) syncRecentTailViewFromRuntimeState() {
	if m == nil || !m.hasRuntimeClient() {
		return
	}
	totalEntries := m.transcriptTotalEntries
	if totalEntries < m.transcriptBaseOffset+len(m.transcriptEntries) {
		totalEntries = m.transcriptBaseOffset + len(m.transcriptEntries)
	}
	m.forwardToView(tui.SetConversationMsg{
		BaseOffset:   m.transcriptBaseOffset,
		TotalEntries: totalEntries,
		Entries:      append([]tui.TranscriptEntry(nil), m.transcriptEntries...),
		Ongoing:      m.view.OngoingStreamingText(),
		OngoingError: m.view.OngoingErrorText(),
	})
}

func (m *uiModel) clearCmdForModeTransition(prev, next tui.Mode) tea.Cmd {
	if prev == next {
		return nil
	}
	if next != tui.ModeDetail {
		return nil
	}
	return nil
}

func (m *uiModel) detailLoadCmdForModeTransition(prev, next tui.Mode) tea.Cmd {
	if prev == next || next != tui.ModeDetail {
		return nil
	}
	m.detailTranscript.totalEntries = max(m.detailTranscript.totalEntries, m.view.TranscriptTotalEntries())
	return tea.Tick(time.Millisecond, func(time.Time) tea.Msg {
		return detailTranscriptLoadMsg{}
	})
}

func (m *uiModel) nativeReplayCmdForModeTransition(prev, next tui.Mode, enabled bool) tea.Cmd {
	if !enabled {
		return nil
	}
	if prev != tui.ModeDetail || next != tui.ModeOngoing {
		return nil
	}
	// Detail-mode transcript changes may append newly committed suffix rows on return.
	// If a spilled streaming assistant committed while detail was active, finalize that
	// deferred tail through the normal sync path; otherwise preserve append-only
	// delivery for deferred committed deltas.
	nativeCommittedEntries := committedNativeScrollbackEntriesForApp(m.transcriptEntries)
	committedEntries := nativeCommittedEntries.Entries
	if m.canFinalizeNativeStreamingCommit(nativeCommittedEntries, len(committedEntries)) {
		beforeSequence := m.nativeLastScheduledFlushSequence()
		cmd := m.syncNativeHistoryFromTranscript()
		if m.nativeScrollbackInvariantSet {
			return cmd
		}
		return sequenceCmds(cmd, m.trackOngoingCommittedFrontierFlush(committedOngoingLocalFrontierEnd(m), m.transcriptRevision, beforeSequence))
	}
	if len(committedEntries) > 0 && !m.nativeCurrentProjection().Empty() {
		projection := m.nativeCommittedProjection(committedEntries)
		currentProjection := m.nativeCurrentProjection()
		if _, ok := projection.RenderAppendDeltaFrom(currentProjection, tui.TranscriptDivider); !ok {
			return m.reportNativeProjectionDivergence(projection, currentProjection)
		}
	}
	if m.nativeCurrentProjection().Empty() && len(committedEntries) > 0 {
		projection := m.nativeCommittedProjection(committedEntries)
		m.rebaseNativeProjection(projection, m.transcriptBaseOffset, len(committedEntries))
		return m.emitCurrentNativeScrollbackStateAndTrackCommittedDelivery()
	}
	return m.emitCurrentNativeScrollbackStateAndTrackCommittedDelivery()
}

func sequenceCmds(cmds ...tea.Cmd) tea.Cmd {
	filtered := make([]tea.Cmd, 0, len(cmds))
	for _, cmd := range cmds {
		if cmd != nil {
			filtered = append(filtered, cmd)
		}
	}
	if len(filtered) == 0 {
		return nil
	}
	if len(filtered) == 1 {
		return filtered[0]
	}
	return tea.Sequence(filtered...)
}

func (m *uiModel) altScreenCmdForModeTransition(prev, next tui.Mode) tea.Cmd {
	if prev == next {
		return nil
	}
	return m.altScreenCmdForSurfaceTransition(surfaceForTranscriptMode(prev), surfaceForTranscriptMode(next))
}

func enableAlternateScrollCmd() tea.Cmd {
	return func() tea.Msg {
		writeTerminalSequence("\x1b[?1007h")
		return nil
	}
}

func disableAlternateScrollCmd() tea.Cmd {
	return func() tea.Msg {
		writeTerminalSequence("\x1b[?1007l")
		return nil
	}
}
