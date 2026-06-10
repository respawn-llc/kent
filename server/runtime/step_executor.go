package runtime

import (
	"context"
	"strings"

	"builder/server/llm"
	"builder/server/tools"
	"builder/server/workflowruntime"
	"builder/shared/toolspec"
)

type defaultStepExecutor struct {
	engine   *Engine
	phase    phaseProtocolEnforcer
	reviewer reviewerPipeline
	messages messageLifecycle
	tools    toolExecutor
}

func (s *defaultStepExecutor) RunStepLoopWithOptions(ctx context.Context, stepID string, options stepLoopOptions) (stepLoopResult, error) {
	e := s.engine
	executedToolCall := false
	patchEditsApplied := false
	deferredFinal := llm.Message{}
	deferredFinalCommittedStart := -1
	hasDeferredFinal := false
	for {
		if err := s.prepareModelTurn(ctx, stepID); err != nil {
			return stepLoopResult{}, err
		}

		requestPlan, err := e.buildRequestPlan(ctx, stepID, true)
		if err != nil {
			return stepLoopResult{}, err
		}
		req := requestPlan.Request

		resp, err := e.generateWithRetry(
			ctx,
			stepID,
			req,
			func(delta string) {
				e.transcriptPersistence().AppendOngoingDelta(delta)
				e.emit(Event{Kind: EventAssistantDelta, StepID: stepID, AssistantDelta: delta})
			},
			func(delta llm.ReasoningSummaryDelta) {
				e.emit(Event{Kind: EventReasoningDelta, StepID: stepID, ReasoningDelta: &delta})
			},
			func() {
				e.clearStreamingAssistantState(stepID)
			},
		)
		if err != nil {
			return stepLoopResult{}, err
		}
		if err := e.recordLastUsage(resp.Usage); err != nil {
			return stepLoopResult{}, err
		}

		localToolCalls := append([]llm.ToolCall(nil), resp.ToolCalls...)
		hostedToolExecutions := hostedToolExecutionsFromOutputItems(resp.OutputItems, tools.DefinitionsFor(e.cfg.EnabledTools))
		if len(localToolCalls) > 0 || len(hostedToolExecutions) > 0 {
			executedToolCall = true
		}

		phaseTurn := s.phase.Apply(ctx, resp, resp.Assistant, localToolCalls, hostedToolExecutions)
		assistantMsg := phaseTurn.Assistant
		localToolCalls = phaseTurn.LocalToolCalls
		hostedToolExecutions = phaseTurn.HostedToolExecutions
		noopFinalAnswer := isNoopFinalAnswer(assistantMsg)
		assistantCommittedStart := -1
		if noopFinalAnswer {
			e.clearStreamingAssistantState(stepID)
		}

		if preflightErr := workflowPreflightError(e.workflowRunActive(), localToolCalls, hostedToolExecutions); preflightErr != nil {
			terminal, err := s.appendWorkflowInvalidCompletionNudge(ctx, stepID, preflightErr)
			if err != nil {
				return stepLoopResult{}, err
			}
			if terminal {
				return stepLoopResult{Message: assistantMsg, ExecutedToolCall: executedToolCall}, nil
			}
			continue
		}

		finalAnswerWithToolCalls := assistantMsg.Phase == llm.MessagePhaseFinal &&
			strings.TrimSpace(assistantMsg.Content) != "" &&
			(len(localToolCalls) > 0 || len(hostedToolExecutions) > 0)
		if finalAnswerWithToolCalls {
			applied, terminal, err := s.materializeFinalAnswerToolCalls(ctx, stepID, localToolCalls, hostedToolExecutions)
			if err != nil {
				return stepLoopResult{}, err
			}
			patchEditsApplied = patchEditsApplied || applied
			if terminal {
				return stepLoopResult{Message: assistantMsg, ExecutedToolCall: true}, nil
			}
			assistantMsg.ToolCalls = nil
			localToolCalls = nil
			hostedToolExecutions = nil
			e.emitCommittedTranscriptAdvanced(stepID)
		}

		if !noopFinalAnswer {
			e.emit(Event{
				Kind:   EventModelResponse,
				StepID: stepID,
				ModelResponse: &ModelResponseTrace{
					AssistantPhase:   assistantMsg.Phase,
					AssistantChars:   len(assistantMsg.Content),
					ToolCallsCount:   len(resp.ToolCalls),
					OutputItemsCount: len(resp.OutputItems),
					OutputItemTypes:  summarizeOutputItemTypes(resp.OutputItems),
				},
			})
		}
		if err := e.appendAssistantMessage(stepID, assistantMsg); err != nil {
			return stepLoopResult{}, err
		}
		if !noopFinalAnswer {
			executableCallIDs := make(map[string]struct{}, len(localToolCalls))
			for _, call := range localToolCalls {
				if callID := strings.TrimSpace(call.ID); callID != "" {
					executableCallIDs[callID] = struct{}{}
				}
			}
			toolCallStarts := map[string]int(nil)
			assistantCommittedStart, toolCallStarts = committedStartsForPersistedAssistantMessage(e, assistantMsg, executableCallIDs)
			e.rememberPendingToolCallStarts(toolCallStarts)
			if liveAssistant, ok := liveCommittedAssistantEventMessage(assistantMsg); ok && options.EmitAssistantEvent {
				e.emit(Event{
					Kind:                       EventAssistantMessage,
					StepID:                     stepID,
					Message:                    liveAssistant,
					CommittedTranscriptChanged: true,
					CommittedEntryStart:        assistantCommittedStart,
					CommittedEntryStartSet:     assistantCommittedStart >= 0,
				})
			}
			if err := e.appendReasoningEntries(stepID, resp.Reasoning); err != nil {
				return stepLoopResult{}, err
			}
			if phaseTurn.MissingAssistantPhase {
				if err := e.appendMessage(stepID, llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeErrorFeedback, Content: missingAssistantPhaseWarning}); err != nil {
					return stepLoopResult{}, err
				}
			}
		}

		for _, hosted := range hostedToolExecutions {
			if err := e.persistToolCompletion(stepID, hosted.Result); err != nil {
				return stepLoopResult{}, err
			}
			msg := llm.Message{Role: llm.RoleTool, Content: string(hosted.Result.Output), ToolCallID: hosted.Result.CallID, Name: string(hosted.Result.Name)}
			if err := e.appendMessage(stepID, msg); err != nil {
				return stepLoopResult{}, err
			}
		}

		if len(localToolCalls) == 0 && len(hostedToolExecutions) == 0 {
			handled, terminal, err := s.handleWorkflowAssistantWithoutTools(ctx, stepID, assistantMsg)
			if err != nil {
				return stepLoopResult{}, err
			}
			if terminal {
				return stepLoopResult{Message: assistantMsg, ExecutedToolCall: executedToolCall}, nil
			}
			if handled {
				continue
			}
		}

		if len(localToolCalls) == 0 {
			if phaseTurn.MissingAssistantPhase {
				if len(hostedToolExecutions) > 0 {
					e.emitCommittedTranscriptAdvanced(stepID)
				}
				if _, err := s.messages.FlushPendingUserInjections(stepID); err != nil {
					return stepLoopResult{}, err
				}
				continue
			}
			if phaseTurn.EnforcePhaseProtocol && assistantMsg.Phase != llm.MessagePhaseFinal {
				if err := e.appendMessage(stepID, llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeErrorFeedback, Content: commentaryWithoutToolCallsWarning}); err != nil {
					return stepLoopResult{}, err
				}
				if _, err := s.messages.FlushPendingUserInjections(stepID); err != nil {
					return stepLoopResult{}, err
				}
				continue
			}
			if phaseTurn.EnforcePhaseProtocol && assistantMsg.Phase == llm.MessagePhaseFinal && strings.TrimSpace(assistantMsg.Content) == "" && !noopFinalAnswer {
				if err := e.appendMessage(stepID, llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeErrorFeedback, Content: finalWithoutContentWarning}); err != nil {
					return stepLoopResult{}, err
				}
				if _, err := s.messages.FlushPendingUserInjections(stepID); err != nil {
					return stepLoopResult{}, err
				}
				continue
			}

			flushed, err := s.messages.FlushPendingUserInjections(stepID)
			if err != nil {
				return stepLoopResult{}, err
			}
			if flushed > 0 {
				if assistantMsg.Phase == llm.MessagePhaseFinal && strings.TrimSpace(assistantMsg.Content) != "" && !noopFinalAnswer {
					deferredFinal = assistantMsg
					deferredFinalCommittedStart = assistantCommittedStart
					hasDeferredFinal = true
				}
				continue
			}
			if len(hostedToolExecutions) > 0 {
				e.emitCommittedTranscriptAdvanced(stepID)
				continue
			}

			resolved := assistantMsg
			resolvedNoopFinalAnswer := noopFinalAnswer
			resolvedCommittedStart := assistantCommittedStart
			resolvedCommittedStartSet := assistantCommittedStart >= 0
			var reviewerCompletion *ReviewerStatus
			if hasDeferredFinal {
				resolved = deferredFinal
				resolvedNoopFinalAnswer = isNoopFinalAnswer(resolved)
				resolvedCommittedStart = deferredFinalCommittedStart
				resolvedCommittedStartSet = deferredFinalCommittedStart >= 0
				hasDeferredFinal = false
				deferredFinalCommittedStart = -1
			}
			if resolvedNoopFinalAnswer {
				if e.goalActive() {
					if err := e.appendMessage(stepID, llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeErrorFeedback, Content: goalNoopFinalWarning}); err != nil {
						return stepLoopResult{}, err
					}
					continue
				}
				return stepLoopResult{Message: resolved, ExecutedToolCall: executedToolCall, NoopFinalAnswer: true, AssistantCommittedStart: resolvedCommittedStart, AssistantCommittedStartSet: resolvedCommittedStartSet}, nil
			}

			effectiveReviewerFrequency := options.ReviewerFrequency
			effectiveReviewerClient := options.ReviewerClient
			if options.RefreshReviewerConfigOnResolve {
				effectiveReviewerFrequency, effectiveReviewerClient = e.reviewerTurnConfigSnapshot()
			}
			assistantEventEmitted := false
			if s.reviewer.ShouldRunTurn(effectiveReviewerFrequency, effectiveReviewerClient, patchEditsApplied) {
				if options.EmitAssistantEvent {
					// The answer is already committed before supervisor entries are appended.
					// Publish it first so live clients never see supervisor entries as a gap
					// after an unannounced committed assistant message.
					e.emit(Event{Kind: EventAssistantMessage, StepID: stepID, Message: resolved, CommittedTranscriptChanged: true, CommittedEntryStart: resolvedCommittedStart, CommittedEntryStartSet: resolvedCommittedStartSet})
					assistantEventEmitted = true
				}
				preReviewMessage := resolved
				reviewed, err := s.reviewer.RunFollowUp(ctx, stepID, resolved, resolvedCommittedStart, resolvedCommittedStartSet, effectiveReviewerClient)
				if err == nil {
					resolved = reviewed.Message
					reviewerCompletion = reviewed.Completion
					resolvedCommittedStart = reviewed.AssistantCommittedStart
					resolvedCommittedStartSet = reviewed.AssistantCommittedStartSet
				}
				assistantEventEmitted = assistantEventEmitted && sameVisibleAssistantMessage(preReviewMessage, resolved)
			}
			if options.EmitAssistantEvent && !assistantEventEmitted {
				e.emit(Event{Kind: EventAssistantMessage, StepID: stepID, Message: resolved, CommittedTranscriptChanged: true, CommittedEntryStart: resolvedCommittedStart, CommittedEntryStartSet: resolvedCommittedStartSet})
			}
			if reviewerCompletion != nil {
				if err := e.appendPersistedLocalEntry(stepID, "reviewer_status", reviewerStatusText(*reviewerCompletion, nil)); err != nil {
					return stepLoopResult{}, err
				}
				e.emit(Event{Kind: EventReviewerCompleted, StepID: stepID, Reviewer: reviewerCompletion})
			}
			return stepLoopResult{Message: resolved, ExecutedToolCall: executedToolCall, AssistantCommittedStart: resolvedCommittedStart, AssistantCommittedStartSet: resolvedCommittedStartSet}, nil
		}

		applied, terminal, err := s.executeLocalToolCallsAndAppendResults(ctx, stepID, localToolCalls)
		if err != nil {
			return stepLoopResult{}, err
		}
		patchEditsApplied = patchEditsApplied || applied
		if terminal {
			return stepLoopResult{Message: assistantMsg, ExecutedToolCall: true}, nil
		}
		if _, err := s.messages.FlushPendingUserInjections(stepID); err != nil {
			return stepLoopResult{}, err
		}
	}
}

