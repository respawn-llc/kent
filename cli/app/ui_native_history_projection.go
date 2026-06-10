package app

import (
	"strings"

	"builder/cli/tui"

	tea "github.com/charmbracelet/bubbletea"
)

func nativeProjectionRenderedFrontier(projection tui.TranscriptProjection) (int, bool) {
	if len(projection.Blocks) == 0 {
		return 0, false
	}
	frontier := projection.Blocks[len(projection.Blocks)-1].EntryEnd
	if frontier < 0 {
		frontier = projection.Blocks[len(projection.Blocks)-1].EntryIndex
	}
	return frontier, frontier >= 0
}

func nativeProjectionFirstBlockAfterEntry(projection tui.TranscriptProjection, frontier int) int {
	for idx, block := range projection.Blocks {
		if block.EntryIndex > frontier {
			return idx
		}
	}
	return -1
}

func nativeProjectionOverlapMatchesRendered(current tui.TranscriptProjection, rendered tui.TranscriptProjection, frontier int) bool {
	if frontier < 0 {
		return false
	}
	renderedByRange := make(map[[2]int]tui.TranscriptProjectionBlock, len(rendered.Blocks))
	for _, block := range rendered.Blocks {
		renderedByRange[[2]int{block.EntryIndex, block.EntryEnd}] = block
	}
	for _, block := range current.Blocks {
		if block.EntryEnd > frontier {
			continue
		}
		renderedBlock, ok := renderedByRange[[2]int{block.EntryIndex, block.EntryEnd}]
		if !ok || !nativeProjectionBlocksEqual(block, renderedBlock) {
			return false
		}
	}
	return true
}

func nativeProjectionBlocksEqual(left tui.TranscriptProjectionBlock, right tui.TranscriptProjectionBlock) bool {
	if left.Role != right.Role || left.DividerGroup != right.DividerGroup || len(left.Lines) != len(right.Lines) {
		return false
	}
	for idx := range left.Lines {
		if left.Lines[idx] != right.Lines[idx] {
			return false
		}
	}
	return true
}

func (m *uiModel) emitNonContiguousNativeProjectionRecovery(current tui.TranscriptProjection, rendered tui.TranscriptProjection) tea.Cmd {
	if current.Empty() {
		return nil
	}
	m.logf("ui.native_history.rebuild_required rendered_blocks=%d current_blocks=%d", len(rendered.Blocks), len(current.Blocks))
	return m.emitForcedNativeProjectionReplay(current)
}

