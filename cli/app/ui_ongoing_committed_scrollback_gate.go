package app

import (
	"strings"

	"core/cli/tui"
	"core/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *uiModel) ongoingCommittedScrollbackGateActive() bool {
	if m == nil {
		return false
	}
	return strings.TrimSpace(m.view.OngoingStreamingText()) != "" ||
		m.sawAssistantDelta ||
		m.nativeStreamingActive ||
		m.nativeStreamingAwaitingCommit ||
		strings.TrimSpace(m.nativeScrollbackLedger.AssistantStreamState().Source) != ""
}

func (m *uiModel) shouldGateCommittedSuffixResponse(suffix clientui.CommittedTranscriptSuffix) bool {
	if m == nil || !m.ongoingCommittedScrollbackGateActive() {
		return false
	}
	trimmed := m.trimCommittedTranscriptSuffixForGate(suffix)
	if trimmed.NextEntryCount <= trimmed.StartEntryCount {
		return suffix.HasMore
	}
	if !m.committedSuffixCanFinalizeAssistantStream(trimmed) {
		return true
	}
	return m.committedSuffixAppendWouldDivergeNativeProjection(trimmed)
}

func (m *uiModel) committedSuffixAppendWouldDivergeNativeProjection(suffix clientui.CommittedTranscriptSuffix) bool {
	if m == nil || len(suffix.Entries) == 0 {
		return false
	}
	previous := m.nativeScheduledRenderedProjectionState().Projection
	if previous.Empty() {
		previous = m.nativeCurrentProjection()
	}
	if previous.Empty() {
		return false
	}
	projected := committedTranscriptEntriesForApp(m.transcriptEntries)
	for _, entry := range suffix.Entries {
		projected = append(projected, transcriptEntryFromProjectedChatEntry(entry, false, true))
	}
	projected = committedTranscriptEntriesForApp(projected)
	var projector tui.CommittedOngoingProjector
	projection := projector.Project(projected, tui.CommittedOngoingProjectionKey{
		Revision:   m.transcriptRevision,
		Width:      m.nativeReplayRenderWidth(),
		Theme:      m.theme,
		BaseOffset: m.transcriptBaseOffset,
		EntryCount: len(projected),
	})
	_, cleanAppend := projection.RenderAppendDeltaFrom(previous, tui.TranscriptDivider)
	return !cleanAppend
}

func (m *uiModel) trimCommittedTranscriptSuffixForGate(suffix clientui.CommittedTranscriptSuffix) clientui.CommittedTranscriptSuffix {
	if len(suffix.Entries) > 0 && suffix.NextEntryCount == suffix.StartEntryCount {
		suffix.NextEntryCount = suffix.StartEntryCount + len(suffix.Entries)
	}
	return m.trimCommittedTranscriptSuffixToDeliveryCursor(suffix)
}

func (m *uiModel) committedSuffixCanFinalizeAssistantStream(suffix clientui.CommittedTranscriptSuffix) bool {
	if m == nil {
		return false
	}
	// Committed suffix responses do not carry assistant step identity. When the
	// active stream has a step ID, only the direct finalizer event can prove it
	// belongs to that stream; otherwise a stale in-flight suffix with matching
	// text could clear a newer assistant.
	if strings.TrimSpace(m.nativeScrollbackLedger.AssistantStreamState().StepID) != "" {
		return false
	}
	activeStreams := m.activeAssistantStreamTextsForFinalizer()
	if len(activeStreams) == 0 {
		return false
	}
	for _, entry := range suffix.Entries {
		if !isProjectedAssistantEntry(entry) {
			continue
		}
		if m.nativeScrollbackLedger.AssistantSuffixCanFinalizeText(entry.Text) || stringSetContains(activeStreams, strings.TrimSpace(entry.Text)) {
			return true
		}
	}
	return false
}

func (m *uiModel) activeAssistantStreamTextsForFinalizer() map[string]struct{} {
	if m == nil {
		return nil
	}
	values := []string{
		m.view.OngoingStreamingText(),
		m.nativeScrollbackLedger.AssistantStreamState().Source,
	}
	streams := make(map[string]struct{}, len(values))
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			continue
		}
		streams[trimmed] = struct{}{}
	}
	if len(streams) == 0 {
		return nil
	}
	return streams
}

