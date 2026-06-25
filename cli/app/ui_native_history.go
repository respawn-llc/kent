package app

import (
	"fmt"
	"strings"

	"core/cli/app/internal/nativescrollback"
	"core/cli/tui"

	tea "github.com/charmbracelet/bubbletea"
)

type nativeScrollbackInvariantViolation struct {
	Reason         string
	RenderedBlocks int
	CurrentBlocks  int
}

func (v nativeScrollbackInvariantViolation) Error() string {
	if strings.TrimSpace(v.Reason) == "" {
		return "native scrollback invariant violation"
	}
	return fmt.Sprintf("native scrollback invariant violation: %s", v.Reason)
}

func (m *uiModel) syncNativeHistoryFromTranscript() tea.Cmd {
	if m.nativeScrollbackInvariantSet {
		return nil
	}
	if !m.windowSizeKnown {
		return nil
	}
	nativeCommittedEntries := committedNativeScrollbackEntriesForApp(m.transcriptEntries)
	committedEntries := nativeCommittedEntries.Entries
	if len(committedEntries) == 0 {
		if m.nativeCommittedHistoryBaselineExists() {
			if m.nativeHistoryReplayPermit == nativeHistoryReplayPermitContinuityRecovery {
				m.rebaseNativeProjection(tui.TranscriptProjection{}, m.transcriptBaseOffset, 0)
				if !m.shouldEmitNativeHistory() {
					return sequenceCmds(nil, m.syncNativeStreamingScrollback())
				}
				return sequenceCmds(m.emitCurrentNativeScrollbackState(), m.syncNativeStreamingScrollback())
			}
			return sequenceCmds(
				m.reportNativeProjectionDivergence(tui.TranscriptProjection{}, m.nativeCommittedHistoryDivergenceBaseline()),
				m.syncNativeStreamingScrollback(),
			)
		}
		hasPendingTransientTail := len(nativescrollback.PendingOngoingEntries(m.transcriptEntries)) > 0
		alreadyReplayed := m.nativeHistoryReplayed()
		m.resetNativeHistoryState()
		m.nativeScrollbackLedger.MarkHistoryReplayed()
		if hasPendingTransientTail || alreadyReplayed || !m.shouldEmitNativeHistory() {
			return sequenceCmds(nil, m.syncNativeStreamingScrollback())
		}
		return sequenceCmds(m.emitCurrentNativeScrollbackState(), m.syncNativeStreamingScrollback())
	}

	committedCount := len(committedEntries)
	projection := m.nativeCommittedProjection(committedEntries)
	if m.nativeCommittedEntryCount() < 0 || m.nativeCommittedEntryCount() > committedCount {
		if m.nativeHistoryReplayPermit == nativeHistoryReplayPermitContinuityRecovery {
			previousProjection := m.nativeCommittedHistoryDivergenceBaseline()
			m.rebaseNativeProjection(projection, m.transcriptBaseOffset, committedCount)
			if !m.shouldEmitNativeHistory() {
				return sequenceCmds(nil, m.syncNativeStreamingScrollback())
			}
			m.consumeNativeHistoryReplayPermit()
			return sequenceCmds(m.emitNonContiguousNativeProjectionRecovery(projection, previousProjection), m.syncNativeStreamingScrollback())
		}
		return sequenceCmds(
			m.reportNativeProjectionDivergence(projection, m.nativeCommittedHistoryDivergenceBaseline()),
			m.syncNativeStreamingScrollback(),
		)
	}
	if !m.shouldEmitNativeHistory() && m.canFinalizeNativeStreamingCommit(nativeCommittedEntries, committedCount) {
		return nil
	}
	if cmd, handled := m.finalizeNativeStreamingCommit(projection, nativeCommittedEntries, committedCount); handled {
		return cmd
	}
	if !m.nativeHistoryReplayed() || m.nativeCurrentProjection().Empty() {
		m.rebaseNativeProjection(projection, m.transcriptBaseOffset, committedCount)
		if !m.shouldEmitNativeHistory() {
			return nil
		}
		return sequenceCmds(m.emitCurrentNativeScrollbackState(), m.syncNativeStreamingScrollback())
	}
	rendered := m.nativeScheduledRenderedProjectionState()
	previousProjection := rendered.Projection
	previousBaseOffset := rendered.BaseOffset
	if previousProjection.Empty() {
		current := m.nativeCurrentProjectionState()
		previousProjection = current.Projection
		previousBaseOffset = current.BaseOffset
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
			return sequenceCmds(appendCmd, m.syncNativeStreamingScrollback())
		}
		if appendCmd, appended := m.emitNativePostRewriteVisibleAppend(projection, previousProjection); appended {
			return sequenceCmds(appendCmd, m.syncNativeStreamingScrollback())
		}
		if replayPermit == nativeHistoryReplayPermitContinuityRecovery {
			return sequenceCmds(m.emitNonContiguousNativeProjectionRecovery(projection, previousProjection), m.syncNativeStreamingScrollback())
		}
		return sequenceCmds(m.reportNativeProjectionDivergence(projection, previousProjection), m.syncNativeStreamingScrollback())
	}
	if strings.TrimSpace(delta) == "" {
		return sequenceCmds(nil, m.syncNativeStreamingScrollback())
	}
	cmd := m.emitNativeRenderedTextWithOptions(renderStyledNativeProjectionLines(projection.LinesFromBlock(previousBlockCount, tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth()), false)
	m.scheduleNativeRenderedProjectionCommit(projection, false)
	return sequenceCmds(cmd, m.syncNativeStreamingScrollback())
}

