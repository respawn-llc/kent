package runtime

import (
	"context"
	"errors"

	"core/prompts"
	"core/server/llm"
	"core/server/session"
	"core/shared/textutil"
)

// A saved Workflow summary is not a prepared conversation. Its role-dependent
// context can only be constructed after the target runtime has been selected.
type generationContext interface {
	generationContext()
}

type freshGenerationContext struct{}
type preparedGenerationContext struct{}
type pendingWorkflowGeneration struct {
	summary session.WorkflowCompactionRecord
}

func (freshGenerationContext) generationContext()    {}
func (preparedGenerationContext) generationContext() {}
func (pendingWorkflowGeneration) generationContext() {}

type compactionOutput struct {
	engine               session.CompactionEngine
	summary              []llm.ResponseItem
	runningShells        []llm.Message
	preservedUserMessage *llm.Message
	futureAgentMessage   *llm.Message
}

type preparedCompactionHistory struct {
	items []llm.ResponseItem
}

func (output compactionOutput) estimateTokens() int {
	return estimateItemsTokens(output.summary) +
		estimateItemsTokens(llm.ItemsFromMessages(output.runningShells)) +
		estimateItemsTokens(llm.ItemsFromMessages(output.continuationMessages()))
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

func (output compactionOutput) prepare(meta metaContextProjection) preparedCompactionHistory {
	var items []llm.ResponseItem
	if output.engine == session.CompactionEngineRemote {
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

func (e *Engine) prepareWorkflowGeneration(ctx context.Context, stepID string, pending pendingWorkflowGeneration) error {
	output, err := compactionOutputFromRecord(pending.summary)
	if err != nil {
		return err
	}
	meta, err := e.compactionReinjectedMetaContextProjection(ctx, compactionModeWorkflowPostCompletion)
	if err != nil {
		return err
	}
	history := output.prepare(meta)
	history.items = append(history.items, e.transcriptRuntimeState().SnapshotItems()...)
	receipt, err := e.steerWithCommitReceipt(stepID, steerHistoryReplacementIntent(
		string(output.engine),
		compactionModeWorkflowPostCompletion,
		pending.summary.CompactionNumber,
		pending.summary.LastCommittedAssistantFinalAnswer,
		history,
	))
	if err != nil {
		return err
	}
	if !receipt.Committed {
		return errors.New("workflow generation context was not committed")
	}
	return nil
}

func compactionOutputFromRecord(record session.WorkflowCompactionRecord) (compactionOutput, error) {
	output := compactionOutput{engine: record.Engine}
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
	return output, nil
}

func (e *Engine) workflowCompactionRecord(output compactionOutput) (session.WorkflowCompactionRecord, error) {
	record := session.WorkflowCompactionRecord{
		Engine: output.engine, CompactionNumber: e.compactionRuntimeState().Count() + 1,
		CommittedEntryStart:               e.CommittedTranscriptEntryCount(),
		LastCommittedAssistantFinalAnswer: e.LastCommittedAssistantFinalAnswer(),
		LatestRollbackCandidate:           e.transcriptRuntimeState().LatestRollbackCandidate(),
	}
	for index, item := range llm.PrepareOpenAIInputItems(output.summary) {
		stored, err := sessionProviderHistoryItemFromLLM(index, item)
		if err != nil {
			return session.WorkflowCompactionRecord{}, err
		}
		record.Summary = append(record.Summary, stored)
	}
	for _, message := range output.runningShells {
		stored, err := sessionMessageRecordFromLLM(message)
		if err != nil {
			return session.WorkflowCompactionRecord{}, err
		}
		record.RunningShells = append(record.RunningShells, stored)
	}
	if output.preservedUserMessage != nil {
		stored, err := sessionMessageRecordFromLLM(*output.preservedUserMessage)
		if err != nil {
			return session.WorkflowCompactionRecord{}, err
		}
		record.PreservedUserMessage = &stored
	}
	return record, nil
}

func (e *Engine) installWorkflowCompaction(record session.WorkflowCompactionRecord) error {
	e.generationContext = pendingWorkflowGeneration{summary: record}
	e.transcriptRuntimeState().BeginGeneration(record.CommittedEntryStart)
	e.transcriptRuntimeState().SeedLastCommittedAssistantFinalAnswerIfAbsent(record.LastCommittedAssistantFinalAnswer)
	e.compactionRuntimeState().SetCount(record.CompactionNumber)
	mode := session.CompactionModeWorkflowPostCompletion
	return e.compactionRuntimeState().SetHistoryReplacementMode(&mode)
}

func (e *Engine) persistWorkflowCompaction(stepID string, record session.WorkflowCompactionRecord) (session.CommitReceipt, error) {
	_, receipt, err := e.eventLog.AppendWorkflowCompaction(textutil.OptionalExactString(stepID), record)
	if !receipt.Committed {
		return receipt, err
	}
	e.invalidateCompactedRuntime()
	installErr := e.installWorkflowCompaction(record)
	return receipt, errors.Join(err, installErr, e.finishCompactionCommit(stepID))
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
