package app

import (
	"core/cli/tui"
	"core/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

func shouldDeliverCommittedRuntimeEventFromSuffix(m *uiModel, evt clientui.Event) bool {
	if m == nil || !evt.CommittedTranscriptChanged || len(evt.TranscriptEntries) == 0 {
		return false
	}
	state := newProjectedTranscriptEventState(projectedTranscriptEventSnapshotFromModel(m))
	if evt.Kind == clientui.EventUserMessageFlushed {
		return false
	}
	if projectedEventIsLiveOnlyUnresolvedToolStart(state, evt) {
		return false
	}
	if m.ongoingCommittedScrollbackGateActive() {
		return false
	}
	if _, ok := m.runtimeClient().(interface {
		RefreshCommittedTranscriptSuffix(clientui.CommittedTranscriptSuffixRequest) (clientui.CommittedTranscriptSuffix, error)
	}); !ok {
		return false
	}
	return true
}

func committedTranscriptSuffixFromEvent(m *uiModel, evt clientui.Event) (clientui.CommittedTranscriptSuffix, bool) {
	if m == nil || !evt.CommittedEntryStartSet {
		return clientui.CommittedTranscriptSuffix{}, false
	}
	delta := evt.CommittedEntryCount - evt.CommittedEntryStart
	if delta < 0 || len(evt.TranscriptEntries) != delta {
		return clientui.CommittedTranscriptSuffix{}, false
	}
	if evt.TranscriptRevision < m.transcriptRevision {
		return clientui.CommittedTranscriptSuffix{}, false
	}
	if evt.CommittedEntryStart > committedTranscriptTailEnd(m) {
		return clientui.CommittedTranscriptSuffix{}, false
	}
	for i := range evt.TranscriptEntries {
		if isNoopFinalText(evt.TranscriptEntries[i].Text) {
			return clientui.CommittedTranscriptSuffix{}, false
		}
	}
	return clientui.CommittedTranscriptSuffix{
		SessionID:             m.sessionID,
		SessionName:           m.sessionName,
		ConversationFreshness: m.currentConversationFreshness(),
		Revision:              evt.TranscriptRevision,
		CommittedEntryCount:   evt.CommittedEntryCount,
		StartEntryCount:       evt.CommittedEntryStart,
		NextEntryCount:        evt.CommittedEntryCount,
		HasMore:               false,
		Entries:               append([]clientui.ChatEntry(nil), evt.TranscriptEntries...),
	}, true
}

func (m *uiModel) applyCommittedTranscriptSuffixFromEvent(suffix clientui.CommittedTranscriptSuffix) tea.Cmd {
	if m == nil {
		return nil
	}
	m.runtimeCommittedSuffixToken++
	token := m.runtimeCommittedSuffixToken
	return func() tea.Msg {
		return runtimeCommittedTranscriptSuffixRefreshedMsg{token: token, req: clientui.CommittedTranscriptSuffixRequest{}, suffix: suffix}
	}
}

func suffixSessionChanged(m *uiModel, suffix clientui.CommittedTranscriptSuffix) bool {
	if m == nil || suffix.SessionID == "" || m.sessionID == "" {
		return false
	}
	return suffix.SessionID != m.sessionID
}

func committedTranscriptSuffixStartsAfterDeliveryCursor(m *uiModel, suffix clientui.CommittedTranscriptSuffix) bool {
	if m == nil {
		return false
	}
	expectedStart := committedTranscriptTailEnd(m)
	suffix = m.trimCommittedTranscriptSuffixToDeliveryCursor(suffix)
	if suffix.NextEntryCount <= suffix.StartEntryCount || suffix.StartEntryCount <= expectedStart {
		return false
	}
	return suffix.StartEntryCount > loadedTranscriptTailEnd(m)
}

func committedTranscriptTailEnd(m *uiModel) int {
	if m == nil {
		return 0
	}
	if m.ongoingCommittedDelivery.initialized {
		return m.ongoingCommittedDelivery.lastAppliedCommittedEntryCount
	}
	return committedTranscriptLocalFrontierEnd(m)
}

func committedTranscriptLocalFrontierEnd(m *uiModel) int {
	if m == nil {
		return 0
	}
	committedCount := 0
	for _, entry := range m.transcriptEntries {
		if !entry.Transient || entry.Committed {
			committedCount++
		}
	}
	end := m.transcriptBaseOffset + committedCount
	if end < 0 {
		return 0
	}
	return end
}

func committedOngoingLocalFrontierEnd(m *uiModel) int {
	if m == nil {
		return 0
	}
	return m.transcriptBaseOffset + len(committedTranscriptEntriesForApp(m.transcriptEntries))
}

func loadedTranscriptTailEnd(m *uiModel) int {
	if m == nil {
		return 0
	}
	end := m.transcriptBaseOffset + len(m.transcriptEntries)
	if end < 0 {
		return 0
	}
	return end
}

func (m *uiModel) truncatePendingRecentTailBeforeSuffix(startEntryCount int) {
	if m == nil {
		return
	}
	localIndex := startEntryCount - m.transcriptBaseOffset
	if localIndex < 0 || localIndex > len(m.transcriptEntries) {
		return
	}
	if localIndex == len(m.transcriptEntries) {
		return
	}
	m.transcriptEntries = append([]tui.TranscriptEntry(nil), m.transcriptEntries[:localIndex]...)
	if m.view.Mode() == tui.ModeOngoing {
		m.forwardToView(tui.SetConversationMsg{
			BaseOffset:   m.transcriptBaseOffset,
			TotalEntries: m.transcriptTotalEntries,
			Entries:      append([]tui.TranscriptEntry(nil), m.transcriptEntries...),
			Ongoing:      m.view.OngoingStreamingText(),
			OngoingError: m.view.OngoingErrorText(),
		})
	}
}

func (m *uiModel) applyCommittedTranscriptSuffixAppend(suffix clientui.CommittedTranscriptSuffix) tea.Cmd {
	if m == nil {
		return nil
	}
	if !m.ongoingCommittedDelivery.initialized {
		m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(committedTranscriptTailEnd(m), m.transcriptRevision)
	}
	// Multiple committed events can race their suffix reads while final-answer
	// streaming is being finalized. Trim any already-delivered overlap so a late
	// suffix cannot re-emit the final answer that advanced the delivery cursor.
	suffix = m.trimCommittedTranscriptSuffixToDeliveryCursor(suffix)
	if suffix.NextEntryCount <= suffix.StartEntryCount {
		m.transcriptRevision = max(m.transcriptRevision, suffix.Revision)
		m.transcriptTotalEntries = max(m.transcriptTotalEntries, suffix.CommittedEntryCount)
		return nil
	}
	page := transcriptPageFromCommittedTranscriptSuffix(suffix)
	entries := transcriptEntriesFromPage(page)
	expectedStart := committedTranscriptTailEnd(m)
	if page.Offset > expectedStart && page.Offset <= loadedTranscriptTailEnd(m) {
		expectedStart = page.Offset
	}
	if page.Offset != expectedStart {
		if page.Offset > expectedStart {
			return m.requestRuntimeCommittedGapSync()
		}
		m.runtimeAdapter().applyAuthoritativeRecentTailPage(page, entries, false)
		if m.view.Mode() == tui.ModeOngoing {
			m.forwardToView(tui.SetConversationMsg{
				BaseOffset:   page.Offset,
				TotalEntries: page.TotalEntries,
				Entries:      entries,
				Ongoing:      page.Streaming,
				OngoingError: page.StreamingError,
			})
		}
		return m.syncNativeHistoryFromTranscript()
	}
	if len(entries) == 0 && suffix.NextEntryCount > suffix.StartEntryCount {
		m.transcriptRevision = max(m.transcriptRevision, suffix.Revision)
		m.transcriptTotalEntries = max(m.transcriptTotalEntries, suffix.CommittedEntryCount)
		m.transcriptLiveDirty = true
		m.ongoingCommittedDelivery.markApplied(max(committedOngoingLocalFrontierEnd(m), suffix.NextEntryCount), suffix.Revision)
		m.refreshRollbackCandidates()
		if m.view.Mode() == tui.ModeDetail {
			m.detailTranscript.apply(page)
		}
		return nil
	}
	m.truncatePendingRecentTailBeforeSuffix(expectedStart)
	if shouldClearAssistantStreamForCommittedTranscriptEntries(entries, m.view.OngoingStreamingText()) {
		m.clearAssistantStreamForCommittedAppend()
	}
	for _, entry := range entries {
		m.transcriptEntries = append(m.transcriptEntries, entry)
		if m.view.Mode() == tui.ModeOngoing {
			m.forwardToView(appendTranscriptMsgFromEntry(entry))
		}
	}
	m.transcriptRevision = max(m.transcriptRevision, suffix.Revision)
	m.transcriptTotalEntries = max(m.transcriptTotalEntries, suffix.CommittedEntryCount)
	m.transcriptLiveDirty = true
	m.ongoingCommittedDelivery.markApplied(committedOngoingLocalFrontierEnd(m), suffix.Revision)
	m.refreshRollbackCandidates()
	if m.view.Mode() == tui.ModeDetail {
		m.detailTranscript.apply(page)
	}
	beforeSequence := m.nativeFlushSequence
	cmd := m.syncNativeHistoryFromTranscript()
	return sequenceCmds(cmd, m.trackOngoingCommittedSuffixFlush(suffix, beforeSequence))
}

func (m *uiModel) trackOngoingCommittedSuffixFlush(suffix clientui.CommittedTranscriptSuffix, beforeSequence uint64) tea.Cmd {
	if m == nil || !m.ongoingCommittedDelivery.initialized || suffix.NextEntryCount <= suffix.StartEntryCount {
		return nil
	}
	emittedEnd := committedOngoingLocalFrontierEnd(m)
	if emittedEnd <= m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount {
		return nil
	}
	if !m.shouldEmitNativeHistory() {
		m.ongoingCommittedDelivery.recordCommittedAdvance(emittedEnd, suffix.Revision)
		return nil
	}
	if m.nativeFlushSequence <= beforeSequence {
		m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount = emittedEnd
		m.ongoingCommittedDelivery.lastEmittedTranscriptRevision = suffix.Revision
		return nil
	}
	emittedSuffix := suffix
	emittedSuffix.NextEntryCount = emittedEnd
	if err := m.ongoingCommittedDelivery.beginNativeFlush(emittedSuffix, m.nativeFlushSequence); err != nil {
		m.logf(
			"ui.runtime.committed_suffix.begin_flush_failed err=%q sequence=%d start=%d next=%d revision=%d cursor=%d",
			err.Error(),
			m.nativeFlushSequence,
			emittedSuffix.StartEntryCount,
			emittedSuffix.NextEntryCount,
			emittedSuffix.Revision,
			m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount,
		)
		m.ongoingCommittedDelivery.recordPendingRange(
			m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount,
			emittedEnd,
			suffix.Revision,
		)
		return m.requestRuntimeCommittedGapSync()
	}
	return nil
}

func (m *uiModel) trimCommittedTranscriptSuffixToDeliveryCursor(suffix clientui.CommittedTranscriptSuffix) clientui.CommittedTranscriptSuffix {
	if m == nil {
		return suffix
	}
	expectedStart := committedTranscriptTailEnd(m)
	if suffix.StartEntryCount >= expectedStart {
		return normalizeCommittedTranscriptSuffixEntryWindow(suffix)
	}
	if suffix.NextEntryCount <= expectedStart {
		suffix.StartEntryCount = expectedStart
		suffix.NextEntryCount = expectedStart
		suffix.Entries = nil
		return suffix
	}
	skip := expectedStart - suffix.StartEntryCount
	if skip <= 0 {
		return normalizeCommittedTranscriptSuffixEntryWindow(suffix)
	}
	if skip >= len(suffix.Entries) {
		suffix.Entries = nil
	} else {
		suffix.Entries = append([]clientui.ChatEntry(nil), suffix.Entries[skip:]...)
	}
	suffix.StartEntryCount = expectedStart
	return normalizeCommittedTranscriptSuffixEntryWindow(suffix)
}

func normalizeCommittedTranscriptSuffixEntryWindow(suffix clientui.CommittedTranscriptSuffix) clientui.CommittedTranscriptSuffix {
	expectedCount := suffix.NextEntryCount - suffix.StartEntryCount
	if expectedCount < 0 {
		suffix.NextEntryCount = suffix.StartEntryCount
		suffix.Entries = nil
		return suffix
	}
	if len(suffix.Entries) > expectedCount {
		suffix.Entries = append([]clientui.ChatEntry(nil), suffix.Entries[len(suffix.Entries)-expectedCount:]...)
	}
	if len(suffix.Entries) < expectedCount {
		suffix.NextEntryCount = suffix.StartEntryCount + len(suffix.Entries)
	}
	return suffix
}