func (m *uiModel) emitForcedNativeProjectionReplay(projection tui.TranscriptProjection) tea.Cmd {
	rawSnapshot := projection.Render(tui.TranscriptDivider)
	m.nativeRenderedProjection = projection
	m.nativeRenderedBaseOffset = m.nativeProjectionBaseOffset
	m.nativeRenderedSnapshot = rawSnapshot
	if strings.TrimSpace(rawSnapshot) == "" {
		return tea.ClearScreen
	}
	styled := renderStyledNativeProjectionLines(projection.Lines(tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth())
	if strings.TrimSpace(styled) == "" {
		return tea.ClearScreen
	}
	return tea.Sequence(tea.ClearScreen, m.emitNativeRenderedTextWithOptions(styled, false))
}

func nativeRenderedDelta(previous, current string) (string, bool) {
	if strings.TrimSpace(previous) == "" || previous == current {
		return "", false
	}
	if !strings.HasPrefix(current, previous) {
		return "", false
	}
	delta := strings.TrimPrefix(current, previous)
	delta = strings.TrimPrefix(delta, "\n")
	return delta, true
}

func (m *uiModel) replayNativeTranscriptThroughEntry(entryIndex int) tea.Cmd {
	if !m.windowSizeKnown {
		return nil
	}
	localIndex := entryIndex - m.transcriptBaseOffset
	if localIndex < 0 || localIndex >= len(m.transcriptEntries) {
		return nil
	}
	entries := m.transcriptEntries[:localIndex+1]
	projection := m.nativeCommittedProjection(committedTranscriptEntriesForApp(entries))
	rawSnapshot := projection.Render(tui.TranscriptDivider)
	m.nativeRenderedProjection = projection
	m.nativeRenderedBaseOffset = m.transcriptBaseOffset
	m.nativeRenderedSnapshot = rawSnapshot
	if strings.TrimSpace(rawSnapshot) == "" {
		return tea.ClearScreen
	}
	return tea.Sequence(
		tea.ClearScreen,
		m.emitNativeRenderedTextWithOptions(renderStyledNativeProjectionLines(projection.Lines(tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth()), false),
	)
}

func (m *uiModel) emitNativeRenderedTextWithOptions(rendered string, clearBelowBefore bool) tea.Cmd {
	if len(rendered) <= 64*1024 {
		return m.emitNativeHistoryFlushWithOptions(rendered, false, clearBelowBefore)
	}
	chunks := splitNativeScrollbackChunks(rendered, 64*1024)
	if len(chunks) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(chunks))
	for idx, chunk := range chunks {
		if cmd := m.emitNativeHistoryFlushWithOptions(chunk, false, clearBelowBefore && idx == 0); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	if len(cmds) == 0 {
		return nil
	}
	if len(cmds) == 1 {
		return cmds[0]
	}
	return tea.Sequence(cmds...)
}

func (m *uiModel) emitNativeHistoryFlushWithOptions(text string, allowBlank bool, clearBelowBefore bool) tea.Cmd {
	if text == "" {
		return nil
	}
	if !allowBlank && strings.TrimSpace(text) == "" {
		return nil
	}
	m.nativeFlushSequence++
	msg := nativeHistoryFlushMsg{Text: text, AllowBlank: allowBlank, ClearBelowBefore: clearBelowBefore, Sequence: m.nativeFlushSequence}
	return func() tea.Msg {
		return msg
	}
}

func (m *uiModel) discardPendingNativeHistoryFlushes() {
	m.nativeFlushedSequence = m.nativeFlushSequence
	if len(m.nativePendingFlushes) == 0 {
		return
	}
	clear(m.nativePendingFlushes)
}

func (m *uiModel) handleNativeHistoryFlush(msg nativeHistoryFlushMsg) tea.Cmd {
	defer func() {
		if m.ongoingCommittedDelivery.initialized {
			m.ongoingCommittedDelivery.ackNativeFlush(m.nativeFlushedSequence)
		}
	}()
	if msg.Sequence == 0 {
		if !msg.AllowBlank && strings.TrimSpace(msg.Text) == "" {
			if m.waitRuntimeEventAfterFlushSequence != 0 && m.nativeFlushedSequence >= m.waitRuntimeEventAfterFlushSequence {
				m.waitRuntimeEventAfterFlushSequence = 0
				return m.waitRuntimeEventCmd()
			}
			return nil
		}
		cmds := []tea.Cmd{tea.Printf("%s", msg.Text)}
		if m.waitRuntimeEventAfterFlushSequence != 0 && m.nativeFlushedSequence >= m.waitRuntimeEventAfterFlushSequence {
			m.waitRuntimeEventAfterFlushSequence = 0
			cmds = append(cmds, m.waitRuntimeEventCmd())
		}
		return sequenceCmds(cmds...)
	}
	if msg.Sequence <= m.nativeFlushedSequence {
		return nil
	}
	if msg.Sequence > m.nativeFlushedSequence+1 {
		if m.nativePendingFlushes == nil {
			m.nativePendingFlushes = make(map[uint64]nativeHistoryFlushMsg)
		}
		m.nativePendingFlushes[msg.Sequence] = msg
		return nil
	}
	cmds := make([]tea.Cmd, 0, 1)
	current := msg
	for {
		m.nativeFlushedSequence = current.Sequence
		if current.AllowBlank || strings.TrimSpace(current.Text) != "" {
			text := current.Text
			if current.ClearBelowBefore {
				text = "\x1b[J" + text
			}
			cmds = append(cmds, tea.Printf("%s", text))
		}
		next, ok := m.nativePendingFlushes[m.nativeFlushedSequence+1]
		if !ok {
			break
		}
		delete(m.nativePendingFlushes, next.Sequence)
		current = next
	}
	ackSequence := m.nativeStreamingStableFlushAckSequence()
	if ackSequence != 0 {
		cmds = append(cmds, func() tea.Msg {
			return nativeStreamingStableFlushAckMsg{Sequence: ackSequence}
		})
	}
	if m.waitRuntimeEventAfterFlushSequence != 0 && m.nativeFlushedSequence >= m.waitRuntimeEventAfterFlushSequence {
		m.waitRuntimeEventAfterFlushSequence = 0
		cmds = append(cmds, m.waitRuntimeEventCmd())
	}
	return sequenceCmds(cmds...)
}

func (m *uiModel) nativeStreamingStableFlushAckSequence() uint64 {
	if m.nativeStreamingStableFlushSequence == 0 || m.nativeFlushedSequence < m.nativeStreamingStableFlushSequence {
		return 0
	}
	return m.nativeFlushedSequence
}

func (m *uiModel) ackNativeStreamingStableFlush(sequence uint64) {
	if sequence == 0 || m.nativeStreamingStableFlushSequence == 0 || sequence < m.nativeStreamingStableFlushSequence {
		return
	}
	m.nativeStreamingStableFlushSequence = 0
	if m.nativeStreamingController.invalidatedByResize {
		m.nativeStreamingTail = cloneNativeStreamProjectionLines(m.nativeStreamingController.rendered)
	} else {
		m.nativeStreamingTail = cloneNativeStreamProjectionLines(m.nativeStreamingController.rendered[m.nativeStreamingController.enqueuedStableLineCount:])
	}
}

func splitNativeScrollbackChunks(rendered string, maxBytes int) []string {
	if strings.TrimSpace(rendered) == "" {
		return nil
	}
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	lines := strings.Split(rendered, "\n")
	capacity := len(lines) / 32
	if capacity < 1 {
		capacity = 1
	}
	chunks := make([]string, 0, capacity)
	var current strings.Builder
	for _, line := range lines {
		if current.Len() == 0 {
			current.WriteString(line)
			continue
		}
		if current.Len()+1+len(line) > maxBytes {
			chunks = append(chunks, current.String())
			current.Reset()
			current.WriteString(line)
			continue
		}
		current.WriteByte('\n')
		current.WriteString(line)
	}
	if current.Len() > 0 {
		chunks = append(chunks, current.String())
	}
	return chunks
}

func (m *uiModel) nativeCommittedProjection(entries []tui.TranscriptEntry) tui.TranscriptProjection {
	if m == nil {
		return tui.ProjectCommittedOngoingTranscript(entries, "", 0)
	}
	return m.nativeCommittedProjector.Project(entries, tui.CommittedOngoingProjectionKey{
		Revision:   m.transcriptRevision,
		Width:      m.nativeReplayRenderWidth(),
		Theme:      m.theme,
		BaseOffset: m.transcriptBaseOffset,
		EntryCount: len(entries),
	})
}

func styleNativeReplayDividers(rendered, theme string, width int) string {
	if strings.TrimSpace(rendered) == "" {
		return rendered
	}
	rawLines := splitPlainLines(rendered)
	lines := make([]tui.TranscriptProjectionLine, 0, len(rawLines))
	for _, line := range rawLines {
		lines = append(lines, tui.TranscriptProjectionLine{Kind: tui.VisibleLineContent, Text: line})
	}
	return renderStyledNativeProjectionLines(lines, theme, width)
}

func renderStyledNativeProjectionLines(lines []tui.TranscriptProjectionLine, theme string, width int) string {
	if len(lines) == 0 {
		return ""
	}
	if width <= 0 {
		width = 120
	}
	style := uiThemeStyles(theme)
	divider := style.meta.Render(strings.Repeat("─", width))
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line.Kind == tui.VisibleLineDivider {
			out = append(out, divider)
			continue
		}
		out = append(out, line.Text)
	}
	return strings.Join(out, "\n")
}
