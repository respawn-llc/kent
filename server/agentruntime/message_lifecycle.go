package runtime

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"core/server/llm"
	"core/server/sessionstore"
	"core/server/tools"
	"core/shared/config"
	"core/shared/modelcontract"
	"core/shared/runtimeinput"
	"core/shared/textutil"
	"core/shared/toolspec"
)

type defaultMessageLifecycle struct {
	engine *Engine
	queue  *queuedUserMessageStore
}

func newDefaultMessageLifecycle(engine *Engine) *defaultMessageLifecycle {
	return &defaultMessageLifecycle{
		engine: engine,
		queue:  newQueuedUserMessageStore(),
	}
}

func (m *defaultMessageLifecycle) RestoreMessages() error {
	e := m.engine
	meta := e.store.Meta()
	reminderIssued := meta.CompactionSoonReminderIssued
	var matchErr error
	activeWindow, err := e.eventLog.ReadNewestSegmentBackward(compactionBoundaryMatcher(&matchErr))
	if err != nil {
		return err
	}
	if matchErr != nil {
		return matchErr
	}
	producingEvidence, err := restoredProducingStepEvidence(activeWindow.Records, e.SessionID())
	if err != nil {
		return err
	}
	var rollbackLocator rollbackCandidateLocatorTracker
	manualEligible := false
	type restoredToolGeneration struct {
		expected  map[string]struct{}
		completed map[string]struct{}
	}
	generationsByStep := make(map[string][]*restoredToolGeneration)
	for _, record := range activeWindow.Records {
		stepIDPointer := record.StepID()
		stepID, _ := textutil.OptionalExact(stepIDPointer)
		payload, err := record.Payload()
		if err != nil {
			return err
		}
		switch payload := payload.(type) {
		case session.ConfigurationUpdateRecord:
			provenance, err := transcriptProvenanceFromRecord(record)
			if err != nil {
				return err
			}
			e.transcriptRuntimeState().AppendConfigurationItem(stepIDPointer, llmResponseItemFromSessionHistory(payload.Item), &provenance)
		case session.MessageRecord:
			if _, fresh := e.generationContextSnapshot().(freshGenerationContext); fresh {
				e.setGenerationContext(preparedGenerationContext{})
			}
			msg, err := llmMessageFromSessionRecord(payload)
			if err != nil {
				return fmt.Errorf("restore session message record: %w", err)
			}
			for index := range msg.ReasoningItems {
				item := &msg.ReasoningItems[index]
				item.Attribution = producingEvidence.infer(item.Attribution, stepIDPointer, modelcontract.ProviderOperationPurposeGeneration)
			}
			if err := rollbackLocator.ObserveMessage(record.Seq(), msg); err != nil {
				return err
			}
			provenance, provenanceErr := transcriptProvenanceFromRecord(record)
			if provenanceErr != nil {
				return fmt.Errorf("restore session message provenance: %w", provenanceErr)
			}
			if err := e.transcriptRuntimeState().AppendMessage(stepIDPointer, msg, &provenance); err != nil {
				return fmt.Errorf("restore session message projection: %w", err)
			}
			if msg.Role == llm.RoleAssistant && len(msg.ToolCalls) > 0 {
				expected := make(map[string]struct{}, len(msg.ToolCalls))
				userShellGeneration := true
				for _, call := range msg.ToolCalls {
					if call.ID != "" {
						expected[call.ID] = struct{}{}
					}
					if !isUserInitiatedShellCall(call) {
						userShellGeneration = false
					}
				}
				if len(expected) > 0 && !userShellGeneration {
					generationsByStep[stepID] = append(generationsByStep[stepID], &restoredToolGeneration{
						expected:  expected,
						completed: make(map[string]struct{}, len(expected)),
					})
				}
			} else if msg.Role == llm.RoleAssistant &&
				((msg.Content != nil && strings.TrimSpace(*msg.Content) != "") || len(msg.ReasoningItems) > 0) {
				manualEligible = true
			}
			if isCompactionSoonReminderMessage(msg) {
				reminderIssued = true
			}
		case session.ToolCompletionRecord:
			provenance, provenanceErr := transcriptProvenanceFromRecord(record)
			if provenanceErr != nil {
				return fmt.Errorf("restore session tool completion provenance: %w", provenanceErr)
			}
			if err := e.transcriptRuntimeState().RestoreToolCompletionRecord(payload, &provenance); err != nil {
				return err
			}
			for _, generation := range generationsByStep[stepID] {
				if _, ok := generation.expected[payload.CallID]; ok {
					generation.completed[payload.CallID] = struct{}{}
				}
			}
		case session.LocalEntryRecord:
			entry, err := storedLocalEntryFromSessionRecord(payload)
			if err != nil {
				return fmt.Errorf("restore session local entry record: %w", err)
			}
			if entry.DiagnosticKey != nil {
				e.diagnosticDedupeStore().RestoreLocal(*entry.DiagnosticKey)
			}
			restored := *localEntryChatEntryForStep(entry, stepIDPointer)
			provenance, provenanceErr := transcriptProvenanceFromRecord(record)
			if provenanceErr != nil {
				return fmt.Errorf("restore session local entry provenance: %w", provenanceErr)
			}
			e.transcriptRuntimeState().AppendLocalEntryRecord(restored, entry.AfterToolCallID, &provenance)
		case session.ReviewerFeedbackRecord:
			provenance, provenanceErr := transcriptProvenanceFromRecord(record)
			if provenanceErr != nil {
				return fmt.Errorf("restore Reviewer feedback provenance: %w", provenanceErr)
			}
			e.transcriptRuntimeState().AppendLocalEntryRecord(
				reviewerFeedbackChatEntryFromSessionRecord(payload, stepID, &provenance), nil,
			)
		case session.ReviewerErrorRecord:
			provenance, provenanceErr := transcriptProvenanceFromRecord(record)
			if provenanceErr != nil {
				return fmt.Errorf("restore Reviewer error provenance: %w", provenanceErr)
			}
			e.transcriptRuntimeState().AppendLocalEntryRecord(
				reviewerErrorChatEntryFromSessionRecord(payload, stepID, &provenance), nil,
			)
		case session.CacheWarningRecord:
			provenance, provenanceErr := transcriptProvenanceFromRecord(record)
			if provenanceErr != nil {
				return fmt.Errorf("restore session cache warning provenance: %w", provenanceErr)
			}
			applyPersistedCacheWarningToTranscript(e.transcriptRuntimeState(), stepIDPointer, payload, e.cfg.CacheWarningMode, &provenance)
		case session.CacheRequestObservationRecord:
			// Cache requests are observational. The following response record
			// reconstructs the cache tracker state.
		case session.CacheResponseObservationRecord:
			if cache := e.modelRequests().RequestCache(); cache != nil {
				cache.RecordResponse(persistedCacheResponseObservedFromSessionRecord(payload))
			}
		case session.HistoryReplacementRecord:
			replacement, err := historyReplacementPayloadFromSessionRecord(payload)
			if err != nil {
				return fmt.Errorf("restore session history replacement record: %w", err)
			}
			for index := range replacement.Items {
				item := &replacement.Items[index]
				if item.Type == llm.ResponseItemTypeCompaction {
					item.Attribution = producingEvidence.infer(item.Attribution, stepIDPointer, modelcontract.ProviderOperationPurposeCompaction)
				}
			}
			e.resetLocalDiagnostics()
			manualEligible = false
			if _, err := e.installHistoryReplacement(record, replacement); err != nil {
				return err
			}
			rollbackLocator.ObserveCompaction(replacement.LatestRollbackCandidate)
			e.resetPromptCacheObservationBaselines()
			reminderIssued = false
		case session.GenerationContextRecord:
			if _, err := e.installGenerationContext(record, payload); err != nil {
				return err
			}
		}
		e.compactionRuntimeState().ApplyWorkflowPostCompletionActivity(
			workflowPostCompletionActivityForSessionRecord(payload),
		)
	}
	for _, generations := range generationsByStep {
		for _, generation := range generations {
			if len(generation.expected) > 0 && len(generation.completed) == len(generation.expected) {
				manualEligible = true
				break
			}
		}
	}
	restoredRollbackCandidate, err := rollbackLocator.Resolve(activeWindow.EndOffset)
	if err != nil {
		return fmt.Errorf("restore latest rollback candidate: %w", err)
	}
	if restoredRollbackCandidate != nil {
		e.transcriptRuntimeState().SetLatestRollbackCandidate(*restoredRollbackCandidate)
	}
	e.compactionRuntimeState().SetSoonReminderIssued(reminderIssued)
	e.compactionRuntimeState().SetManualCompactionEligible(manualEligible)
	if err := e.store.SetCompactionSoonReminderIssued(reminderIssued); err != nil {
		return err
	}
	return nil
}