func stringSetContains(values map[string]struct{}, value string) bool {
	if value == "" {
		return false
	}
	_, ok := values[value]
	return ok
}

func isProjectedAssistantEntry(entry clientui.ChatEntry) bool {
	return tui.TranscriptRoleFromWire(entry.Role) == tui.TranscriptRoleAssistant
}

func (m *uiModel) deferRuntimeCommittedSuffixRefresh() {
	if m == nil {
		return
	}
	m.deferredCommittedSuffixRefreshSet = true
}

func (m *uiModel) drainDeferredCommittedDeliveryIfUnblocked() tea.Cmd {
	if m == nil || m.ongoingCommittedScrollbackGateActive() {
		return nil
	}
	tailCmd := m.drainDeferredCommittedTailIfUnblocked()
	suffixCmd := m.drainDeferredCommittedSuffixRefreshIfUnblocked()
	return sequenceCmds(tailCmd, suffixCmd)
}

func (m *uiModel) drainDeferredCommittedTailIfUnblocked() tea.Cmd {
	if m == nil || len(m.deferredCommittedTail) == 0 || m.ongoingCommittedScrollbackGateActive() {
		return nil
	}
	chainEnd := committedOngoingLocalFrontierEnd(m)
	entriesStart := -1
	revision := m.transcriptRevision
	totalEntries := m.transcriptTotalEntries
	entries := make([]clientui.ChatEntry, 0, len(m.deferredCommittedTail))
	used := 0
	for _, tail := range m.deferredCommittedTail {
		chainEnd = m.advanceDeferredDrainChainThroughLoadedPendingEntries(chainEnd, tail.rangeStart)
		if tail.rangeEnd <= chainEnd {
			used++
			continue
		}
		if tail.rangeStart > chainEnd {
			break
		}
		tailEntries := cloneChatEntries(tail.entries)
		if tail.rangeStart < chainEnd {
			skip := chainEnd - tail.rangeStart
			if skip >= len(tailEntries) {
				used++
				continue
			}
			tailEntries = tailEntries[skip:]
		}
		if entriesStart < 0 {
			entriesStart = max(tail.rangeStart, chainEnd)
		}
		entries = append(entries, tailEntries...)
		chainEnd = tail.rangeEnd
		if tail.revision > revision {
			revision = tail.revision
		}
		if tail.rangeEnd > totalEntries {
			totalEntries = tail.rangeEnd
		}
		used++
	}
	if used > 0 {
		m.deferredCommittedTail = append([]deferredProjectedTranscriptTail(nil), m.deferredCommittedTail[used:]...)
	}
	if len(entries) == 0 {
		if len(m.deferredCommittedTail) > 0 {
			return m.requestRuntimeCommittedGapSync()
		}
		return nil
	}
	if entriesStart < 0 {
		entriesStart = chainEnd - len(entries)
	}
	evt := clientui.Event{
		Kind:                       clientui.EventConversationUpdated,
		CommittedTranscriptChanged: true,
		CommittedEntryStart:        entriesStart,
		CommittedEntryStartSet:     true,
		CommittedEntryCount:        chainEnd,
		TranscriptRevision:         revision,
		TranscriptEntries:          entries,
	}
	beforeSequence := m.nativeLastScheduledFlushSequence()
	cmd, mutated := m.applyDeferredCommittedTailDelivery(evt, totalEntries)
	if mutated && !m.nativeScrollbackInvariantSet {
		return sequenceCmds(cmd, m.trackOngoingCommittedFrontierFlush(evt.CommittedEntryCount, evt.TranscriptRevision, beforeSequence))
	}
	return cmd
}