func (m *uiModel) syncNativeHistoryFromTranscriptAndTrackCommittedDelivery() tea.Cmd {
	if m == nil {
		return nil
	}
	beforeSequence := m.nativeLastScheduledFlushSequence()
	cmd := m.syncNativeHistoryFromTranscript()
	if m.nativeScrollbackInvariantSet {
		return cmd
	}
	return sequenceCmds(cmd, m.trackOngoingCommittedFrontierFlush(committedOngoingLocalFrontierEnd(m), m.transcriptRevision, beforeSequence))
}

func (m *uiModel) nativeCommittedHistoryBaselineExists() bool {
	if m == nil {
		return false
	}
	return m.nativeCommittedEntryCount() > 0 ||
		!m.nativeCurrentProjection().Empty() ||
		!m.nativeScheduledRenderedProjectionState().Projection.Empty()
}

func (m *uiModel) nativeCommittedHistoryDivergenceBaseline() tui.TranscriptProjection {
	if m == nil {
		return tui.TranscriptProjection{}
	}
	if rendered := m.nativeScheduledRenderedProjectionState().Projection; !rendered.Empty() {
		return rendered
	}
	return m.nativeCurrentProjection()
}

func (m *uiModel) canFinalizeNativeStreamingCommit(committedEntries committedNativeScrollbackEntries, committedCount int) bool {
	return m.nativeStreamingCommitAssistantIndex(committedEntries, committedCount) >= 0
}

