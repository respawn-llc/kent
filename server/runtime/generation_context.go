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

type compactionOutput struct {
	summary              []llm.ResponseItem
	preservedUserMessage *llm.Message
	futureAgentMessage   *llm.Message
}

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

func prepareGenerationContext(engine session.CompactionEngine, meta metaContextProjection, runningShells []llm.Message) (session.GenerationContextRecord, error) {
	var before []llm.Message
	if engine == session.CompactionEngineRemote {
		before = append(before, llm.Message{
			Role: llm.RoleDeveloper, Content: textutil.Value(prompts.CompactionContinuationReminder),
		})
	}
	before = append(before, meta.StablePrefix...)
	after := append(runningShells, meta.Environment...)
	record := session.GenerationContextRecord{}
	for _, segment := range []struct {
		messages []llm.Message
		target   *[]session.MessageRecord
	}{{before, &record.BeforeSummary}, {after, &record.AfterSummary}} {
		for _, message := range segment.messages {
			stored, err := sessionMessageRecordFromLLM(message)
			if err != nil {
				return session.GenerationContextRecord{}, err
			}
			*segment.target = append(*segment.target, stored)
		}
	}
	return record, nil
}

// Live request operations prepare a saved generation once. Idle Session
// activation and inspection leave it pending.
func (e *Engine) prepareRequestGenerationContext(ctx context.Context, stepID string) error {
	pending, ok := e.generationContextSnapshot().(pendingGenerationContext)
	if !ok {
		return nil
	}
	if _, err := e.ensureLocked(); err != nil {
		return err
	}
	if e.workflowPromptActive() {
		var committedErr error
		trigger := workflowTaskPromptTriggerCompaction
		if pending.replacement.Mode == string(session.CompactionModeWorkflowPostCompletion) {
			trigger = workflowTaskPromptTriggerTaskDelivery
		}
		err := e.currentNodeExecutionSnapshot().delivery.apply(trigger, func(trigger workflowTaskPromptTrigger) error {
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
	meta, err := e.generationMetaContextProjection(ctx, trigger)
	if err != nil {
		return session.CommitReceipt{}, err
	}
	record, err := prepareGenerationContext(session.CompactionEngine(pending.replacement.Engine), meta, e.compactionRunningShellReminder())
	if err != nil {
		return session.CommitReceipt{}, err
	}
	receipt, err := e.steerWithCommitReceipt(stepID, steeringIntent{
		priority: steeringPriorityNormal,
		items:    []steeringItem{{generationContext: &record}},
	})
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

func (e *Engine) commitGenerationContextRaw(stepID string, record session.GenerationContextRecord) (session.CommitReceipt, error) {
	if _, pending := e.generationContextSnapshot().(pendingGenerationContext); !pending {
		return session.CommitReceipt{}, errors.New("generation context requires pending compaction output")
	}
	appended, receipt, err := e.eventLog.AppendRecord(textutil.OptionalExactString(stepID), record)
	if !receipt.Committed {
		return receipt, err
	}
	start := e.CommittedTranscriptEntryCount()
	entries, installErr := e.installGenerationContext(appended, record)
	if installErr != nil {
		return receipt, errors.Join(err, installErr)
	}
	return receipt, errors.Join(err, e.emitProjectedHistoryReplacementEntriesRaw(stepID, start, entries))
}

func generationContextMessages(record session.GenerationContextRecord) (before, after []llm.Message, err error) {
	for _, segment := range []struct {
		records []session.MessageRecord
		target  *[]llm.Message
	}{{record.BeforeSummary, &before}, {record.AfterSummary, &after}} {
		for _, stored := range segment.records {
			message, messageErr := llmMessageFromSessionRecord(stored)
			if messageErr != nil {
				return nil, nil, messageErr
			}
			*segment.target = append(*segment.target, message)
		}
	}
	return before, after, nil
}

func generationContextEntries(record session.EventRecord, context session.GenerationContextRecord) ([]ChatEntry, error) {
	before, after, err := generationContextMessages(context)
	if err != nil {
		return nil, err
	}
	var entries []ChatEntry
	for _, message := range append(before, after...) {
		entries = append(entries, VisibleChatEntriesFromMessage(message)...)
	}
	provenance, err := transcriptProvenanceFromRecord(record)
	if err != nil {
		return nil, err
	}
	for i := range entries {
		entries[i].StepID = record.StepID()
	}
	return assignHistoryReplacementEntryProvenance(entries, &provenance), nil
}

func (e *Engine) installGenerationContext(record session.EventRecord, context session.GenerationContextRecord) ([]ChatEntry, error) {
	pending, ok := e.generationContextSnapshot().(pendingGenerationContext)
	if !ok {
		return nil, errors.New("generation context has no pending compaction output")
	}
	before, after, err := generationContextMessages(context)
	if err != nil {
		return nil, err
	}
	entries, err := generationContextEntries(record, context)
	if err != nil {
		return nil, err
	}
	output := pending.replacement.Output
	items := llm.ItemsFromMessages(before)
	items = append(items, output.summary...)
	items = append(items, llm.ItemsFromMessages(after)...)
	items = append(items, llm.ItemsFromMessages(output.continuationMessages())...)
	e.transcriptRuntimeState().chatProjection().prepareGeneration(items, entries)
	e.setGenerationContext(preparedGenerationContext{})
	return entries, nil
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
