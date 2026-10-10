package runtime

import (
	"core/server/llm"
	"core/server/session"
	"core/shared/textutil"
)

// Historical prepared records remain readable. These fixtures exercise their
// projection through the normal commit owner without keeping a production
// writer for the superseded compaction format.
type preparedCompactionHistory struct {
	items []llm.ResponseItem
}

func steerTestPreparedHistoryIntent(engine string, mode compactionMode, number int, finalAnswer *string, history preparedCompactionHistory) steeringIntent {
	return steeringIntent{
		priority: steeringPriorityNormal,
		items: []steeringItem{{historyReplace: &steeringHistoryReplacement{payload: historyReplacementPayload{
			Engine: engine, Mode: string(mode), CompactionNumber: &number,
			LastCommittedAssistantFinalAnswer: textutil.Pointer(finalAnswer),
			Items:                             llm.CloneResponseItems(llm.PrepareOpenAIInputItems(history.items)),
		}}}},
	}
}

func replaceTestPreparedHistory(engine *Engine, stepID, provider string, mode compactionMode, history preparedCompactionHistory) (session.CommitReceipt, error) {
	return engine.steerWithCommitReceipt(stepID, steerTestPreparedHistoryIntent(provider, mode, engine.CompactionCount()+1, engine.LastCommittedAssistantFinalAnswer(), history))
}