func (s *defaultStepExecutor) materializeFinalAnswerToolCalls(ctx context.Context, stepID string, localToolCalls []llm.ToolCall, hostedToolExecutions []hostedToolExecution) (bool, bool, error) {
	e := s.engine
	toolCallMessage := llm.Message{
		Role:      llm.RoleAssistant,
		Phase:     llm.MessagePhaseCommentary,
		ToolCalls: append([]llm.ToolCall(nil), localToolCalls...),
	}
	for _, hosted := range hostedToolExecutions {
		toolCallMessage.ToolCalls = append(toolCallMessage.ToolCalls, hosted.Call)
	}
	if err := e.appendAssistantMessage(stepID, toolCallMessage); err != nil {
		return false, false, err
	}

	executableCallIDs := make(map[string]struct{}, len(localToolCalls))
	for _, call := range localToolCalls {
		if callID := strings.TrimSpace(call.ID); callID != "" {
			executableCallIDs[callID] = struct{}{}
		}
	}
	_, toolCallStarts := committedStartsForPersistedAssistantMessage(e, toolCallMessage, executableCallIDs)
	e.rememberPendingToolCallStarts(toolCallStarts)

	patchEditsApplied, terminal, err := s.executeLocalToolCallsAndAppendResults(ctx, stepID, localToolCalls)
	if err != nil {
		return false, false, err
	}
	if err := s.appendHostedToolExecutionResults(stepID, hostedToolExecutions); err != nil {
		return false, false, err
	}
	return patchEditsApplied, terminal, nil
}

