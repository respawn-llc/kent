package app

import (
	"strings"

	"core/cli/app/internal/nativescrollback"
	"core/cli/tui"

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
	rawSnapshot := current.Render(tui.TranscriptDivider)
	if strings.TrimSpace(rawSnapshot) == "" {
		return tea.ClearScreen
	}
	styled := renderStyledNativeProjectionLines(current.Lines(tui.TranscriptDivider), m.theme, m.nativeReplayRenderWidth())
	if strings.TrimSpace(styled) == "" {
		return tea.ClearScreen
	}
	return tea.Sequence(
		tea.ClearScreen,
		m.emitNativeRenderedTextAndCommitProjection(styled, false, current, m.nativeCurrentProjectionBaseOffset(), false),
	)
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

func (m *uiModel) emitNativeRenderedTextWithOptions(rendered string, clearBelowBefore bool) tea.Cmd {
	maxChunkBytes := min(64*1024, nativescrollback.TerminalWriteMaxPayload)
	if len(rendered) <= maxChunkBytes {
		return m.emitNativeHistoryFlushWithOptions(rendered, false, clearBelowBefore)
	}
	chunks := splitNativeScrollbackChunks(rendered, maxChunkBytes)
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
	flush, ok := m.nativeScrollbackLedger.Enqueue(text, nativescrollback.FlushOptions{
		AllowBlank:       allowBlank,
		ClearBelowBefore: clearBelowBefore,
	})
	if !ok {
		return nil
	}
	msg := nativeHistoryFlushMsg{
		Text:             flush.Text,
		AllowBlank:       flush.AllowBlank,
		ClearBelowBefore: flush.ClearBelowBefore,
		Sequence:         uint64(flush.Sequence),
		Flush:            flush,
	}
	return func() tea.Msg {
		return msg
	}
}

func (m *uiModel) nativeLastScheduledFlushSequence() uint64 {
	if m == nil {
		return 0
	}
	return uint64(m.nativeScrollbackLedger.LastScheduledSequence())
}

func (m *uiModel) nativeAckedFlushSequence() uint64 {
	if m == nil {
		return 0
	}
	return uint64(m.nativeScrollbackLedger.AckedSequence())
}

func (m *uiModel) nativeCurrentProjectionState() nativescrollback.ProjectionState {
	if m == nil {
		return nativescrollback.ProjectionState{}
	}
	return m.nativeScrollbackLedger.CurrentProjection()
}

func (m *uiModel) nativeCurrentProjection() tui.TranscriptProjection {
	return m.nativeCurrentProjectionState().Projection
}

func (m *uiModel) nativeCurrentProjectionBaseOffset() int {
	return m.nativeCurrentProjectionState().BaseOffset
}

func (m *uiModel) nativeCommittedEntryCount() int {
	if m == nil {
		return 0
	}
	return m.nativeScrollbackLedger.CurrentCommittedEntryCount()
}

func (m *uiModel) nativeHistoryReplayed() bool {
	return m != nil && m.nativeScrollbackLedger.HistoryReplayed()
}

func (m *uiModel) nativeRenderedProjectionState() nativescrollback.RenderedProjectionState {
	if m == nil {
		return nativescrollback.RenderedProjectionState{}
	}
	return m.nativeScrollbackLedger.RenderedProjection()
}

func (m *uiModel) nativeScheduledRenderedProjectionState() nativescrollback.RenderedProjectionState {
	if m == nil {
		return nativescrollback.RenderedProjectionState{}
	}
	return m.nativeScrollbackLedger.ScheduledRenderedProjection()
}

func (m *uiModel) nativeRenderedProjection() tui.TranscriptProjection {
	return m.nativeRenderedProjectionState().Projection
}

func (m *uiModel) nativeRenderedProjectionBaseOffset() int {
	return m.nativeRenderedProjectionState().BaseOffset
}

func (m *uiModel) nativeRenderedSnapshot() string {
	return m.nativeRenderedProjectionState().Snapshot
}

func (m *uiModel) nativeRenderedProjectionCommitPending() bool {
	return m != nil && m.nativeScrollbackLedger.RenderedProjectionCommitPending()
}

func (m *uiModel) handleNativeHistoryFlush(msg nativeHistoryFlushMsg) tea.Cmd {
	flush := msg.scheduledFlush()
	if flush.Sequence == 0 {
		if !msg.AllowBlank && strings.TrimSpace(msg.Text) == "" {
			if m.waitRuntimeEventAfterFlushSequence != 0 && m.nativeAckedFlushSequence() >= m.waitRuntimeEventAfterFlushSequence {
				m.waitRuntimeEventAfterFlushSequence = 0
				return m.waitRuntimeEventCmd()
			}
			return nil
		}
		cmds := []tea.Cmd{tea.Printf("%s", msg.Text)}
		if m.waitRuntimeEventAfterFlushSequence != 0 && m.nativeAckedFlushSequence() >= m.waitRuntimeEventAfterFlushSequence {
			m.waitRuntimeEventAfterFlushSequence = 0
			cmds = append(cmds, m.waitRuntimeEventCmd())
		}
		return sequenceCmds(cmds...)
	}
	write, ready, err := m.nativeScrollbackLedger.AcceptFlush(flush)
	if err != nil {
		return m.reportNativeTerminalWriteFailure(err)
	}
	if !ready {
		return nil
	}
	return m.nativeTerminalWriteCmd(write)
}

func (msg nativeHistoryFlushMsg) scheduledFlush() nativescrollback.ScheduledFlush {
	if msg.Flush.Sequence != 0 {
		return msg.Flush
	}
	return nativescrollback.ScheduledFlush{
		Sequence:         nativescrollback.Sequence(msg.Sequence),
		Text:             msg.Text,
		AllowBlank:       msg.AllowBlank,
		ClearBelowBefore: msg.ClearBelowBefore,
	}
}

func (m *uiModel) handleNativeTerminalWriteResult(result nativescrollback.TerminalWriteResult) tea.Cmd {
	update, err := m.nativeScrollbackLedger.Ack(result)
	if err != nil {
		return m.reportNativeTerminalWriteFailure(err)
	}
	cmds := make([]tea.Cmd, 0, 1)
	if commitCmd := m.applyNativeRenderedProjectionCommitIfReady(); commitCmd != nil {
		cmds = append(cmds, commitCmd)
	}
	m.nativeScrollbackLedger.AckCommittedNativeFlush(nativescrollback.Sequence(m.nativeAckedFlushSequence()))
	if update.HasNext {
		cmds = append(cmds, m.nativeTerminalWriteCmd(update.Next))
	}
	if m.waitRuntimeEventAfterFlushSequence != 0 && m.nativeAckedFlushSequence() >= m.waitRuntimeEventAfterFlushSequence {
		m.waitRuntimeEventAfterFlushSequence = 0
		cmds = append(cmds, m.waitRuntimeEventCmd())
	}
	return sequenceCmds(cmds...)
}

func (m *uiModel) nativeTerminalWriteCmd(write nativescrollback.TerminalWrite) tea.Cmd {
	if write.Text == "" {
		return nil
	}
	if !write.AllowBlank && strings.TrimSpace(write.Text) == "" {
		return nil
	}
	ackResults := m.nativeTerminalWriteResults()
	if ackResults == nil {
		return tea.Sequence(
			tea.Printf("%s", write.Text),
			func() tea.Msg {
				return nativeTerminalWriteResultMsg{Result: nativescrollback.TerminalWriteResult{Sequence: write.Sequence}}
			},
		)
	}
	encoded, err := m.terminalCursor.encodeNativeScrollbackWrite(write)
	if err != nil {
		return func() tea.Msg {
			return nativeTerminalWriteResultMsg{Result: nativescrollback.TerminalWriteResult{Sequence: write.Sequence, Err: err.Error()}}
		}
	}
	return tea.Printf("%s", encoded)
}

func (m *uiModel) nativeTerminalWriteResults() <-chan nativescrollback.TerminalWriteResult {
	if m == nil || m.terminalCursor == nil {
		return nil
	}
	return m.terminalCursor.nativeScrollbackWriteResults()
}

func waitNativeTerminalWriteResult(results <-chan nativescrollback.TerminalWriteResult) tea.Cmd {
	if results == nil {
		return nil
	}
	return func() tea.Msg {
		result, ok := <-results
		if !ok {
			return nil
		}
		return nativeTerminalWriteResultMsg{Result: result}
	}
}

func (m *uiModel) reportNativeTerminalWriteFailure(err error) tea.Cmd {
	if err == nil {
		return nil
	}
	violation := nativeScrollbackInvariantViolation{
		Reason:         err.Error(),
		RenderedBlocks: len(m.nativeRenderedProjection().Blocks),
		CurrentBlocks:  len(m.nativeCurrentProjection().Blocks),
	}
	if !m.nativeScrollbackInvariantSet {
		m.nativeScrollbackInvariant = violation
		m.nativeScrollbackInvariantSet = true
	}
	m.logf("ui.native_history.terminal_write_failure err=%q", err.Error())
	return m.sendTransientStatusWithNoticeID(violation.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "native-scrollback-invariant")
}

func splitNativeScrollbackChunks(rendered string, maxBytes int) []string {
	if strings.TrimSpace(rendered) == "" {
		return nil
	}
	if maxBytes <= 0 {
		maxBytes = 64 * 1024
	}
	maxBytes = min(maxBytes, nativescrollback.TerminalWriteMaxPayload)
	lines := strings.Split(rendered, "\n")
	capacity := len(lines) / 32
	if capacity < 1 {
		capacity = 1
	}
	chunks := make([]string, 0, capacity)
	var current strings.Builder
	for _, line := range lines {
		if len(line) > maxBytes {
			if current.Len() > 0 {
				chunks = append(chunks, current.String())
				current.Reset()
			}
			chunks = append(chunks, splitNativeScrollbackLongLine(line, maxBytes)...)
			continue
		}
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

func splitNativeScrollbackLongLine(line string, maxBytes int) []string {
	if maxBytes <= 0 || len(line) <= maxBytes {
		if line == "" {
			return nil
		}
		return []string{line}
	}
	chunks := make([]string, 0, len(line)/maxBytes+1)
	start := 0
	for start < len(line) {
		end := min(start+maxBytes, len(line))
		chunks = append(chunks, line[start:end])
		start = end
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
