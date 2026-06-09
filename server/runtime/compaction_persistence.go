package runtime

import (
	"errors"
	"fmt"
	"strings"

	"builder/server/llm"
)

type compactionPersistence struct {
	engine *Engine
}

func newCompactionPersistence(engine *Engine) compactionPersistence {
	return compactionPersistence{engine: engine}
}

func (e *Engine) replaceHistory(stepID, engine string, mode compactionMode, items []llm.ResponseItem) error {
	return newCompactionPersistence(e).replaceHistory(stepID, engine, mode, items)
}

func (p compactionPersistence) replaceHistory(stepID, engine string, mode compactionMode, items []llm.ResponseItem) error {
	e := p.engine
	preparedItems := llm.PrepareOpenAIInputItems(items)
	payload := historyReplacementPayload{
		Engine: normalizeHistoryReplacementEngine(engine),
		Mode:   string(mode),
		Items:  llm.CloneResponseItems(preparedItems),
	}
	reminderIssued := false
	projectedStart := e.CommittedTranscriptEntryCount()
	projectedEntries := transcriptEntriesFromHistoryReplacement(payload.Items)
	if err := e.store.SetAgentsInjected(false); err != nil {
		return err
	}
	_, committed, appendErr := e.store.AppendEventWithCommitStatus(stepID, "history_replaced", payload)
	if appendErr != nil && !committed {
		return appendErr
	}
	e.resetCurrentPreciseInputTracking()
	e.resetLocalDiagnostics()
	e.transcriptPersistence().ReplaceHistory(payload.Items)
	e.setCompactionSoonReminderIssued(false)
	p.emitProjectedHistoryReplacementEntries(stepID, projectedStart, projectedEntries)
	conversationErr := e.steerConversationUpdated(stepID)
	return errors.Join(
		appendErr,
		conversationErr,
		e.store.SetCompactionSoonReminderIssued(reminderIssued),
		e.store.SetUsageState(nil),
	)
}

func (p compactionPersistence) emitProjectedHistoryReplacementEntries(stepID string, start int, entries []ChatEntry) {
	e := p.engine
	if e == nil || len(entries) == 0 {
		return
	}
	// Live subscribers must observe the same committed transcript progression that
	// restart hydration reconstructs from history_replaced. Emit projected
	// compaction rows before any later local entry.
	if start < 0 {
		start = 0
	}
	for idx, entry := range entries {
		copyEntry := clonePersistedChatEntry(entry)
		_ = e.steerEvent(stepID, Event{
			Kind:                       EventLocalEntryAdded,
			StepID:                     stepID,
			LocalEntry:                 &copyEntry,
			CommittedTranscriptChanged: true,
			CommittedEntryStart:        start + idx,
			CommittedEntryStartSet:     true,
		})
	}
}

func (e *Engine) emitCompactionStatus(stepID string, kind EventKind, mode compactionMode, engine, provider string, trimmed, count int, errText string) error {
	return newCompactionPersistence(e).emitStatus(stepID, kind, mode, engine, provider, trimmed, count, errText)
}

func (p compactionPersistence) emitStatus(stepID string, kind EventKind, mode compactionMode, engine, provider string, trimmed, count int, errText string) error {
	e := p.engine
	status := &CompactionStatus{
		Mode:              string(mode),
		Engine:            strings.TrimSpace(engine),
		Provider:          strings.TrimSpace(provider),
		TrimmedItemsCount: trimmed,
		Count:             count,
		Error:             strings.TrimSpace(errText),
	}

	switch kind {
	case EventCompactionStarted:
		return e.steerEvent(stepID, Event{
			Kind:       kind,
			StepID:     stepID,
			Compaction: status,
		})
	case EventCompactionCompleted:
		return e.steerEvent(stepID, Event{
			Kind:       kind,
			StepID:     stepID,
			Compaction: status,
		})
	case EventCompactionFailed:
		message := fmt.Sprintf("Context compaction failed (%s): %s", status.Mode, status.Error)
		if strings.TrimSpace(status.Error) == "" {
			message = fmt.Sprintf("Context compaction failed (%s).", status.Mode)
		}
		if err := e.steer(stepID, steerLocalEntryIntent(storedLocalEntry{Role: "error", Text: message})); err != nil {
			_ = e.steerEvent(stepID, Event{
				Kind:       kind,
				StepID:     stepID,
				Compaction: status,
			})
			return err
		}
		return e.steerEvent(stepID, Event{
			Kind:       kind,
			StepID:     stepID,
			Compaction: status,
		})
	default:
		return nil
	}
}