func (m *uiModel) nativeStreamingCommitAssistantIndex(committedEntries committedNativeScrollbackEntries, committedCount int) int {
	if m == nil {
		return -1
	}
	if strings.TrimSpace(m.view.OngoingStreamingText()) != "" {
		return -1
	}
	streamState := m.nativeScrollbackLedger.AssistantStreamState()
	if strings.TrimSpace(streamState.Source) == "" && !m.nativeStreamingAwaitingCommit {
		return -1
	}
	previousCommittedCount := m.nativeCommittedEntryCount()
	if previousCommittedCount < 0 || previousCommittedCount > committedCount {
		return -1
	}
	startIndex := previousCommittedCount
	endIndex := committedCount
	if streamState.CommitRangeSet {
		var ok bool
		startIndex, endIndex, ok = committedEntries.renderedRangeForSourceRange(
			m.transcriptBaseOffset,
			streamState.CommitStartEntryCount,
			streamState.CommitEndEntryCount,
		)
		if !ok {
			return -1
		}
		if startIndex < previousCommittedCount || endIndex > committedCount || endIndex <= startIndex {
			return -1
		}
	} else if strings.TrimSpace(streamState.StepID) != "" {
		return -1
	}
	assistantIndex := -1
	for idx := startIndex; idx < endIndex && idx < len(committedEntries.Entries); idx++ {
		entry := committedEntries.Entries[idx]
		if entry.Role != tui.TranscriptRoleAssistant {
			continue
		}
		if assistantIndex >= 0 {
			return -1
		}
		assistantIndex = idx
	}
	return assistantIndex
}

func (m *uiModel) shouldEmitNativeHistory() bool {
	return m.windowSizeKnown && m.view.Mode() == tui.ModeOngoing
}

func (m *uiModel) nativeReplayRenderWidth() int {
	if m.nativeReplayWidth > 0 {
		return m.nativeReplayWidth
	}
	if m.termWidth > 0 {
		return m.termWidth
	}
	return 120
}

func (m *uiModel) resetNativeHistoryState() {
	m.nativeCommittedProjector = tui.CommittedOngoingProjector{}
	m.nativeHistoryReplayPermit = nativeHistoryReplayPermitNone
	m.waitRuntimeEventAfterFlushSequence = 0
	m.nativeScrollbackInvariant = nativeScrollbackInvariantViolation{}
	m.nativeScrollbackInvariantSet = false
	m.resetNativeStreamingState()
	m.nativeScrollbackLedger.ResetHistoryState()
}

func (m *uiModel) flushSupersededAssistantStreamTurn() tea.Cmd {
	if m == nil {
		return nil
	}
	m.sawAssistantDelta = false
	m.nativeStreamingActive = false
	m.forwardToView(tui.ClearOngoingAssistantMsg{})
	m.resetNativeStreamingState()
	return m.drainDeferredCommittedDeliveryIfUnblocked()
}

func (m *uiModel) resetNativeStreamingState() {
	m.nativeStreamingAwaitingCommit = false
	m.nativeStreamingDividerFlushed = false
	m.nativeScrollbackLedger.ResetAssistantStream()
}