func workflowPostCompletionActivityForSessionRecord(payload any) workflowPostCompletionActivity {
	switch record := payload.(type) {
	case session.MessageRecord:
		message := llm.Message{
			Role:            llm.Role(record.Role),
			SourcePath:      textutil.Pointer(record.SourcePath),
			WorktreeContext: session.CloneWorktreeContext(record.WorktreeContext),
		}
		if record.MessageType != nil {
			messageType := llm.MessageType(*record.MessageType)
			message.MessageType = &messageType
		}
		return workflowPostCompletionMessageActivity(message)
	case session.ToolCompletionRecord:
		return workflowPostCompletionDurableActivity
	case session.CacheRequestObservationRecord:
		return workflowPostCompletionNoActivity
	default:
		return workflowPostCompletionNoActivity
	}
}

func isUserInitiatedShellCall(call llm.ToolCall) bool {
	if call.Name != string(toolspec.ToolExecCommand) {
		return false
	}
	return tools.ParseShellToolCallUserInitiated(call.Input)
}

func isCompactionSoonReminderMessage(msg llm.Message) bool {
	return msg.Role == llm.RoleDeveloper &&
		msg.MessageType != nil &&
		*msg.MessageType == llm.MessageTypeCompactionSoonReminder &&
		msg.Content != nil &&
		strings.TrimSpace(*msg.Content) != ""
}

