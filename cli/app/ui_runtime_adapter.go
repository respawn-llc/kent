package app

import (
	"strconv"
	"strings"

	"builder/cli/tui"
	"builder/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

type uiRuntimeAdapter struct {
	model *uiModel
}

type runtimeEventApplyResult struct {
	cmd               tea.Cmd
	transcriptMutated bool
	awaitsHydration   bool
}

func (a uiRuntimeAdapter) handleProjectedRuntimeEvent(evt clientui.Event) tea.Cmd {
	return a.applyProjectedRuntimeEvent(evt, true).cmd
}

func (a uiRuntimeAdapter) handleProjectedRuntimeEventsBatch(events []clientui.Event) tea.Cmd {
	return a.applyProjectedRuntimeEventsBatch(events).cmd
}

func (a uiRuntimeAdapter) applyProjectedRuntimeEventsBatch(events []clientui.Event) runtimeEventApplyResult {
	cmds := make([]tea.Cmd, 0, len(events)+1)
	transcriptMutated := false
	awaitsHydration := false
	for _, evt := range events {
		result := a.applyProjectedRuntimeEvent(evt, false)
		cmds = append(cmds, result.cmd)
		transcriptMutated = transcriptMutated || result.transcriptMutated
		awaitsHydration = awaitsHydration || result.awaitsHydration
	}
	batchedCmd := batchCmds(cmds...)
	if !transcriptMutated {
		return runtimeEventApplyResult{cmd: batchedCmd, awaitsHydration: awaitsHydration}
	}
	nativeCmd := a.model.syncNativeHistoryFromTranscript()
	return runtimeEventApplyResult{cmd: sequenceCmds(nativeCmd, batchedCmd), transcriptMutated: true, awaitsHydration: awaitsHydration}
}

func (a uiRuntimeAdapter) applyProjectedRuntimeEvent(evt clientui.Event, flushNativeHistory bool) runtimeEventApplyResult {
	m := a.model
	if merge := reduceDeferredCommittedTailMerge(newDeferredCommittedTailState(deferredCommittedTailSnapshotFromModel(m)), evt); merge.merged {
		evt = merge.event
		m.deferredCommittedTail = merge.remaining
		m.logDeferredCommittedTailMergeDiag(evt, merge)
	}
	if m.turnQueueHook != nil {
		m.turnQueueHook.OnProjectedRuntimeEvent(evt)
	}
	reduction := clientui.ReduceRuntimeEvent(
		a.runtimeRunState(),
		a.runtimeConversationState(),
		a.pendingInputState(),
		a.runtimeReasoningState(),
		m.activity == uiActivityRunning,
		evt,
	)
	transcriptSync := a.effectiveRuntimeTranscriptSync(evt, reduction.Transcript.Sync)
	m.logTranscriptEventDiag("transcript.diag.client.apply_event", evt, map[string]string{
		"path":                  "live_event",
		"recovery_cause":        string(evt.RecoveryCause),
		"sync_session_view":     strconv.FormatBool(transcriptSync.IsSet()),
		"sync_reason":           runtimeTranscriptSyncReasonLabel(transcriptSync),
		"record_prompt_history": strconv.FormatBool(reduction.PendingInput.PromptHistoryCommand != nil),
	})
	m.markActiveSubmitFlushed(evt)
	m.applyRuntimeEventStatus(evt)
	a.applyRuntimeEventReduction(reduction)
	a.reconcileInterruptFromRunState(evt)
	cmds := make([]tea.Cmd, 0, 4)
	transcriptMutated := false
	awaitsHydration := false
	if shouldAppendSyntheticOngoingEntry(m, reduction.Transcript.SyntheticOngoingEntry) {
		entry := transcriptEntryFromProjectedChatEntry(*reduction.Transcript.SyntheticOngoingEntry, true, false)
		m.forwardToView(appendTranscriptMsgFromEntry(entry))
	}
	if shouldInvalidateTransientTranscriptStateForSync(transcriptSync) {
		m.invalidateTransientTranscriptState()
	}
	if len(evt.TranscriptEntries) > 0 {
		if shouldDeliverCommittedRuntimeEventFromSuffix(m, evt) {
			cmds = append(cmds, m.requestRuntimeCommittedTranscriptSuffix(committedTranscriptSuffixRequestForEvent(m, evt)))
			if shouldClearAssistantStreamForCommittedAssistantEvent(evt) || skippedAssistantCommitMatchesActiveLiveStream(m, evt) {
				if stepID := strings.TrimSpace(evt.StepID); stepID != "" {
					m.lastCommittedAssistantStepID = stepID
				}
				m.sawAssistantDelta = false
				m.forwardToView(tui.ClearOngoingAssistantMsg{})
			}
		} else {
			cmd, mutated, needsHydration := a.applyProjectedTranscriptEntries(evt, flushNativeHistory)
			cmds = append(cmds, cmd)
			transcriptMutated = transcriptMutated || mutated
			awaitsHydration = awaitsHydration || needsHydration
			if shouldClearAssistantStreamForCommittedAssistantEvent(evt) && (mutated || skippedAssistantCommitMatchesActiveLiveStream(m, evt)) {
				if stepID := strings.TrimSpace(evt.StepID); stepID != "" {
					m.lastCommittedAssistantStepID = stepID
				}
				m.sawAssistantDelta = false
				m.forwardToView(tui.ClearOngoingAssistantMsg{})
			}
		}
	}
	for _, streamCommand := range reduction.Transcript.AssistantStream {
		switch streamCommand.Kind {
		case clientui.RuntimeAssistantStreamAppend:
			delta := streamCommand.Delta
			if shouldIgnoreStaleAssistantDelta(m, evt, delta) {
				continue
			}
			if isNoopFinalText(delta) {
				continue
			}
			m.sawAssistantDelta = true
			m.forwardToView(tui.StreamAssistantMsg{Delta: delta})
		case clientui.RuntimeAssistantStreamClear:
			if stepID := strings.TrimSpace(streamCommand.StepID); stepID != "" {
				m.lastCommittedAssistantStepID = stepID
			}
			m.sawAssistantDelta = false
			m.forwardToView(tui.ClearOngoingAssistantMsg{})
		}
	}
	for _, streamCommand := range reduction.Reasoning.Stream {
		switch streamCommand.Kind {
		case clientui.RuntimeReasoningStreamUpsert:
			if streamCommand.Delta == nil {
				continue
			}
			m.reasoningLiveDirty = true
			m.forwardToView(tui.UpsertStreamingReasoningMsg{Key: streamCommand.Delta.Key, Role: streamCommand.Delta.Role, Text: streamCommand.Delta.Text})
		case clientui.RuntimeReasoningStreamClear:
			m.reasoningLiveDirty = false
			m.forwardToView(tui.ClearStreamingReasoningMsg{})
		}
	}
	if reduction.Notices.BackgroundNotice != nil {
		kind := uiStatusNoticeSuccess
		if reduction.Notices.BackgroundNotice.Kind == clientui.BackgroundNoticeError {
			kind = uiStatusNoticeError
		}
		cmds = append(cmds, m.setTransientStatusWithKind(reduction.Notices.BackgroundNotice.Message, kind))
	}
	if reduction.PendingInput.PromptHistoryCommand != nil && strings.TrimSpace(reduction.PendingInput.PromptHistoryCommand.Text) != "" {
		cmds = append(cmds, m.recordPromptHistory(reduction.PendingInput.PromptHistoryCommand.Text))
	}
	if transcriptSync.IsSet() {
		cmds = append(cmds, a.syncConversationFromRuntimeTranscriptCommand(transcriptSync))
		awaitsHydration = awaitsHydration || shouldPauseRuntimeEventsForHydration(m)
	} else if shouldRefreshDeferredCommittedTailOnRunEnd(m, evt) {
		cmds = append(cmds, m.requestRuntimeCommittedConversationSync())
	}
	return runtimeEventApplyResult{cmd: batchCmds(cmds...), transcriptMutated: transcriptMutated, awaitsHydration: awaitsHydration}
}

func shouldInvalidateTransientTranscriptStateForSync(sync clientui.RuntimeTranscriptSyncCommand) bool {
	switch sync.Reason {
	case clientui.RuntimeTranscriptSyncRecovery, clientui.RuntimeTranscriptSyncStreamGap, clientui.RuntimeTranscriptSyncCommittedAdvance:
		return true
	default:
		return false
	}
}

func runtimeTranscriptSyncReasonLabel(sync clientui.RuntimeTranscriptSyncCommand) string {
	if !sync.IsSet() {
		return ""
	}
	return string(sync.Reason)
}

func (a uiRuntimeAdapter) syncConversationFromRuntimeTranscriptCommand(sync clientui.RuntimeTranscriptSyncCommand) tea.Cmd {
	switch sync.Reason {
	case clientui.RuntimeTranscriptSyncRecovery, clientui.RuntimeTranscriptSyncStreamGap:
		return a.model.requestRuntimeTranscriptSyncForContinuityLoss(sync.RecoveryCause)
	case clientui.RuntimeTranscriptSyncCommittedAdvance, clientui.RuntimeTranscriptSyncOngoingErrorUpdated:
		return a.syncConversationFromEngine()
	default:
		return nil
	}
}

func (a uiRuntimeAdapter) syncConversationFromEngine() tea.Cmd {
	m := a.model
	if !m.hasRuntimeClient() {
		return nil
	}
	return m.requestRuntimeCommittedConversationSync()
}

func waitAskEvent(ch <-chan askEvent) tea.Cmd {
	return func() tea.Msg {
		evt, ok := <-ch
		if !ok {
			return nil
		}
		return askEventMsg{event: evt}
	}
}

func waitRuntimeConnectionStateChange(ch <-chan runtimeConnectionStateChangedMsg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func waitRuntimeLeaseRecoveryWarning(ch <-chan runtimeLeaseRecoveryWarningMsg) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return msg
	}
}

func (m *uiModel) handleRuntimeEvent(evt clientui.Event) {
	_ = m.runtimeAdapter().handleProjectedRuntimeEvent(evt)
}

func (m *uiModel) syncConversationFromEngine() {
	_ = m.runtimeAdapter().syncConversationFromEngine()
}
