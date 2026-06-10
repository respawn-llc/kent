package app

import (
	"strconv"
	"strings"

	"builder/cli/app/internal/runtimestate"
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
	reduction := runtimestate.ReduceRuntimeEvent(
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
		"sync_session_view":     strconv.FormatBool(transcriptSync.Reason != runtimestate.RuntimeTranscriptSyncNone),
		"sync_reason":           runtimeTranscriptSyncReasonLabel(transcriptSync),
		"record_prompt_history": strconv.FormatBool(reduction.PendingInput.PromptHistoryCommand != nil),
	})
	m.markActiveSubmitFlushed(evt)
	m.applyRuntimeEventStatus(evt)
	if !m.processList.open {
		m.applyBackgroundProcessEventToCache(evt.Background)
	}
	cmds := make([]tea.Cmd, 0, 5)
	cmds = append(cmds, a.applyRuntimeEventReduction(reduction))
	cmds = append(cmds, a.reconcileInterruptFromRunState(evt))
	transcriptMutated := false
	awaitsHydration := false
	evt = m.suppressLocalEntryEchoesInEvent(evt)
	if len(evt.TranscriptEntries) > 0 {
		if shouldDeliverCommittedRuntimeEventFromSuffix(m, evt) {
			m.observeNativeStreamingAssistantCommitCandidate(evt)
			cmds = append(cmds, m.requestRuntimeCommittedTranscriptSuffix(committedTranscriptSuffixRequestForEvent(m, evt)))
			if shouldClearAssistantStreamForCommittedAssistantEvent(evt) || skippedAssistantCommitMatchesActiveLiveStream(m, evt) {
				if stepID := strings.TrimSpace(evt.StepID); stepID != "" {
					m.lastCommittedAssistantStepID = stepID
				}
				m.sawAssistantDelta = false
				m.forwardToView(tui.ClearOngoingAssistantMsg{})
			}
		} else {
			m.observeNativeStreamingAssistantCommitCandidate(evt)
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
		case runtimestate.RuntimeAssistantStreamAppend:
			delta := streamCommand.Delta
			if shouldIgnoreStaleAssistantDelta(m, evt, delta) {
				continue
			}
			if isNoopFinalText(delta) {
				continue
			}
			if stepID := strings.TrimSpace(streamCommand.StepID); stepID != "" {
				m.nativeStreamingStepID = stepID
				m.nativeStreamingCommitRangeSet = false
			}
			m.sawAssistantDelta = true
			m.forwardToView(tui.StreamAssistantMsg{Delta: delta})
		case runtimestate.RuntimeAssistantStreamClear:
			if stepID := strings.TrimSpace(streamCommand.StepID); stepID != "" {
				m.lastCommittedAssistantStepID = stepID
			}
			m.sawAssistantDelta = false
			m.forwardToView(tui.ClearOngoingAssistantMsg{})
			cmds = append(cmds, m.releaseDeferredRuntimeSyncs())
		}
	}
	for _, streamCommand := range reduction.Reasoning.Stream {
		switch streamCommand.Kind {
		case runtimestate.RuntimeReasoningStreamUpsert:
			if streamCommand.Delta == nil {
				continue
			}
			m.reasoningLiveDirty = true
			m.forwardToView(tui.UpsertStreamingReasoningMsg{Key: streamCommand.Delta.Key, Role: streamCommand.Delta.Role, Text: streamCommand.Delta.Text})
		case runtimestate.RuntimeReasoningStreamClear:
			m.reasoningLiveDirty = false
			m.forwardToView(tui.ClearStreamingReasoningMsg{})
			cmds = append(cmds, m.releaseDeferredRuntimeSyncs())
		}
	}
	if reduction.Notices.BackgroundNotice != nil {
		kind := uiStatusNoticeSuccess
		if reduction.Notices.BackgroundNotice.Kind == runtimestate.BackgroundNoticeError {
			kind = uiStatusNoticeError
		}
		cmds = append(cmds, m.sendTransientStatusWithNoticeID(reduction.Notices.BackgroundNotice.Message, kind, transientStatusDuration, uiStatusNoticeReplace, ""))
	}
	if reduction.PendingInput.PromptHistoryCommand != nil && strings.TrimSpace(reduction.PendingInput.PromptHistoryCommand.Text) != "" {
		cmds = append(cmds, m.recordPromptHistory(reduction.PendingInput.PromptHistoryCommand.Text))
	}
	if transcriptSync.Reason != runtimestate.RuntimeTranscriptSyncNone {
		syncDecision := a.syncConversationFromRuntimeTranscriptCommand(transcriptSync)
		cmds = append(cmds, syncDecision.cmd)
		awaitsHydration = awaitsHydration || syncDecision.awaitsHydration
	} else if shouldRefreshDeferredCommittedTailOnRunEnd(m, evt) {
		cmds = append(cmds, m.requestRuntimeCommittedConversationSync())
	}
	return runtimeEventApplyResult{cmd: batchCmds(cmds...), transcriptMutated: transcriptMutated, awaitsHydration: awaitsHydration}
}

func runtimeTranscriptSyncReasonLabel(sync runtimestate.RuntimeTranscriptSyncCommand) string {
	if sync.Reason == runtimestate.RuntimeTranscriptSyncNone {
		return ""
	}
	return string(sync.Reason)
}

func (a uiRuntimeAdapter) syncConversationFromRuntimeTranscriptCommand(sync runtimestate.RuntimeTranscriptSyncCommand) runtimeTranscriptSyncDecision {
	switch sync.Reason {
	case runtimestate.RuntimeTranscriptSyncRecovery, runtimestate.RuntimeTranscriptSyncStreamGap:
		return a.model.startRuntimeTranscriptSyncRequest(runtimeTranscriptSyncRequestForPage(a.model.transcriptRequestForCurrentMode(), false, runtimeTranscriptSyncCauseContinuityRecovery, sync.RecoveryCause))
	case runtimestate.RuntimeTranscriptSyncCommittedAdvance, runtimestate.RuntimeTranscriptSyncOngoingErrorUpdated:
		return a.syncConversationFromEngine()
	default:
		return runtimeTranscriptSyncDecision{}
	}
}

func (a uiRuntimeAdapter) syncConversationFromEngine() runtimeTranscriptSyncDecision {
	m := a.model
	if !m.hasRuntimeClient() {
		return runtimeTranscriptSyncDecision{}
	}
	return m.startRuntimeTranscriptSyncRequest(runtimeTranscriptSyncRequestForPage(m.transcriptRequestForCurrentMode(), false, runtimeTranscriptSyncCauseCommittedConversation, clientui.TranscriptRecoveryCauseNone))
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
