package runtime

import (
	"fmt"
	"strings"

	"core/server/llm"
	"core/server/session"
	"core/shared/textutil"
	"core/shared/transcript"
)

// Both live commits and bounded restoration install the same durable generation.
// Preparation changes readiness, not the completed compaction's lifecycle.
func (e *Engine) installHistoryReplacement(record session.EventRecord, replacement historyReplacementPayload) ([]ChatEntry, error) {
	provenance, err := transcriptProvenanceFromRecord(record)
	if err != nil {
		return nil, err
	}
	entries := transcriptEntriesFromHistoryReplacement(replacement)
	for index := range entries {
		entries[index].StepID = record.StepID()
	}
	entries = assignHistoryReplacementEntryProvenance(entries, &provenance)
	if replacement.Output != nil {
		e.setGenerationContext(pendingGenerationContext{replacement: replacement})
		e.transcriptRuntimeState().BeginGeneration(record.StepID(), *replacement.CommittedEntryStart, entries)
	} else {
		e.setGenerationContext(preparedGenerationContext{})
		e.transcriptRuntimeState().ReplaceHistoryAtCommittedEntryStart(
			record.StepID(), replacement.Items, replacement.CommittedEntryStart, entries,
		)
	}
	e.transcriptRuntimeState().SeedLastCommittedAssistantFinalAnswerIfAbsent(replacement.LastCommittedAssistantFinalAnswer)
	mode := session.CompactionMode(replacement.Mode)
	if err := e.compactionRuntimeState().SetHistoryReplacementMode(&mode); err != nil {
		return nil, fmt.Errorf("install history replacement mode: %w", err)
	}
	if replacement.CompactionNumber != nil {
		e.compactionRuntimeState().SetCount(*replacement.CompactionNumber)
	} else {
		count := e.compactionRuntimeState().IncrementCount()
		stepID, _ := textutil.OptionalExact(record.StepID())
		e.persistCompletedCompactionFactsBestEffort(stepID, count)
	}
	return entries, nil
}

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
	if replacement.Output != nil {
		items = append(llm.CloneResponseItems(replacement.Output.summary), llm.ItemsFromMessages(replacement.Output.continuationMessages())...)
	}
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