func (m *uiModel) syncNativeStreamingScrollback() tea.Cmd {
	if m == nil || !m.shouldEmitNativeHistory() {
		return nil
	}
	streamText, ok := m.activeNativeStreamingText()
	if !ok {
		if m.nativeStreamingAwaitingCommit {
			return nil
		}
		m.resetNativeStreamingState()
		return nil
	}
	width := m.nativeReplayRenderWidth()
	update := m.nativeScrollbackLedger.ApplyAssistantStreamSource(nativescrollback.AssistantStreamInput{
		Source: streamText,
		Theme:  m.theme,
		Width:  width,
	})
	if len(update.Stable) == 0 {
		return nil
	}
	lines := make([]tui.TranscriptProjectionLine, 0, len(update.Stable)+1)
	hasNativeCommittedHistory := len(committedNativeScrollbackEntriesForApp(m.transcriptEntries).Entries) > 0
	if hasNativeCommittedHistory && !m.nativeStreamingDividerFlushed {
		lines = append(lines, tui.TranscriptProjectionLine{Kind: tui.VisibleLineDivider, Text: tui.TranscriptDivider})
		m.nativeStreamingDividerFlushed = true
	}
	lines = append(lines, update.Stable...)
	cmd := m.emitNativeRenderedTextWithOptions(renderStyledNativeProjectionLines(lines, m.theme, width), false)
	if cmd != nil {
		m.nativeScrollbackLedger.BindAssistantStableFlush(nativescrollback.Sequence(m.nativeLastScheduledFlushSequence()), update.StableLineCount)
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

func (m *uiModel) finalizeNativeStreamingCommit(projection tui.TranscriptProjection, nativeCommittedEntries committedNativeScrollbackEntries, committedCount int) (tea.Cmd, bool) {
	committedEntries := nativeCommittedEntries.Entries
	streamedAssistantIndex := m.nativeStreamingCommitAssistantIndex(nativeCommittedEntries, committedCount)
	if streamedAssistantIndex < 0 {
		if m != nil && strings.TrimSpace(m.nativeScrollbackLedger.AssistantStreamState().Source) == "" {
			m.resetNativeStreamingState()
		}
		return nil, false
	}
	previousCommittedCount := m.nativeCommittedEntryCount()
	newEntries := committedEntries[previousCommittedCount:]
	if len(newEntries) == 0 {
		m.resetNativeStreamingState()
		return nil, false
	}
	if m.nativeScrollbackLedger.AssistantStreamState().NeedsReplay {
		m.consumeNativeHistoryReplayPermit()
		m.resetNativeStreamingState()
		return m.reportNativeProjectionDivergence(projection, m.nativeRenderedProjection()), true
	}
	hadCommittedHistory := previousCommittedCount > 0
	committedAssistantText := committedEntries[streamedAssistantIndex].Text
	finalUpdate := m.nativeScrollbackLedger.FinalizeAssistantStreamSource(nativescrollback.AssistantStreamInput{
		Source: committedAssistantText,
		Theme:  m.theme,
		Width:  m.nativeReplayRenderWidth(),
	})
	if finalUpdate.NeedsReplay {
		m.consumeNativeHistoryReplayPermit()
		m.resetNativeStreamingState()
		return m.reportNativeProjectionDivergence(projection, m.nativeRenderedProjection()), true
	}
	flushTailText := renderStyledNativeProjectionLines(m.nativeStreamingFinalizeLines(finalUpdate.Stable, hadCommittedHistory), m.theme, m.nativeReplayRenderWidth())
	postAssistantText := m.nativeProjectionTextForEntryRangeExcluding(projection, previousCommittedCount, committedCount, streamedAssistantIndex)
	var flushTail tea.Cmd
	var postAssistant tea.Cmd
	if strings.TrimSpace(flushTailText) != "" {
		flushTail = m.emitNativeRenderedTextWithOptions(flushTailText, true)
		postAssistant = m.emitNativeRenderedTextWithOptions(postAssistantText, false)
	} else {
		postAssistant = m.emitNativeRenderedTextWithOptions(postAssistantText, true)
	}
	m.consumeNativeHistoryReplayPermit()
	m.rebaseNativeProjection(projection, m.transcriptBaseOffset, committedCount)
	m.scheduleNativeRenderedProjectionCommit(projection, true)
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

func (m *uiModel) nativeProjectionTextForEntryRangeExcluding(projection tui.TranscriptProjection, startIndex int, endIndex int, excludedIndex int) string {
	if m == nil || startIndex >= endIndex {
		return ""
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
		return ""
	}
	return styled
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
}

func (m *uiModel) consumeNativeHistoryReplayPermit() nativeHistoryReplayPermit {
	if m == nil {
		return nativeHistoryReplayPermitNone
	}
	permit := m.nativeHistoryReplayPermit
	m.nativeHistoryReplayPermit = nativeHistoryReplayPermitNone
	return permit
}

func (m *uiModel) scheduleNativeRenderedProjectionCommit(projection tui.TranscriptProjection, resetStreaming bool) {
	m.scheduleNativeRenderedProjectionCommitAtBase(projection, m.nativeCurrentProjectionBaseOffset(), resetStreaming)
}

func (m *uiModel) scheduleNativeRenderedProjectionCommitAtBase(projection tui.TranscriptProjection, baseOffset int, resetStreaming bool) {
	if m == nil {
		return
	}
	m.nativeScrollbackLedger.ScheduleRenderedProjectionCommit(projection, baseOffset, resetStreaming)
	m.applyNativeRenderedProjectionCommitIfReady()
}

func (m *uiModel) applyNativeRenderedProjectionCommitIfReady() tea.Cmd {
	update, ok := m.nativeScrollbackLedger.ApplyRenderedProjectionCommitIfReady()
	if !ok {
		return nil
	}
	if update.ResetStreaming {
		m.resetNativeStreamingState()
		return sequenceCmds(m.releaseDeferredRuntimeSyncs(), m.drainDeferredCommittedDeliveryIfUnblocked())
	}
	return nil
}

func (m *uiModel) reportNativeProjectionDivergence(current tui.TranscriptProjection, rendered tui.TranscriptProjection) tea.Cmd {
	violation := nativeScrollbackInvariantViolation{
		Reason:         "same-session committed transcript divergence requires root-cause fix",
		RenderedBlocks: len(rendered.Blocks),
		CurrentBlocks:  len(current.Blocks),
	}
	if !m.nativeScrollbackInvariantSet {
		m.nativeScrollbackInvariant = violation
		m.nativeScrollbackInvariantSet = true
	}
	m.logf(
		"ui.native_history.invariant_violation reason=%q rendered_blocks=%d current_blocks=%d",
		violation.Reason,
		violation.RenderedBlocks,
		violation.CurrentBlocks,
	)
	return m.sendTransientStatusWithNoticeID(violation.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "native-scrollback-invariant")
}

func (m *uiModel) rebaseNativeProjection(projection tui.TranscriptProjection, baseOffset int, committedCount int) {
	m.nativeScrollbackLedger.SetCurrentProjection(projection, baseOffset, committedCount)
}

func (m *uiModel) emitCurrentNativeScrollbackState() tea.Cmd {
	replayPermit := m.consumeNativeHistoryReplayPermit()
	if !m.nativeCurrentProjection().Empty() {
		return m.emitCurrentNativeHistorySnapshot(replayPermit)
	}
	if replayPermit == nativeHistoryReplayPermitContinuityRecovery {
		return m.resetAndEmitContinuityRecoveryEmptyNativeScrollback()
	}
	return m.emitEmptyNativeScrollbackSpacer()
}

func (m *uiModel) emitCurrentNativeScrollbackStateAndTrackCommittedDelivery() tea.Cmd {
	if m == nil {
		return nil
	}
	beforeSequence := m.nativeLastScheduledFlushSequence()
	cmd := m.emitCurrentNativeScrollbackState()
	if m.nativeScrollbackInvariantSet {
		return cmd
	}
	return sequenceCmds(cmd, m.trackOngoingCommittedFrontierFlush(committedOngoingLocalFrontierEnd(m), m.transcriptRevision, beforeSequence))
}

func (m *uiModel) emitEmptyNativeScrollbackSpacer() tea.Cmd {
	spacer := m.nativeEmptyScrollbackSpacerText()
	if spacer == "" {
		return nil
	}
	return m.emitNativeHistoryFlushWithOptions(spacer, true, false)
}

func (m *uiModel) emitContinuityRecoveryEmptyNativeScrollback() tea.Cmd {
	spacer := m.nativeEmptyScrollbackSpacerText()
	if spacer == "" {
		return tea.ClearScreen
	}
	return tea.Sequence(tea.ClearScreen, m.emitNativeHistoryFlushWithOptions(spacer, true, false))
}

func (m *uiModel) resetAndEmitContinuityRecoveryEmptyNativeScrollback() tea.Cmd {
	m.resetNativeHistoryState()
	m.nativeScrollbackLedger.MarkHistoryReplayed()
	return m.emitContinuityRecoveryEmptyNativeScrollback()
}

func (m *uiModel) nativeEmptyScrollbackSpacerText() string {
	if !m.windowSizeKnown || m.termHeight <= 0 {
		return ""
	}
	return strings.Repeat("\n", m.termHeight)
}

func (m *uiModel) emitCurrentNativeHistorySnapshot(replayPermit nativeHistoryReplayPermit) tea.Cmd {
	current := m.nativeCurrentProjectionState()
	rawSnapshot := current.Projection.Render(tui.TranscriptDivider)
	if strings.TrimSpace(rawSnapshot) == "" {
		return nil
	}
	rendered := m.nativeScheduledRenderedProjectionState()
	rewriteRenderedHistory := m.view.Mode() == tui.ModeOngoing && !rendered.Projection.Empty()
	if !rendered.Projection.Empty() {
		previousBlockCount := len(rendered.Projection.Blocks)
		delta, ok := current.Projection.RenderAppendDeltaFrom(rendered.Projection, tui.TranscriptDivider)
		delta = strings.TrimPrefix(delta, "\n")
		if ok && strings.TrimSpace(delta) != "" {
			styledDelta := renderStyledNativeProjectionLines(current.Projection.LinesFromBlock(previousBlockCount, tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth())
			if strings.TrimSpace(styledDelta) != "" {
				return m.emitNativeRenderedTextAndCommitProjection(styledDelta, false, current.Projection, current.BaseOffset, false)
			}
		}
		if ok && strings.TrimSpace(delta) == "" {
			return nil
		}
		if appendCmd, appended := m.emitNativeSlidingWindowAppend(current.Projection, rendered.Projection, current.BaseOffset, rendered.BaseOffset); appended {
			return appendCmd
		}
		if appendCmd, appended := m.emitNativePostRewriteVisibleAppend(current.Projection, rendered.Projection); appended {
			return appendCmd
		}
		if rewriteRenderedHistory {
			if replayPermit == nativeHistoryReplayPermitContinuityRecovery {
				return m.emitNonContiguousNativeProjectionRecovery(current.Projection, rendered.Projection)
			}
			return m.reportNativeProjectionDivergence(current.Projection, rendered.Projection)
		}
		return nil
	}
	if deltaRaw, ok := nativeRenderedDelta(rendered.Snapshot, rawSnapshot); ok {
		styledDelta := styleNativeReplayDividers(deltaRaw, m.theme, m.nativeReplayRenderWidth())
		if strings.TrimSpace(styledDelta) == "" {
			return nil
		}
		return m.emitNativeRenderedTextAndCommitProjection(styledDelta, false, current.Projection, current.BaseOffset, false)
	}
	if rewriteRenderedHistory {
		return nil
	}
	styled := renderStyledNativeProjectionLines(current.Projection.Lines(tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth())
	if strings.TrimSpace(styled) == "" {
		return nil
	}
	return m.emitNativeRenderedTextAndCommitProjection(styled, false, current.Projection, current.BaseOffset, false)
}

func (m *uiModel) emitNativeRenderedTextAndCommitProjection(rendered string, clearBelowBefore bool, projection tui.TranscriptProjection, baseOffset int, resetStreaming bool) tea.Cmd {
	cmd := m.emitNativeRenderedTextWithOptions(rendered, clearBelowBefore)
	m.scheduleNativeRenderedProjectionCommitAtBase(projection, baseOffset, resetStreaming)
	return cmd
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
	if overlapBlocks >= len(current.Blocks) {
		return nil, true
	}
	styledDelta := renderStyledNativeProjectionLines(current.LinesFromBlock(overlapBlocks, tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth())
	if strings.TrimSpace(styledDelta) == "" {
		return nil, true
	}
	return m.emitNativeRenderedTextAndCommitProjection(styledDelta, false, current, currentBaseOffset, false), true
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
	styledDelta := renderStyledNativeProjectionLines(current.LinesFromBlock(startBlock, tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth())
	if strings.TrimSpace(styledDelta) == "" {
		return nil, true
	}
	return m.emitNativeRenderedTextAndCommitProjection(styledDelta, false, current, m.nativeCurrentProjectionBaseOffset(), false), true
}