func queuedUserMessageText(message QueuedUserMessage) string {
	return strings.TrimSpace(message.ExecutionText())
}

type queuedUserMessageFlushGroup struct {
	message    llm.Message
	batch      []string
	queueItems []QueuedUserMessage
}

type preparedUserInjections struct {
	queue  *queuedUserMessageStore
	claim  *queuedUserMessageClaim
	items  []llm.ResponseItem
	groups []queuedUserMessageFlushGroup
}

func (m *defaultMessageLifecycle) PreparePendingUserInjections(selection userInjectionSelection) (*preparedUserInjections, error) {
	var claim *queuedUserMessageClaim
	switch selected := selection.(type) {
	case queuedTurnUserInjectionSelection:
		claim = m.queue.ClaimQueuedTurn()
	case steerUserInjectionSelection:
		claim = m.queue.ClaimSteersAndIDs(selected.queueItemIDs)
	default:
		return nil, fmt.Errorf("unsupported user injection selection %T", selection)
	}
	prepared := &preparedUserInjections{queue: m.queue, claim: claim}
	if claim == nil {
		return prepared, nil
	}
	for _, pending := range claim.items {
		id := mustQueueItemID(pending.message.ID)
		lane := runtimeinput.PendingWorkLaneQueue
		if pending.steerAdmission != nil {
			lane = runtimeinput.PendingWorkLaneSteer
		}
		publishing, _, err := m.engine.liveRun.beginQueueItemPublication(id, lane, claim)
		if err != nil {
			return nil, err
		}
		if publishing && m.engine.liveRun.finishQueueItemPublication(id) {
			m.engine.failStoppedLiveRunQueueItems(typedQueueItemIDSet(map[string]struct{}{pending.message.ID: {}}))
		}
	}
	groups := queuedUserMessageFlushGroups(claim.items)
	for _, group := range groups {
		prepared.items = append(prepared.items, llm.ItemsFromMessages([]llm.Message{group.message})...)
	}
	prepared.groups = groups
	return prepared, nil
}

