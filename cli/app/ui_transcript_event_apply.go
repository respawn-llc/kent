package app

import (
	"strconv"

	"core/cli/tui"
	"core/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

func (a uiRuntimeAdapter) applyProjectedTranscriptEntries(evt clientui.Event, flushNativeHistory bool) (tea.Cmd, bool, bool) {
	m := a.model
	if len(evt.TranscriptEntries) == 0 {
		return nil, false, false
	}
	incomingCount := len(evt.TranscriptEntries)
	reduction := reduceProjectedTranscriptEvent(newProjectedTranscriptEventState(projectedTranscriptEventSnapshotFromModel(m)), evt)
	if reduction.decision == projectedTranscriptDecisionSkip && reduction.duplicateToolStarts {
		m.logTranscriptEventDiag("transcript.diag.client.append_entries", evt, map[string]string{
			"path":           "live_event",
			"incoming_count": strconv.Itoa(incomingCount),
			"reason":         reduction.skipReason,
			"applied_count":  "0",
		})
		return nil, false, false
	}
	plan := reduction.plan
	m.logProjectedTranscriptPlanDiag(evt, plan, incomingCount)
	switch reduction.decision {
	case projectedTranscriptDecisionSkip:
		if evt.CommittedTranscriptChanged {
			m.transcriptRevision = max(m.transcriptRevision, evt.TranscriptRevision)
			m.transcriptTotalEntries = max(m.transcriptTotalEntries, evt.CommittedEntryCount)
		}
		m.logTranscriptEventDiag("transcript.diag.client.append_entries", evt, map[string]string{
			"path":           "live_event",
			"incoming_count": strconv.Itoa(incomingCount),
			"reason":         reduction.skipReason,
			"applied_count":  "0",
		})
		return nil, false, false
	case projectedTranscriptDecisionHydrate:
		m.beginCommittedTranscriptContinuityRecovery()
		m.logTranscriptEventDiag("transcript.diag.client.append_entries", evt, map[string]string{
			"path":           "live_event",
			"incoming_count": strconv.Itoa(incomingCount),
			"reason":         "requires_hydration",
			"divergence":     plan.divergence,
			"applied_count":  "0",
		})
		if m.hasRuntimeClient() {
			if reduction.hydrationCause != clientui.TranscriptRecoveryCauseNone {
				return m.requestRuntimeTranscriptSyncForContinuityLoss(reduction.hydrationCause), false, true
			}
			return m.requestRuntimeCommittedGapSync(), false, true
		}
		return nil, false, false
	case projectedTranscriptDecisionDefer:
		m.deferProjectedCommittedTail(evt)
		m.logTranscriptEventDiag("transcript.diag.client.append_entries", evt, map[string]string{
			"path":           "live_event",
			"incoming_count": strconv.Itoa(incomingCount),
			"reason":         reduction.skipReason,
			"applied_count":  "0",
		})
		return nil, false, false
	}
	entries := plan.entries
	m.transcriptLiveDirty = true
	startOffset := m.transcriptBaseOffset + plan.rangeStart
	convertedEntries := make([]tui.TranscriptEntry, 0, len(entries))
	for _, entry := range entries {
		convertedEntries = append(convertedEntries, transcriptEntryFromProjectedChatEntry(entry, reduction.projectedTransient, reduction.projectedCommitted))
	}
	if shouldClearAssistantStreamForCommittedTranscriptEntries(convertedEntries, m.view.OngoingStreamingText()) {
		m.clearAssistantStreamForCommittedAppend()
	}
	showTransientInCurrentView := m.view.Mode() != tui.ModeDetail || !allTranscriptEntriesTransient(convertedEntries)
	replaceLoadedTransientEntries := shouldReplaceLoadedTransientEntriesWithCommittedAppend(m, convertedEntries)
	if plan.mode == projectedTranscriptEntryPlanAppend {
		for _, transcriptEntry := range convertedEntries {
			m.transcriptEntries = append(m.transcriptEntries, transcriptEntry)
			if showTransientInCurrentView && !replaceLoadedTransientEntries {
				m.forwardToView(appendTranscriptMsgFromEntry(transcriptEntry))
			}
		}
	} else {
		prefix := append([]tui.TranscriptEntry(nil), m.transcriptEntries[:plan.rangeStart]...)
		suffix := append([]tui.TranscriptEntry(nil), m.transcriptEntries[plan.rangeEnd:]...)
		m.transcriptEntries = append(prefix, convertedEntries...)
		m.transcriptEntries = append(m.transcriptEntries, suffix...)
	}
	m.transcriptRevision = max(m.transcriptRevision, evt.TranscriptRevision)
	m.transcriptTotalEntries = max(m.transcriptTotalEntries, max(evt.CommittedEntryCount, m.transcriptBaseOffset+len(m.transcriptEntries)))
	m.observeDirectCommittedEventDelivery(evt)
	m.refreshRollbackCandidates()
	if plan.mode == projectedTranscriptEntryPlanAppend && replaceLoadedTransientEntries {
		m.forwardToView(tui.SetConversationMsg{
			BaseOffset:   m.transcriptBaseOffset,
			TotalEntries: m.transcriptTotalEntries,
			Entries:      append([]tui.TranscriptEntry(nil), m.transcriptEntries...),
			Ongoing:      m.view.OngoingStreamingText(),
			OngoingError: m.view.OngoingErrorText(),
		})
	}
	if m.detailTranscript.loaded && !allTranscriptEntriesTransient(convertedEntries) {
		page := clientui.TranscriptPage{
			Revision:     m.transcriptRevision,
			Offset:       startOffset,
			TotalEntries: m.transcriptTotalEntries,
			Entries:      cloneChatEntries(entries),
			Ongoing:      m.view.OngoingStreamingText(),
			OngoingError: m.view.OngoingErrorText(),
		}
		m.detailTranscript.apply(page)
	}
	if plan.mode == projectedTranscriptEntryPlanReplace && showTransientInCurrentView {
		m.forwardToView(tui.SetConversationMsg{
			BaseOffset:   m.transcriptBaseOffset,
			TotalEntries: m.transcriptTotalEntries,
			Entries:      append([]tui.TranscriptEntry(nil), m.transcriptEntries...),
			Ongoing:      m.view.OngoingStreamingText(),
			OngoingError: m.view.OngoingErrorText(),
		})
	}
	if plan.mode == projectedTranscriptEntryPlanAppend && m.view.Mode() == tui.ModeDetail && !allTranscriptEntriesTransient(convertedEntries) && m.detailTranscript.loaded && m.view.TranscriptBaseOffset() == m.detailTranscript.offset {
		m.forwardToView(tui.SetConversationMsg{
			BaseOffset:   m.detailTranscript.offset,
			TotalEntries: m.detailTranscript.totalEntries,
			Entries:      append([]tui.TranscriptEntry(nil), m.detailTranscript.entries...),
			Ongoing:      m.view.OngoingStreamingText(),
			OngoingError: m.view.OngoingErrorText(),
		})
	}
	if showTransientInCurrentView && m.view.Mode() == tui.ModeOngoing {
		m.forwardToView(tui.SetOngoingScrollMsg{Scroll: m.view.OngoingScroll()})
	}
	if showTransientInCurrentView {
		m.clearMirroredTransientStatus(convertedEntries)
	}
	if !flushNativeHistory {
		m.logProjectedTranscriptAppliedDiag(evt, plan, incomingCount, len(entries), startOffset, entries, false)
		return nil, true, false
	}
	m.logProjectedTranscriptAppliedDiag(evt, plan, incomingCount, len(entries), startOffset, entries, true)
	return m.syncNativeHistoryFromTranscript(), true, false
}

func (m *uiModel) clearMirroredTransientStatus(entries []tui.TranscriptEntry) {
	if m == nil || m.transientStatusNoticeID == "" {
		return
	}
	for _, entry := range entries {
		if entry.NoticeID != m.transientStatusNoticeID {
			continue
		}
		m.transientStatus = ""
		m.transientStatusKind = uiStatusNoticeNeutral
		m.transientStatusNoticeID = ""
		return
	}
}

func (m *uiModel) clearMirroredTransientStatusByNoticeID(noticeID string) {
	if m == nil || m.transientStatusNoticeID == "" || noticeID == "" {
		return
	}
	if noticeID != m.transientStatusNoticeID {
		return
	}
	m.transientStatus = ""
	m.transientStatusKind = uiStatusNoticeNeutral
	m.transientStatusNoticeID = ""
}

func (m *uiModel) observeDirectCommittedEventDelivery(evt clientui.Event) {
	if m == nil || !evt.CommittedTranscriptChanged || evt.CommittedEntryCount <= 0 {
		return
	}
	// User echoes still use the direct event path to preserve prompt responsiveness.
	// Keep the SSOT delivery cursor in step so the following committed suffix starts
	// after that already-emitted user row instead of duplicating it from SSOT.
	if evt.Kind != clientui.EventUserMessageFlushed {
		return
	}
	if !m.ongoingCommittedDelivery.initialized {
		m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(evt.CommittedEntryCount, evt.TranscriptRevision)
		return
	}
	m.ongoingCommittedDelivery.markApplied(evt.CommittedEntryCount, evt.TranscriptRevision)
	if evt.CommittedEntryCount > m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount {
		m.ongoingCommittedDelivery.lastEmittedCommittedEntryCount = evt.CommittedEntryCount
	}
	if evt.TranscriptRevision > m.ongoingCommittedDelivery.lastEmittedTranscriptRevision {
		m.ongoingCommittedDelivery.lastEmittedTranscriptRevision = evt.TranscriptRevision
	}
}
