package app

import (
	"fmt"
	"strings"

	"builder/cli/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *uiModel) syncNativeHistoryFromTranscript() tea.Cmd {
	if !m.windowSizeKnown {
		return nil
	}
	committedEntries := committedTranscriptEntriesForApp(m.transcriptEntries)
	if len(committedEntries) == 0 {
		hasPendingTransientTail := len(tui.PendingOngoingEntries(m.transcriptEntries)) > 0
		alreadyReplayed := m.nativeHistoryReplayed
		m.resetNativeHistoryState()
		m.nativeHistoryReplayed = true
		if hasPendingTransientTail || alreadyReplayed || !m.shouldEmitNativeHistory() {
			return m.sequenceNativeStreamingScrollback(nil)
		}
		return m.sequenceNativeStreamingScrollback(m.emitCurrentNativeScrollbackState(false))
	}

	committedCount := len(committedEntries)
	projection := m.nativeCommittedProjection(committedEntries)
	if m.nativeFlushedEntryCount < 0 || m.nativeFlushedEntryCount > committedCount {
		m.rebaseNativeProjection(projection, m.transcriptBaseOffset, committedCount)
		return m.sequenceNativeStreamingScrollback(nil)
	}
	if !m.shouldEmitNativeHistory() && m.canFinalizeNativeStreamingCommit(committedEntries, committedCount) {
		return nil
	}
	if cmd, handled := m.finalizeNativeStreamingCommit(projection, committedEntries, committedCount); handled {
		return cmd
	}
	if !m.nativeHistoryReplayed || m.nativeProjection.Empty() {
		m.rebaseNativeProjection(projection, m.transcriptBaseOffset, committedCount)
		if !m.shouldEmitNativeHistory() {
			return nil
		}
		return m.sequenceNativeStreamingScrollback(m.emitCurrentNativeScrollbackState(false))
	}
	previousProjection := m.nativeRenderedProjection
	previousBaseOffset := m.nativeRenderedBaseOffset
	if previousProjection.Empty() {
		previousProjection = m.nativeProjection
		previousBaseOffset = m.nativeProjectionBaseOffset
	}
	previousBlockCount := len(previousProjection.Blocks)
	delta, ok := projection.RenderAppendDeltaFrom(previousProjection, tui.TranscriptDivider)
	m.rebaseNativeProjection(projection, m.transcriptBaseOffset, committedCount)
	if !m.shouldEmitNativeHistory() {
		return nil
	}
	replayPermit := m.consumeNativeHistoryReplayPermit()
	if !ok {
		if appendCmd, appended := m.emitNativeSlidingWindowAppend(projection, previousProjection, m.transcriptBaseOffset, previousBaseOffset); appended {
			return m.sequenceNativeStreamingScrollback(appendCmd)
		}
		if appendCmd, appended := m.emitNativePostRewriteVisibleAppend(projection, previousProjection); appended {
			return m.sequenceNativeStreamingScrollback(appendCmd)
		}
		if replayPermit == nativeHistoryReplayPermitContinuityRecovery {
			return m.sequenceNativeStreamingScrollback(m.emitNonContiguousNativeProjectionRecovery(projection, previousProjection))
		}
		if replayPermit == nativeHistoryReplayPermitModeRestore {
			m.acceptNativeProjectionWithoutReplay(projection)
			return m.sequenceNativeStreamingScrollback(nil)
		}
		if replayPermit == nativeHistoryReplayPermitAuthoritativeHydrate {
			m.acceptNativeProjectionWithoutReplay(projection)
			return m.sequenceNativeStreamingScrollback(nil)
		}
		m.acceptNativeProjectionWithoutReplay(projection)
		return m.sequenceNativeStreamingScrollback(m.reportNativeProjectionDivergence(projection, previousProjection))
	}
	if strings.TrimSpace(delta) == "" {
		return m.sequenceNativeStreamingScrollback(nil)
	}
	m.nativeRenderedProjection = projection
	m.nativeRenderedSnapshot = projection.Render(tui.TranscriptDivider)
	return m.sequenceNativeStreamingScrollback(m.emitNativeRenderedText(renderStyledNativeProjectionLines(projection.LinesFromBlock(previousBlockCount, tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth())))
}

func (m *uiModel) canFinalizeNativeStreamingCommit(committedEntries []tui.TranscriptEntry, committedCount int) bool {
	return m.nativeStreamingCommitAssistantIndex(committedEntries, committedCount) >= 0
}

func (m *uiModel) nativeStreamingCommitAssistantIndex(committedEntries []tui.TranscriptEntry, committedCount int) int {
	if m == nil {
		return -1
	}
	if strings.TrimSpace(m.view.OngoingStreamingText()) != "" {
		return -1
	}
	if strings.TrimSpace(m.nativeStreamingController.source) == "" {
		return -1
	}
	previousCommittedCount := m.nativeFlushedEntryCount
	if previousCommittedCount < 0 || previousCommittedCount > committedCount {
		return -1
	}
	source := strings.TrimSpace(m.nativeStreamingController.source)
	for idx := previousCommittedCount; idx < committedCount && idx < len(committedEntries); idx++ {
		entry := committedEntries[idx]
		if entry.Role == tui.TranscriptRoleAssistant && strings.TrimSpace(entry.Text) == source {
			return idx
		}
	}
	return -1
}

func (m *uiModel) shouldEmitNativeHistory() bool {
	return m.windowSizeKnown && m.view.Mode() == tui.ModeOngoing
}

func (m *uiModel) nativeReplayRenderWidth() int {
	if m.termWidth > 0 {
		return m.termWidth
	}
	if m.nativeReplayWidth > 0 {
		return m.nativeReplayWidth
	}
	return 120
}

func (m *uiModel) resetNativeHistoryState() {
	m.nativeFlushedEntryCount = 0
	m.nativeHistoryReplayed = false
	m.nativeCommittedProjector = tui.CommittedOngoingProjector{}
	m.nativeProjection = tui.TranscriptProjection{}
	m.nativeProjectionBaseOffset = 0
	m.nativeRenderedProjection = tui.TranscriptProjection{}
	m.nativeRenderedBaseOffset = 0
	m.nativeRenderedSnapshot = ""
	m.nativeHistoryReplayPermit = nativeHistoryReplayPermitNone
	m.waitRuntimeEventAfterFlushSequence = 0
	m.resetNativeStreamingState()
	m.discardPendingNativeHistoryFlushes()
}

func (m *uiModel) resetNativeStreamingState() {
	m.nativeStreamingController = newNativeAssistantStreamController(m.theme, m.nativeReplayRenderWidth())
	m.nativeStreamingTail = nil
	m.nativeStreamingUnflushedStable = nil
	m.nativeStreamingStableFlushSequence = 0
	m.nativeStreamingText = ""
	m.nativeStreamingWidth = 0
	m.nativeStreamingFlushedLineCount = 0
	m.nativeStreamingDividerFlushed = false
}

func (m *uiModel) sequenceNativeStreamingScrollback(cmd tea.Cmd) tea.Cmd {
	return sequenceCmds(cmd, m.syncNativeStreamingScrollback())
}

func (m *uiModel) syncNativeStreamingScrollback() tea.Cmd {
	if m == nil || !m.shouldEmitNativeHistory() {
		return nil
	}
	streamText, ok := m.activeNativeStreamingText()
	if !ok {
		m.resetNativeStreamingState()
		return nil
	}
	width := m.nativeReplayRenderWidth()
	update := m.nativeStreamingController.ApplySource(streamText, m.theme, width)
	m.nativeStreamingText = m.nativeStreamingController.source
	m.nativeStreamingWidth = width
	m.nativeStreamingFlushedLineCount = m.nativeStreamingController.enqueuedStableLineCount
	if len(update.stable) > 0 {
		m.nativeStreamingUnflushedStable = append(m.nativeStreamingUnflushedStable, update.stable...)
	}
	m.nativeStreamingTail = m.nativeStreamingLiveTail(update.tail)
	if len(update.stable) == 0 {
		return nil
	}
	lines := make([]tui.TranscriptProjectionLine, 0, len(update.stable)+1)
	if len(committedTranscriptEntriesForApp(m.transcriptEntries)) > 0 && !m.nativeStreamingDividerFlushed {
		lines = append(lines, tui.TranscriptProjectionLine{Kind: tui.VisibleLineDivider, Text: tui.TranscriptDivider})
		m.nativeStreamingDividerFlushed = true
	}
	lines = append(lines, update.stable...)
	cmd := m.emitNativeRenderedText(renderStyledNativeProjectionLines(lines, m.theme, width))
	if cmd != nil {
		m.nativeStreamingStableFlushSequence = m.nativeFlushSequence
	}
	return cmd
}

func (m *uiModel) activeNativeStreamingText() (string, bool) {
	if m == nil {
		return "", false
	}
	streamText := m.view.OngoingStreamingText()
	if strings.TrimSpace(streamText) == "" {
		return "", false
	}
	// Authoritative ongoing-tail hydrates can populate streaming text before the
	// reviewer/run-state flags or live assistant delta marker are set.
	return streamText, true
}

func (m *uiModel) finalizeNativeStreamingCommit(projection tui.TranscriptProjection, committedEntries []tui.TranscriptEntry, committedCount int) (tea.Cmd, bool) {
	streamedAssistantIndex := m.nativeStreamingCommitAssistantIndex(committedEntries, committedCount)
	if streamedAssistantIndex < 0 {
		if m != nil && strings.TrimSpace(m.nativeStreamingController.source) == "" {
			m.resetNativeStreamingState()
		}
		return nil, false
	}
	previousCommittedCount := m.nativeFlushedEntryCount
	newEntries := committedEntries[previousCommittedCount:]
	if len(newEntries) == 0 {
		m.resetNativeStreamingState()
		return nil, false
	}
	if m.nativeStreamingController.invalidatedByResize {
		m.consumeNativeHistoryReplayPermit()
		m.rebaseNativeProjection(projection, m.transcriptBaseOffset, committedCount)
		m.acceptNativeProjectionWithoutReplay(projection)
		m.resetNativeStreamingState()
		return m.emitCurrentNativeScrollbackState(true), true
	}
	hadCommittedHistory := previousCommittedCount > 0
	finalUpdate := m.nativeStreamingController.Finalize()
	flushTail := m.emitNativeRenderedText(renderStyledNativeProjectionLines(m.nativeStreamingFinalizeLines(finalUpdate.stable, hadCommittedHistory), m.theme, m.nativeReplayRenderWidth()))
	postAssistant := m.emitNativeProjectionLinesForEntryRangeExcluding(projection, previousCommittedCount, committedCount, streamedAssistantIndex)
	m.consumeNativeHistoryReplayPermit()
	m.rebaseNativeProjection(projection, m.transcriptBaseOffset, committedCount)
	m.acceptNativeProjectionWithoutReplay(projection)
	m.resetNativeStreamingState()
	return sequenceCmds(flushTail, postAssistant), true
}

func (m *uiModel) nativeStreamingFinalizeLines(stable []tui.TranscriptProjectionLine, hadCommittedHistory bool) []tui.TranscriptProjectionLine {
	if m == nil {
		return nil
	}
	lines := make([]tui.TranscriptProjectionLine, 0, len(stable)+1)
	if hadCommittedHistory && !m.nativeStreamingDividerFlushed {
		lines = append(lines, tui.TranscriptProjectionLine{Kind: tui.VisibleLineDivider, Text: tui.TranscriptDivider})
	}
	lines = append(lines, stable...)
	return lines
}

func (m *uiModel) nativeStreamingLiveTail(tail []tui.TranscriptProjectionLine) []tui.TranscriptProjectionLine {
	if len(m.nativeStreamingUnflushedStable) == 0 {
		return cloneNativeStreamProjectionLines(tail)
	}
	lines := make([]tui.TranscriptProjectionLine, 0, len(m.nativeStreamingUnflushedStable)+len(tail))
	lines = append(lines, m.nativeStreamingUnflushedStable...)
	lines = append(lines, tail...)
	return lines
}

func (m *uiModel) emitNativeProjectionLinesAfterEntry(projection tui.TranscriptProjection, entryIndex int) tea.Cmd {
	if entryIndex < 0 {
		entryIndex = 0
	}
	startAfter := m.transcriptBaseOffset + entryIndex
	startBlock := -1
	for idx, block := range projection.Blocks {
		if block.EntryIndex > startAfter {
			startBlock = idx
			break
		}
	}
	if startBlock < 0 {
		return nil
	}
	styled := renderStyledNativeProjectionLines(projection.LinesFromBlock(startBlock, tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth())
	if strings.TrimSpace(styled) == "" {
		return nil
	}
	return m.emitNativeRenderedText(styled)
}

func (m *uiModel) emitNativeProjectionLinesForEntryRangeExcluding(projection tui.TranscriptProjection, startIndex int, endIndex int, excludedIndex int) tea.Cmd {
	if m == nil || startIndex >= endIndex {
		return nil
	}
	startAbsolute := m.transcriptBaseOffset + startIndex
	endAbsolute := m.transcriptBaseOffset + endIndex
	excludedAbsolute := m.transcriptBaseOffset + excludedIndex
	lines := make([]tui.TranscriptProjectionLine, 0, len(projection.Blocks)*2)
	previousGroup := string(tui.RenderIntentAssistant)
	for _, block := range projection.Blocks {
		if block.EntryEnd < startAbsolute || block.EntryIndex >= endAbsolute {
			continue
		}
		if block.EntryIndex <= excludedAbsolute && block.EntryEnd >= excludedAbsolute {
			continue
		}
		if previousGroup != "" && previousGroup != block.DividerGroup {
			lines = append(lines, tui.TranscriptProjectionLine{Kind: tui.VisibleLineDivider, Text: tui.TranscriptDivider})
		}
		previousGroup = block.DividerGroup
		for _, line := range block.Lines {
			lines = append(lines, tui.TranscriptProjectionLine{Kind: tui.VisibleLineContent, Text: line})
		}
	}
	styled := renderStyledNativeProjectionLines(lines, m.theme, m.nativeReplayRenderWidth())
	if strings.TrimSpace(styled) == "" {
		return nil
	}
	return m.emitNativeRenderedText(styled)
}

func (m *uiModel) armNativeHistoryReplayPermit(permit nativeHistoryReplayPermit) {
	if m == nil || permit == nativeHistoryReplayPermitNone {
		return
	}
	if permit == nativeHistoryReplayPermitContinuityRecovery {
		m.nativeHistoryReplayPermit = permit
		return
	}
	if m.nativeHistoryReplayPermit == nativeHistoryReplayPermitContinuityRecovery {
		return
	}
	if permit == nativeHistoryReplayPermitModeRestore {
		m.nativeHistoryReplayPermit = permit
		return
	}
	if m.nativeHistoryReplayPermit == nativeHistoryReplayPermitModeRestore {
		return
	}
	m.nativeHistoryReplayPermit = permit
}

func (m *uiModel) consumeNativeHistoryReplayPermit() nativeHistoryReplayPermit {
	if m == nil {
		return nativeHistoryReplayPermitNone
	}
	permit := m.nativeHistoryReplayPermit
	m.nativeHistoryReplayPermit = nativeHistoryReplayPermitNone
	return permit
}

func (m *uiModel) acceptNativeProjectionWithoutReplay(projection tui.TranscriptProjection) {
	m.nativeRenderedProjection = projection
	m.nativeRenderedBaseOffset = m.nativeProjectionBaseOffset
	m.nativeRenderedSnapshot = projection.Render(tui.TranscriptDivider)
}

func (m *uiModel) reportNativeProjectionDivergence(current tui.TranscriptProjection, rendered tui.TranscriptProjection) tea.Cmd {
	if m.debugMode {
		panic(fmt.Sprintf("same-session committed transcript divergence requires root-cause fix: rendered_blocks=%d current_blocks=%d", len(rendered.Blocks), len(current.Blocks)))
	}
	m.logf("ui.native_history.divergence_detected rendered_blocks=%d current_blocks=%d", len(rendered.Blocks), len(current.Blocks))
	return nil
}

func (m *uiModel) rebaseNativeProjection(projection tui.TranscriptProjection, baseOffset int, committedCount int) {
	m.nativeProjection = projection
	m.nativeProjectionBaseOffset = baseOffset
	m.nativeFlushedEntryCount = committedCount
	m.nativeHistoryReplayed = true
}

func (m *uiModel) emitCurrentNativeScrollbackState(forceFull bool) tea.Cmd {
	replayPermit := m.consumeNativeHistoryReplayPermit()
	if !m.nativeProjection.Empty() {
		return m.emitCurrentNativeHistorySnapshot(forceFull, replayPermit)
	}
	return m.emitEmptyNativeScrollbackSpacer(forceFull)
}

func (m *uiModel) emitEmptyNativeScrollbackSpacer(forceFull bool) tea.Cmd {
	spacer := m.nativeEmptyScrollbackSpacerText()
	if spacer == "" {
		if forceFull {
			return tea.ClearScreen
		}
		return nil
	}
	flush := m.emitNativeHistoryFlush(spacer, true)
	if !forceFull {
		return flush
	}
	return tea.Sequence(tea.ClearScreen, flush)
}

func (m *uiModel) nativeEmptyScrollbackSpacerText() string {
	if !m.windowSizeKnown || m.termHeight <= 0 {
		return ""
	}
	return strings.Repeat("\n", m.termHeight)
}

func (m *uiModel) emitCurrentNativeHistorySnapshot(forceFull bool, replayPermit nativeHistoryReplayPermit) tea.Cmd {
	rawSnapshot := m.nativeProjection.Render(tui.TranscriptDivider)
	if strings.TrimSpace(rawSnapshot) == "" {
		return nil
	}
	if forceFull {
		styled := renderStyledNativeProjection(m.nativeProjection, m.theme, m.nativeReplayRenderWidth())
		if strings.TrimSpace(styled) == "" {
			return nil
		}
		m.nativeRenderedProjection = m.nativeProjection
		m.nativeRenderedSnapshot = rawSnapshot
		return tea.Sequence(tea.ClearScreen, m.emitNativeRenderedText(styled))
	}
	rewriteRenderedHistory := m.view.Mode() == tui.ModeOngoing && !m.nativeRenderedProjection.Empty()
	if !m.nativeRenderedProjection.Empty() {
		previousBlockCount := len(m.nativeRenderedProjection.Blocks)
		delta, ok := m.nativeProjection.RenderAppendDeltaFrom(m.nativeRenderedProjection, tui.TranscriptDivider)
		delta = strings.TrimPrefix(delta, "\n")
		if ok && strings.TrimSpace(delta) != "" {
			styledDelta := renderStyledNativeProjectionLines(m.nativeProjection.LinesFromBlock(previousBlockCount, tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth())
			m.nativeRenderedProjection = m.nativeProjection
			m.nativeRenderedSnapshot = rawSnapshot
			if strings.TrimSpace(styledDelta) != "" {
				return m.emitNativeRenderedText(styledDelta)
			}
		}
		if ok && strings.TrimSpace(delta) == "" {
			m.nativeRenderedProjection = m.nativeProjection
			m.nativeRenderedBaseOffset = m.nativeProjectionBaseOffset
			m.nativeRenderedSnapshot = rawSnapshot
			return nil
		}
		if appendCmd, appended := m.emitNativeSlidingWindowAppend(m.nativeProjection, m.nativeRenderedProjection, m.nativeProjectionBaseOffset, m.nativeRenderedBaseOffset); appended {
			return appendCmd
		}
		if appendCmd, appended := m.emitNativePostRewriteVisibleAppend(m.nativeProjection, m.nativeRenderedProjection); appended {
			return appendCmd
		}
		if rewriteRenderedHistory {
			if replayPermit == nativeHistoryReplayPermitContinuityRecovery {
				return m.emitNonContiguousNativeProjectionRecovery(m.nativeProjection, m.nativeRenderedProjection)
			}
			if replayPermit == nativeHistoryReplayPermitModeRestore {
				m.acceptNativeProjectionWithoutReplay(m.nativeProjection)
				return nil
			}
			if replayPermit == nativeHistoryReplayPermitAuthoritativeHydrate {
				m.acceptNativeProjectionWithoutReplay(m.nativeProjection)
				return nil
			}
			m.acceptNativeProjectionWithoutReplay(m.nativeProjection)
			return m.reportNativeProjectionDivergence(m.nativeProjection, m.nativeRenderedProjection)
		}
		forceFull = true
	}
	if !forceFull {
		if deltaRaw, ok := nativeRenderedDelta(m.nativeRenderedSnapshot, rawSnapshot); ok {
			styledDelta := styleNativeReplayDividers(deltaRaw, m.theme, m.nativeReplayRenderWidth())
			m.nativeRenderedProjection = m.nativeProjection
			m.nativeRenderedBaseOffset = m.nativeProjectionBaseOffset
			m.nativeRenderedSnapshot = rawSnapshot
			if strings.TrimSpace(styledDelta) == "" {
				return nil
			}
			return m.emitNativeRenderedText(styledDelta)
		}
	}
	if rewriteRenderedHistory {
		return nil
	}
	styled := renderStyledNativeProjection(m.nativeProjection, m.theme, m.nativeReplayRenderWidth())
	if strings.TrimSpace(styled) == "" {
		return nil
	}
	m.nativeRenderedProjection = m.nativeProjection
	m.nativeRenderedBaseOffset = m.nativeProjectionBaseOffset
	m.nativeRenderedSnapshot = rawSnapshot
	if forceFull {
		return tea.Sequence(tea.ClearScreen, m.emitNativeRenderedText(styled))
	}
	return m.emitNativeRenderedText(styled)
}

func (m *uiModel) emitNativeSlidingWindowAppend(current tui.TranscriptProjection, rendered tui.TranscriptProjection, currentBaseOffset int, renderedBaseOffset int) (tea.Cmd, bool) {
	if current.Empty() || rendered.Empty() {
		return nil, false
	}
	if currentBaseOffset <= renderedBaseOffset {
		return nil, false
	}
	overlapBlocks := current.SharedSuffixPrefixBlockCount(rendered)
	if overlapBlocks <= 0 {
		return nil, false
	}
	m.nativeRenderedProjection = current
	m.nativeRenderedBaseOffset = currentBaseOffset
	m.nativeRenderedSnapshot = current.Render(tui.TranscriptDivider)
	if overlapBlocks >= len(current.Blocks) {
		return nil, true
	}
	styledDelta := renderStyledNativeProjectionLines(current.LinesFromBlock(overlapBlocks, tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth())
	if strings.TrimSpace(styledDelta) == "" {
		return nil, true
	}
	return m.emitNativeRenderedText(styledDelta), true
}

func (m *uiModel) emitNativePostRewriteVisibleAppend(current tui.TranscriptProjection, rendered tui.TranscriptProjection) (tea.Cmd, bool) {
	if current.Empty() || rendered.Empty() {
		return nil, false
	}
	renderedFrontier, ok := nativeProjectionRenderedFrontier(rendered)
	if !ok {
		return nil, false
	}
	if !nativeProjectionOverlapMatchesRendered(current, rendered, renderedFrontier) {
		return nil, false
	}
	startBlock := nativeProjectionFirstBlockAfterEntry(current, renderedFrontier)
	if startBlock < 0 {
		return nil, false
	}
	m.nativeRenderedProjection = current
	m.nativeRenderedBaseOffset = m.nativeProjectionBaseOffset
	m.nativeRenderedSnapshot = current.Render(tui.TranscriptDivider)
	styledDelta := renderStyledNativeProjectionLines(current.LinesFromBlock(startBlock, tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth())
	if strings.TrimSpace(styledDelta) == "" {
		return nil, true
	}
	return m.emitNativeRenderedText(styledDelta), true
}
