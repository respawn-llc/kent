package app

import "strings"

func projectedTranscriptEventSnapshotFromModel(m *uiModel) projectedTranscriptEventSnapshot {
	if m == nil {
		return projectedTranscriptEventSnapshot{}
	}
	return projectedTranscriptEventSnapshot{
		entries:              m.transcriptEntries,
		baseOffset:           m.transcriptBaseOffset,
		revision:             m.transcriptRevision,
		hasRuntimeClient:     m.hasRuntimeClient(),
		busy:                 m.isBusy(),
		liveAssistantPending: strings.TrimSpace(m.view.OngoingStreamingText()) != "" || m.sawAssistantDelta,
	}
}

func deferredCommittedTailSnapshotFromModel(m *uiModel) deferredCommittedTailSnapshot {
	if m == nil {
		return deferredCommittedTailSnapshot{}
	}
	return deferredCommittedTailSnapshot{
		tails:            m.deferredCommittedTail,
		committedEntries: committedTranscriptEntriesForApp(m.transcriptEntries),
		baseOffset:       m.transcriptBaseOffset,
		revision:         m.transcriptRevision,
		totalEntries:     m.transcriptTotalEntries,
	}
}