func (s *defaultStepExecutor) executeLocalToolCallsAndAppendResults(ctx context.Context, stepID string, localToolCalls []llm.ToolCall) (bool, bool, error) {
	if len(localToolCalls) == 0 {
		return false, false, nil
	}
	e := s.engine
	results, err := s.tools.ExecuteToolCalls(ctx, stepID, localToolCalls)
	if err != nil {
		return false, false, err
	}
	patchEditsApplied := false
	terminal := hasWorkflowTerminalResult(results)
	customToolCalls := customToolCallIDs(localToolCalls)
	for _, result := range results {
		if !result.IsError && (result.Name == toolspec.ToolPatch || result.Name == toolspec.ToolEdit) {
			patchEditsApplied = true
		}
		msg := llm.Message{Role: llm.RoleTool, Content: string(result.Output), ToolCallID: result.CallID, Name: string(result.Name)}
		msg.MessageType = llm.ToolOutputMessageType(customToolCalls[result.CallID])
		if err := e.appendMessage(stepID, msg); err != nil {
			return false, false, err
		}
	}
	return patchEditsApplied, terminal, nil
}

func (s *defaultStepExecutor) appendHostedToolExecutionResults(stepID string, hostedToolExecutions []hostedToolExecution) error {
	e := s.engine
	for _, hosted := range hostedToolExecutions {
		if err := e.persistToolCompletion(stepID, hosted.Result); err != nil {
			return err
		}
		msg := llm.Message{Role: llm.RoleTool, Content: string(hosted.Result.Output), ToolCallID: hosted.Result.CallID, Name: string(hosted.Result.Name)}
		if err := e.appendMessage(stepID, msg); err != nil {
			return err
		}
	}
	return nil
}

