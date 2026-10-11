package runtime

import (
	"strings"

	"core/server/session"
	"core/shared/runtimeids"
	"core/shared/textutil"
)

type compactionPersistence struct {
	engine *Engine
}

func newCompactionPersistence(engine *Engine) compactionPersistence {
	return compactionPersistence{engine: engine}
}

func (p compactionPersistence) replaceHistory(stepID, engine string, mode compactionMode, history compactionOutput) (session.CommitReceipt, error) {
	e := p.engine
	return e.steerWithCommitReceipt(stepID, steerHistoryReplacementIntent(engine, mode, e.compactionRuntimeState().Count()+1, e.LastCommittedAssistantFinalAnswer(), history))
}

func (p compactionPersistence) setActivity(
	stepID string,
	requestID *runtimeids.CompactionRequestID,
	mode compactionMode,
	count int,
	activeKind ActiveKind,
	active bool,
) error {
	return p.engine.steer(stepID, steerCompactionActivityIntent(active, requestID, string(mode), count, activeKind))
}

func (p compactionPersistence) emitStatus(
	stepID string,
	requestID *runtimeids.CompactionRequestID,
	kind EventKind,
	mode compactionMode,
	engine, provider string,
	trimmed *int,
	count int,
	errText string,
) error {
	e := p.engine
	status := &CompactionStatus{
		Mode:              string(mode),
		RequestID:         requestID,
		Engine:            strings.TrimSpace(engine),
		Provider:          strings.TrimSpace(provider),
		TrimmedItemsCount: textutil.Pointer(trimmed),
		Count:             count,
		Error:             strings.TrimSpace(errText),
	}

	switch kind {
	case EventCompactionStarted, EventCompactionCompleted, EventCompactionFailed:
		return e.steer(stepID, steerEventIntent(Event{
			Kind:       kind,
			StepID:     textutil.Value(stepID),
			Compaction: status,
		}))

	default:
		return nil
	}
}