func (m *defaultMessageLifecycle) ReleasePreparedUserInjections(prepared *preparedUserInjections) {
	m.engine.removeStoppedLiveRunQueueItems(func() []QueuedUserMessage {
		return m.queue.ReleaseClaim(prepared.claim)
	})
}

func (m *defaultMessageLifecycle) CommitPreparedUserInjections(ctx context.Context, stepID string, prepared *preparedUserInjections, thinking nativeThinkingProjection) (userInjectionCommitResult, error) {
	result := userInjectionCommitResult{}
	receipt, err := m.engine.steerWithCommitReceipt(stepID, steerPreparedModelInputIntent(ctx, prepared, thinking))
	result.receipt = receipt
	if !receipt.Committed {
		if fatal, failedWrite := resultGroupFatalFromError(err); failedWrite {
			return result, &resultGroupFatal{Committed: false, Cause: errors.Join(fatal.Cause, m.restoreFailedPreparedInput(prepared))}
		}
		return result, err
	}
	result.queueItemIDs = make(map[string]struct{})
	for _, group := range prepared.groups {
		for id := range queuedUserMessageIDSet(group.queueItems) {
			result.queueItemIDs[id] = struct{}{}
		}
		result.startedStep = result.startedStep || startsAgentStep(group.message)
		result.flushed++
	}
	m.queue.FinalizeClaimItems(prepared.claim, result.queueItemIDs)
	m.engine.completeQueuedUserMessages(result.queueItemIDs)
	m.engine.publishPendingWorkChanged()
	return result, err
}

func (m *defaultMessageLifecycle) restoreFailedPreparedInput(prepared *preparedUserInjections) error {
	ids := make(map[string]struct{})
	for _, group := range prepared.groups {
		for id := range queuedUserMessageIDSet(group.queueItems) {
			ids[id] = struct{}{}
		}
	}
	e := m.engine
	e.outputMutationMu.Lock()
	technical, stopped := m.queue.FailClaimItems(prepared.claim, ids)
	e.emitInterruptedHumanInputs(stopped)
	var restorationErr error
	for _, pending := range technical {
		item, err := pendingWorkMessage(pending)
		if err == nil {
			err = e.publishPendingWorkTechnicalRestoration(item)
		}
		restorationErr = errors.Join(restorationErr, err)
	}
	e.outputMutationMu.Unlock()
	e.completeQueuedUserMessages(ids)
	e.publishPendingWorkChanged()
	return restorationErr
}

func queuedUserMessageFlushGroups(messages []queuedUserMessage) []queuedUserMessageFlushGroup {
	groups := make([]queuedUserMessageFlushGroup, 0, len(messages))
	for _, pending := range messages {
		text := queuedUserMessageText(pending.message)
		queueItems := []QueuedUserMessage{pending.message}
		message := pending.message.Message
		groups = append(groups, queuedUserMessageFlushGroup{
			message:    message,
			batch:      []string{text},
			queueItems: queueItems,
		})
	}
	return groups
}

func startsAgentStep(message llm.Message) bool {
	return message.Role == llm.RoleUser && message.MessageType == nil ||
		message.Role == llm.RoleDeveloper &&
			message.MessageType != nil &&
			(*message.MessageType == llm.MessageTypeAgentSteer || *message.MessageType == llm.MessageTypeReviewerFeedback)
}

func (m *defaultMessageLifecycle) QueueUserMessage(input QueuedUserInput, association ...queuedUserMessageAssociation) (QueuedUserMessage, error) {
	if m == nil || m.queue == nil {
		return QueuedUserMessage{}, errors.New("queued user message lifecycle is required")
	}
	return m.queue.Queue(input, association...)
}