func (s *defaultStepExecutor) handleWorkflowAssistantWithoutTools(ctx context.Context, stepID string, assistantMsg llm.Message) (bool, bool, error) {
	e := s.engine
	if !e.workflowRunActive() || e.cfg.WorkflowRun.Controller == nil {
		return false, false, nil
	}
	mode, err := e.workflowCompletionMode(ctx)
	if err != nil {
		return false, false, err
	}
	content := strings.TrimSpace(assistantMsg.Content)
	if mode == workflowruntime.CompletionModeStructuredOutput && assistantMsg.Phase == llm.MessagePhaseFinal {
		parsed, parseErr := workflowruntime.DecodeCompletion([]byte(content), e.cfg.WorkflowRun.Contract)
		if parseErr != nil {
			terminal, nudgeErr := s.appendWorkflowInvalidCompletionNudge(ctx, stepID, parseErr)
			return true, terminal, nudgeErr
		}
		_, completeErr := e.cfg.WorkflowRun.Controller.CompleteWorkflowRun(ctx, workflowruntime.CompletionRequest{
			RunID:              e.cfg.WorkflowRun.Contract.RunID,
			ExpectedGeneration: e.cfg.WorkflowRun.Contract.ExpectedGeneration,
			RequireGeneration:  e.cfg.WorkflowRun.Contract.RequireGeneration,
			TransitionID:       parsed.TransitionID,
			OutputValues:       parsed.OutputValues,
			Commentary:         parsed.Commentary,
		})
		if completeErr != nil {
			terminal, nudgeErr := s.appendWorkflowInvalidCompletionNudge(ctx, stepID, completeErr)
			return true, terminal, nudgeErr
		}
		return true, true, nil
	}
	if mode == workflowruntime.CompletionModeTool && assistantMsg.Phase == llm.MessagePhaseFinal {
		record, recordErr := e.recordWorkflowProtocolViolation(ctx, workflowruntime.ViolationKindFinalAnswer, content)
		if recordErr != nil {
			return true, false, recordErr
		}
		if record.Interrupted {
			return true, true, nil
		}
		if err := e.appendMessage(stepID, llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeErrorFeedback, Content: workflowFinalAnswerNudge}); err != nil {
			return true, false, err
		}
		return true, false, nil
	}
	return false, false, nil
}

