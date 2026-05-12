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
	payload := historyReplacementPayload{
		Engine: normalizeHistoryReplacementEngine(engine),
		Mode:   string(mode),
		Items:  llm.CloneResponseItems(items),
	}
	reminderIssued := false
	projectedStart := e.CommittedTranscriptEntryCount()
	projectedEntries := transcriptEntriesFromHistoryReplacement(payload.Items)
	if _, err := e.store.AppendEvent(stepID, "history_replaced", payload); err != nil {
		return err
	}
	e.resetCurrentPreciseInputTracking()
	e.resetLocalDiagnostics()
	e.transcriptPersistence().ReplaceHistory(payload.Items)
	e.setCompactionSoonReminderIssued(false)
	p.emitProjectedHistoryReplacementEntries(stepID, projectedStart, projectedEntries)
	e.emitConversationUpdated(stepID)
	return errors.Join(
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
		e.emit(Event{
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
		e.emit(Event{
			Kind:       kind,
			StepID:     stepID,
			Compaction: status,
		})
		return nil
	case EventCompactionCompleted:
		e.emit(Event{
			Kind:       kind,
			StepID:     stepID,
			Compaction: status,
		})
		return nil
	case EventCompactionFailed:
		message := fmt.Sprintf("Context compaction failed (%s): %s", status.Mode, status.Error)
		if strings.TrimSpace(status.Error) == "" {
			message = fmt.Sprintf("Context compaction failed (%s).", status.Mode)
		}
		if err := e.appendPersistedLocalEntry(stepID, "error", message); err != nil {
			e.emit(Event{
				Kind:       kind,
				StepID:     stepID,
				Compaction: status,
			})
			return err
		}
		e.emit(Event{
			Kind:       kind,
			StepID:     stepID,
			Compaction: status,
		})
		return nil
	default:
		return nil
	}
}
