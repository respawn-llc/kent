package runtime

import (
	"strings"

	"core/server/llm"
	"core/server/session"
	"core/shared/textutil"
	"core/shared/transcript"
)

func normalizeHistoryReplacementEngine(engine string) string {
	engine = strings.TrimSpace(engine)
	if session.IsLegacyReviewerRollbackHistoryReplacementEngine(engine) {
		return ""
	}
	return engine
}

func isCompactionEventRecordBoundary(record session.EventRecord) (bool, error) {
	kind, err := record.Kind()
	if err != nil {
		return false, err
	}
	return session.IsContextBoundary(kind), nil
}

func compactionBoundaryMatcher(matchErr *error) func(session.EventRecord) bool {
	return func(record session.EventRecord) bool {
		matches, err := isCompactionEventRecordBoundary(record)
		if err != nil {
			*matchErr = err
			return true
		}
		return matches
	}
}

func transcriptEntriesFromHistoryReplacement(replacement historyReplacementPayload) []ChatEntry {
	items, compactionNumber := replacement.Items, replacement.CompactionNumber
	entries := make([]ChatEntry, 0, len(items)+1)
	hasCompactionSummary := false
	walker := newResponseItemMessageWalker(func(msg llm.Message) {
		if entry, ok := preservedUserMessageEntry(msg); ok {
			entries = append(entries, entry)
			return
		}
		for _, entry := range VisibleChatEntriesFromMessage(msg) {
			if entry.MessageType == llm.MessageTypeCompactionSummary {
				hasCompactionSummary = true
				entry.CompactionNumber = textutil.Pointer(compactionNumber)
			}
			entries = append(entries, clonePersistedChatEntry(entry))
		}
	})
	for _, item := range items {
		if item.Type == llm.ResponseItemTypeConfigurationUpdate {
			walker.Flush()
			entries = append(entries, configurationUpdateChatEntry(item))
			continue
		}
		walker.Apply(item)
	}
	walker.Flush()

	// Empty legacy replacements are segment boundaries only. Non-empty
	// replacements represent compacted working sets and always receive a notice.
	if !hasCompactionSummary && len(items) > 0 {
		entries = append(
			[]ChatEntry{syntheticCompactionSummaryEntry(compactionNumber)},
			entries...,
		)
	}
	return entries
}

func preservedUserMessageEntry(msg llm.Message) (ChatEntry, bool) {
	if msg.Role != llm.RoleUser || msg.MessageType != nil || msg.Content == nil ||
		strings.TrimSpace(*msg.Content) == "" {
		return ChatEntry{}, false
	}
	// manual_compaction_carryover is the legacy wire name for any user message
	// preserved across a compaction boundary.
	messageType := llm.MessageTypeCompactionPreservedUserMessage
	return ChatEntry{
		Visibility:   messageTypeTranscriptVisibility(&messageType),
		Role:         string(transcript.EntryRoleCompactionPreservedUserMessage),
		Text:         *msg.Content,
		MessageType:  messageType,
		CompactLabel: compactLabelForMessage(llm.Message{MessageType: &messageType}),
	}, true
}

func syntheticCompactionSummaryEntry(compactionNumber *int) ChatEntry {
	return ChatEntry{
		Visibility:       transcript.EntryVisibilityOngoing,
		Role:             string(transcript.EntryRoleCompactionSummary),
		MessageType:      llm.MessageTypeCompactionSummary,
		CompactionNumber: textutil.Pointer(compactionNumber),
	}
}

func assignHistoryReplacementEntryProvenance(
	entries []ChatEntry,
	base *TranscriptCommittedRowProvenance,
) []ChatEntry {
	var ordinal int64
	for index := range entries {
		entries[index].CommittedProvenance = cloneTranscriptCommittedRowProvenance(base)
		if _, projected := transcriptCommittedRowFactFromChatEntry(entries[index]); !projected {
			continue
		}
		ordinal++
		provenance := cloneTranscriptCommittedRowProvenance(base)
		projectedOrdinal := ordinal
		provenance.ProjectedOrdinal = &projectedOrdinal
		entries[index].CommittedProvenance = provenance
	}
	return entries
}