func (m *defaultMessageLifecycle) QueueUserMessageWithID(item QueuedUserMessage, association ...queuedUserMessageAssociation) (QueuedUserMessage, error) {
	if m == nil || m.queue == nil {
		return QueuedUserMessage{}, errors.New("queued user message lifecycle is required")
	}
	return m.queue.QueueItem(item, association...)
}

func (m *defaultMessageLifecycle) DrainPendingUserInjections() []QueuedUserMessage {
	if m == nil || m.queue == nil {
		return nil
	}
	pending := m.queue.Drain()
	out := make([]QueuedUserMessage, 0, len(pending))
	for _, item := range pending {
		out = append(out, item.message)
	}
	return out
}

func (m *defaultMessageLifecycle) DrainPendingUserInjectionsByID(ids map[string]struct{}) []QueuedUserMessage {
	if m == nil || m.queue == nil || len(ids) == 0 {
		return nil
	}
	pending := m.queue.DrainByID(ids)
	out := make([]QueuedUserMessage, 0, len(pending))
	for _, item := range pending {
		out = append(out, item.message)
	}
	return out
}

func (m *defaultMessageLifecycle) PendingUserMessages() []QueuedUserMessage {
	if m == nil || m.queue == nil {
		return nil
	}
	return m.queue.Snapshot()
}

func (m *defaultMessageLifecycle) PendingUserMessageEntries() []queuedUserMessage {
	if m == nil || m.queue == nil {
		return nil
	}
	return m.queue.EntrySnapshot()
}

func (m *defaultMessageLifecycle) DiscardQueuedUserMessage(queueItemID string) (queuedUserMessage, bool) {
	if m == nil || m.queue == nil {
		return queuedUserMessage{}, false
	}
	return m.queue.DiscardItem(queueItemID)
}

func (m *defaultMessageLifecycle) HasPendingUserInjections() bool {
	return m != nil && m.queue != nil && m.queue.HasPending()
}

func (m *defaultMessageLifecycle) HasPendingUserSteers() bool {
	return m != nil && m.queue != nil && m.queue.HasPendingSteers()
}

func newActiveMetaContextBuilder(store *session.Store, executionRoot, model, thinkingLevel, globalConfigDir string, skillPolicy config.SkillPolicy, now time.Time) metaContextBuilder {
	roots := activeMetaContextRootsForMeta(store.Meta(), executionRoot)
	builder := newMetaContextBuilder(roots.discoveryRoot, model, thinkingLevel, skillPolicy, now).
		withEnvironmentCWD(roots.environmentCWD).
		withGlobalConfigDir(globalConfigDir)
	builder.sessionShortID = store.Identity().ShortID
	return builder
}

type activeMetaContextRoots struct {
	discoveryRoot  string
	environmentCWD string
}

func activeMetaContextRootsForMeta(meta session.Meta, executionRoot string) activeMetaContextRoots {
	activeRoot := strings.TrimSpace(executionRoot)
	if activeRoot == "" {
		activeRoot = strings.TrimSpace(meta.WorkspaceRoot)
	}
	roots := activeMetaContextRoots{discoveryRoot: activeRoot, environmentCWD: activeRoot}
	state := session.CloneWorktreeReminderState(meta.WorktreeReminder)
	if state == nil {
		return roots
	}

	switch state.Mode {
	case session.WorktreeReminderModeEnter:
		if state.WorktreePath != "" {
			roots.discoveryRoot = state.WorktreePath
		}
	case session.WorktreeReminderModeExit:
		if state.WorkspaceRoot != "" {
			roots.discoveryRoot = state.WorkspaceRoot
		}
	}
	if state.EffectiveCwd != "" {
		roots.environmentCWD = state.EffectiveCwd
	} else {
		roots.environmentCWD = roots.discoveryRoot
	}
	return roots
}
