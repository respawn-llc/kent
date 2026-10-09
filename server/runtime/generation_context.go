package runtime

import (
	"context"
	"errors"

	"core/prompts"
	"core/server/llm"
	"core/server/session"
	"core/shared/textutil"
)

// A compacted output becomes request-ready at live request dispatch, which can
// happen immediately or after an arbitrarily long dormant interval.
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

func (e *Engine) generationContextSnapshot() generationContext {
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.generationContext
}

func (e *Engine) setGenerationContext(generation generationContext) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.generationContext = generation
}

type compactionHistory interface {
	compactionHistory()
}

type compactionOutput struct {
	summary              []llm.ResponseItem
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
		llm.EstimateItemsTokens(estimator, llm.ItemsFromMessages(output.continuationMessages()))
}

func (e *Engine) estimatedProviderHistoryTokens() int {
	estimated := e.transcriptRuntimeState().EstimatedProviderTokens()
	if pending, ok := e.generationContextSnapshot().(pendingGenerationContext); ok {
		estimated += pending.replacement.Output.estimateTokens(e.cfg.TokenEstimator)
	}
	return estimated
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

func (output compactionOutput) prepare(engine session.CompactionEngine, meta metaContextProjection, runningShells []llm.Message) preparedCompactionHistory {
	var items []llm.ResponseItem
	if engine == session.CompactionEngineRemote {
		items = append(items, llm.ItemsFromMessages([]llm.Message{{
			Role: llm.RoleDeveloper, Content: textutil.Value(prompts.CompactionContinuationReminder),
		}})...)
	}
	items = append(items, llm.ItemsFromMessages(meta.StablePrefix)...)
	items = append(items, output.summary...)
	items = append(items, llm.ItemsFromMessages(runningShells)...)
	items = append(items, llm.ItemsFromMessages(meta.Environment)...)
	items = append(items, llm.ItemsFromMessages(output.continuationMessages())...)
	return preparedCompactionHistory{items: items}
}

// Live request operations prepare a saved generation once. Idle Session
// activation and inspection leave it pending.
func (e *Engine) prepareRequestGenerationContext(ctx context.Context, stepID string) error {
	pending, ok := e.generationContextSnapshot().(pendingGenerationContext)
	if !ok {
		return nil
	}
	if e.workflowPromptActive() {
		var committedErr error
		err := e.currentNodeExecutionSnapshot().delivery.apply(workflowTaskPromptTriggerTaskDelivery, func(trigger workflowTaskPromptTrigger) error {
			// A target assignment already committed after the summary keeps its
			// original row; preparing base context must not deliver it again.
			if workflowAssignmentIdentityFromItems(e.transcriptRuntimeState().SnapshotItems()) != nil {
				trigger = workflowTaskPromptTriggerResumeDelivery
			}
			receipt, err := e.commitGenerationContext(ctx, stepID, pending, trigger)
			if receipt.Committed {
				committedErr = err
				return nil
			}
			return err
		})
		return errors.Join(err, committedErr)
	}
	_, err := e.commitGenerationContext(ctx, stepID, pending, workflowTaskPromptTriggerTaskDelivery)
	return err
}

func (e *Engine) commitGenerationContext(ctx context.Context, stepID string, pending pendingGenerationContext, trigger workflowTaskPromptTrigger) (session.CommitReceipt, error) {
	output := pending.replacement.Output
	meta, err := e.generationMetaContextProjection(ctx, trigger)
	if err != nil {
		return session.CommitReceipt{}, err
	}
	history := output.prepare(session.CompactionEngine(pending.replacement.Engine), meta, e.compactionRunningShellReminder())
	history.continuation = e.transcriptRuntimeState().SnapshotItems()
	receipt, err := e.steerWithCommitReceipt(stepID, steerHistoryReplacementIntent(
		session.HistoryReplacementPreparation,
		pending.replacement.Engine,
		compactionMode(pending.replacement.Mode),
		*pending.replacement.CompactionNumber,
		pending.replacement.LastCommittedAssistantFinalAnswer,
		history,
	))
	if receipt.Committed && !e.workflowPromptActive() && e.store.Meta().HeadlessActive != e.cfg.HeadlessMode {
		err = errors.Join(err, e.store.SetHeadlessActive(e.cfg.HeadlessMode))
	}
	if err != nil {
		return receipt, err
	}
	if !receipt.Committed {
		return receipt, errors.New("generation context was not committed")
	}
	return receipt, nil
}

func compactionOutputFromRecord(record session.CompactedOutput) (compactionOutput, error) {
	output := compactionOutput{}
	for _, item := range record.Summary {
		output.summary = append(output.summary, llmResponseItemFromSessionHistory(item))
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
