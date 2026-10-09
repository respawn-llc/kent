package runtime

import (
	"context"
	"errors"

	"core/prompts"
	"core/server/llm"
	"core/server/session"
	"core/shared/textutil"
)

// A compacted output becomes request-ready only when the next operation supplies
// its context. That can happen immediately or after a dormant interval.
type generationContext interface {
	generationContext()
}

type freshGenerationContext struct{}
type preparedGenerationContext struct{}
type pendingGenerationContext struct {
	replacement historyReplacementPayload
}

func (freshGenerationContext) generationContext()    {}
func (preparedGenerationContext) generationContext() {}
func (pendingGenerationContext) generationContext()  {}

type compactionHistory interface {
	compactionHistory()
}

type compactionOutput struct {
	summary              []llm.ResponseItem
	runningShells        []llm.Message
	preservedUserMessage *llm.Message
	futureAgentMessage   *llm.Message
}

type preparedCompactionHistory struct {
	items        []llm.ResponseItem
	continuation []llm.ResponseItem
}

func (compactionOutput) compactionHistory()          {}
func (preparedCompactionHistory) compactionHistory() {}

func (output compactionOutput) estimateTokens(estimator llm.TokenEstimator) int {
	return llm.EstimateItemsTokens(estimator, output.summary) +
		llm.EstimateItemsTokens(estimator, llm.ItemsFromMessages(output.runningShells)) +
		llm.EstimateItemsTokens(estimator, llm.ItemsFromMessages(output.continuationMessages()))
}

func (output compactionOutput) continuationMessages() []llm.Message {
	var messages []llm.Message
	for _, message := range []*llm.Message{output.preservedUserMessage, output.futureAgentMessage} {
		if message != nil {
			messages = append(messages, *message)
		}
	}
	return messages
}

func (output compactionOutput) prepare(engine session.CompactionEngine, meta metaContextProjection) preparedCompactionHistory {
	var items []llm.ResponseItem
	if engine == session.CompactionEngineRemote {
		items = append(items, llm.ItemsFromMessages([]llm.Message{{
			Role: llm.RoleDeveloper, Content: textutil.Value(prompts.CompactionContinuationReminder),
		}})...)
	}
	items = append(items, llm.ItemsFromMessages(meta.StablePrefix)...)
	items = append(items, output.summary...)
	items = append(items, llm.ItemsFromMessages(output.runningShells)...)
	items = append(items, llm.ItemsFromMessages(meta.Environment)...)
	items = append(items, llm.ItemsFromMessages(output.continuationMessages())...)
	return preparedCompactionHistory{items: items}
}

func (e *Engine) prepareGenerationContext(ctx context.Context, stepID string, pending pendingGenerationContext) error {
	output := pending.replacement.Output
	meta, err := e.compactionReinjectedMetaContextProjection(ctx, compactionMode(pending.replacement.Mode))
	if err != nil {
		return err
	}
	history := output.prepare(session.CompactionEngine(pending.replacement.Engine), meta)
	history.continuation = e.transcriptRuntimeState().SnapshotItems()
	receipt, err := e.steerWithCommitReceipt(stepID, steerHistoryReplacementIntent(
		session.HistoryReplacementPreparation,
		pending.replacement.Engine,
		compactionMode(pending.replacement.Mode),
		*pending.replacement.CompactionNumber,
		pending.replacement.LastCommittedAssistantFinalAnswer,
		history,
	))
	if err != nil {
		return err
	}
	if !receipt.Committed {
		return errors.New("generation context was not committed")
	}
	return nil
}

func compactionOutputFromRecord(record session.CompactedOutput) (compactionOutput, error) {
	output := compactionOutput{}
	for _, item := range record.Summary {
		output.summary = append(output.summary, llmResponseItemFromSessionHistory(item))
	}
	for _, record := range record.RunningShells {
		message, err := llmMessageFromSessionRecord(record)
		if err != nil {
			return compactionOutput{}, err
		}
		output.runningShells = append(output.runningShells, message)
	}
	if record.PreservedUserMessage != nil {
		message, err := llmMessageFromSessionRecord(*record.PreservedUserMessage)
		if err != nil {
			return compactionOutput{}, err
		}
		output.preservedUserMessage = &message
	}
	if record.FutureAgentMessage != nil {
		message, err := llmMessageFromSessionRecord(*record.FutureAgentMessage)
		if err != nil {
			return compactionOutput{}, err
		}
		output.futureAgentMessage = &message
	}
	return output, nil
}

func compactedOutputRecord(output compactionOutput) (session.CompactedOutput, error) {
	record := session.CompactedOutput{}
	for index, item := range llm.PrepareOpenAIInputItems(output.summary) {
		stored, err := sessionProviderHistoryItemFromLLM(index, item)
		if err != nil {
			return session.CompactedOutput{}, err
		}
		record.Summary = append(record.Summary, stored)
	}
	for _, message := range output.runningShells {
		stored, err := sessionMessageRecordFromLLM(message)
		if err != nil {
			return session.CompactedOutput{}, err
		}
		record.RunningShells = append(record.RunningShells, stored)
	}
	if output.preservedUserMessage != nil {
		stored, err := sessionMessageRecordFromLLM(*output.preservedUserMessage)
		if err != nil {
			return session.CompactedOutput{}, err
		}
		record.PreservedUserMessage = &stored
	}
	if output.futureAgentMessage != nil {
		stored, err := sessionMessageRecordFromLLM(*output.futureAgentMessage)
		if err != nil {
			return session.CompactedOutput{}, err
		}
		record.FutureAgentMessage = &stored
	}
	return record, nil
}

func (e *Engine) invalidateCompactedRuntime() {
	e.lockedContractState().Clear()
	e.resetPromptCacheObservationBaselines()
	e.resetLocalDiagnostics()
	e.compactionRuntimeState().SetSoonReminderIssued(false)
}

func (e *Engine) finishCompactionCommit(stepID string) error {
	return errors.Join(
		e.emitRaw(Event{Kind: EventConversationUpdated, StepID: exactStepIDPointer(stepID)}),
		e.resetWorkflowProtocolViolationBudget(context.Background()),
		e.store.SetCompactionSoonReminderIssued(false),
	)
}