func (m *uiModel) applyDeferredCommittedTailDelivery(evt clientui.Event, totalEntries int) (tea.Cmd, bool) {
	if m == nil || len(evt.TranscriptEntries) == 0 {
		return nil, false
	}
	start := evt.CommittedEntryStart
	if !evt.CommittedEntryStartSet {
		start = m.transcriptBaseOffset + len(committedTranscriptEntriesForDeferredTail(m.transcriptEntries))
	}
	localIndex := start - m.transcriptBaseOffset
	if localIndex < 0 || localIndex > len(m.transcriptEntries) {
		return m.requestRuntimeCommittedGapSync(), false
	}
	converted := make([]tui.TranscriptEntry, 0, len(evt.TranscriptEntries))
	for _, entry := range evt.TranscriptEntries {
		converted = append(converted, transcriptEntryFromProjectedChatEntry(entry, false, evt.CommittedTranscriptChanged))
	}
	nextEntries := make([]tui.TranscriptEntry, 0, len(m.transcriptEntries)+len(converted))
	nextEntries = append(nextEntries, m.transcriptEntries[:localIndex]...)
	nextEntries = append(nextEntries, converted...)
	nextEntries = append(nextEntries, m.transcriptEntries[localIndex:]...)
	m.transcriptEntries = nextEntries
	m.transcriptRevision = max(m.transcriptRevision, evt.TranscriptRevision)
	m.transcriptTotalEntries = max(m.transcriptTotalEntries, max(totalEntries, m.transcriptBaseOffset+len(m.transcriptEntries)))
	m.transcriptLiveDirty = true
	m.refreshRollbackCandidates()
	if m.view.Mode() == tui.ModeOngoing {
		m.forwardToView(tui.SetConversationMsg{
			BaseOffset:   m.transcriptBaseOffset,
			TotalEntries: m.transcriptTotalEntries,
			Entries:      append([]tui.TranscriptEntry(nil), m.transcriptEntries...),
			Ongoing:      m.view.OngoingStreamingText(),
			OngoingError: m.view.OngoingErrorText(),
		})
	}
	if m.detailTranscript.loaded && !m.detailTranscript.hasMoreBelow {
		page := clientui.TranscriptPage{
			Revision:     m.transcriptRevision,
			Offset:       start,
			TotalEntries: m.transcriptTotalEntries,
			Entries:      cloneChatEntries(evt.TranscriptEntries),
			Streaming:    m.view.OngoingStreamingText(),
		}
		m.detailTranscript.apply(page)
	}
	return m.syncNativeHistoryFromTranscript(), true
}

func (m *uiModel) advanceDeferredDrainChainThroughLoadedPendingEntries(chainEnd int, target int) int {
	if m == nil || target <= chainEnd {
		return chainEnd
	}
	for chainEnd < target {
		localIndex := chainEnd - m.transcriptBaseOffset
		if localIndex < 0 || localIndex >= len(m.transcriptEntries) {
			return chainEnd
		}
		if !unresolvedToolCallEntryAt(m.transcriptEntries, localIndex) {
			return chainEnd
		}
		chainEnd++
	}
	return chainEnd
}

func unresolvedToolCallEntryAt(entries []tui.TranscriptEntry, index int) bool {
	if index < 0 || index >= len(entries) {
		return false
	}
	entry := entries[index]
	if tui.TranscriptRoleFromWire(string(entry.Role)) != tui.TranscriptRoleToolCall {
		return false
	}
	toolCallID := strings.TrimSpace(entry.ToolCallID)
	if toolCallID == "" {
		return false
	}
	for idx := index + 1; idx < len(entries); idx++ {
		candidate := entries[idx]
		if strings.TrimSpace(candidate.ToolCallID) != toolCallID {
			continue
		}
		if tui.TranscriptRoleFromWire(string(candidate.Role)).IsToolResult() {
			return false
		}
	}
	return true
}

func (m *uiModel) drainDeferredCommittedSuffixRefreshIfUnblocked() tea.Cmd {
	if m == nil || !m.deferredCommittedSuffixRefreshSet || m.ongoingCommittedScrollbackGateActive() {
		return nil
	}
	m.deferredCommittedSuffixRefreshSet = false
	return m.requestRuntimeCommittedTranscriptSuffix(clientui.CommittedTranscriptSuffixRequest{})
}
