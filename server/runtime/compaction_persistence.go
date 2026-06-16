package runtime

import (
	"fmt"
	"strings"

	"core/server/llm"
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
	workflowRunID := ""
	if e.cfg.WorkflowRun != nil {
		workflowRunID = strings.TrimSpace(string(e.cfg.WorkflowRun.RunID))
	}
	return e.steer(stepID, steerHistoryReplacementIntent(engine, mode, workflowRunID, items))
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