func (s *defaultStepExecutor) appendWorkflowInvalidCompletionNudge(ctx context.Context, stepID string, err error) (bool, error) {
	e := s.engine
	record, recordErr := e.recordWorkflowProtocolViolation(ctx, workflowruntime.ViolationKindInvalidCompletion, err.Error())
	if recordErr != nil {
		return false, recordErr
	}
	if record.Interrupted {
		return true, nil
	}
	content := workflowInvalidNudge
	if strings.TrimSpace(err.Error()) != "" {
		content += "\n\n" + err.Error()
	}
	return false, e.appendMessage(stepID, llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeErrorFeedback, Content: content})
}

func customToolCallIDs(calls []llm.ToolCall) map[string]bool {
	if len(calls) == 0 {
		return nil
	}
	out := make(map[string]bool, len(calls))
	for _, call := range calls {
		if call.Custom && strings.TrimSpace(call.ID) != "" {
			out[call.ID] = true
		}
	}
	return out
}

func (s *defaultStepExecutor) prepareModelTurn(ctx context.Context, stepID string) error {
	e := s.engine
	compactionCountBeforeReminder := e.compactionCountSnapshot()
	handoffRequestPending := e.pendingHandoffRequestSnapshot() != nil
	if !handoffRequestPending {
		if err := e.materializePendingWorktreeReminder(stepID); err != nil {
			return err
		}
	}
	handoffCompacted, err := e.applyPendingHandoffIfNeeded(ctx, stepID)
	if err != nil {
		return err
	}
	if err := e.requireAskQuestionForActiveGoal(); err != nil {
		return err
	}
	if handoffCompacted {
		if err := e.materializePendingWorktreeReminder(stepID); err != nil {
			return err
		}
		return e.maybeAppendCompactionSoonReminder(ctx, stepID)
	}
	if handoffRequestPending {
		if err := e.materializePendingWorktreeReminder(stepID); err != nil {
			return err
		}
	}
	if err := e.autoCompactIfNeeded(ctx, stepID, compactionModeAuto); err != nil {
		return err
	}
	if err := e.materializePendingWorktreeReminderAfterCompaction(stepID, compactionCountBeforeReminder); err != nil {
		return err
	}
	return e.maybeAppendCompactionSoonReminder(ctx, stepID)
}

func liveCommittedAssistantEventMessage(msg llm.Message) (llm.Message, bool) {
	if msg.Phase != llm.MessagePhaseCommentary {
		return llm.Message{}, false
	}
	if strings.TrimSpace(msg.Content) == "" {
		return llm.Message{}, false
	}
	return llm.Message{
		Role:    llm.RoleAssistant,
		Content: msg.Content,
		Phase:   msg.Phase,
	}, true
}

func sameVisibleAssistantMessage(a, b llm.Message) bool {
	aEntries := VisibleChatEntriesFromMessage(a)
	bEntries := VisibleChatEntriesFromMessage(b)
	if len(aEntries) != len(bEntries) {
		return false
	}
	for idx := range aEntries {
		if !sameVisibleChatEntryContent(aEntries[idx], bEntries[idx]) {
			return false
		}
	}
	return true
}

func sameVisibleChatEntryContent(a, b ChatEntry) bool {
	return a.Visibility == b.Visibility &&
		a.Role == b.Role &&
		a.Text == b.Text &&
		a.OngoingText == b.OngoingText &&
		a.Phase == b.Phase &&
		strings.TrimSpace(a.ToolCallID) == strings.TrimSpace(b.ToolCallID)
}

func committedStartsForPersistedAssistantMessage(e *Engine, msg llm.Message, executableCallIDs map[string]struct{}) (int, map[string]int) {
	if e == nil {
		return -1, nil
	}
	persisted := normalizeMessageForTranscript(msg, e.transcriptWorkingDir())
	entries := VisibleChatEntriesFromMessage(persisted)
	if len(entries) == 0 {
		return -1, nil
	}
	start := e.CommittedTranscriptEntryCount() - len(entries)
	if start < 0 {
		return -1, nil
	}
	toolCallStarts := make(map[string]int)
	for idx, entry := range entries {
		if strings.TrimSpace(entry.Role) != "tool_call" {
			continue
		}
		callID := strings.TrimSpace(entry.ToolCallID)
		if callID == "" {
			continue
		}
		if _, ok := executableCallIDs[callID]; !ok {
			continue
		}
		toolCallStarts[callID] = start + idx
	}
	return start, toolCallStarts
}
