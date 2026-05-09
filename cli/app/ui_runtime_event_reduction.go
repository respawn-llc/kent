package app

import "builder/shared/clientui"

func shouldRefreshDeferredCommittedTailOnRunEnd(m *uiModel, evt clientui.Event) bool {
	if m == nil || !m.hasRuntimeClient() || len(m.deferredCommittedTail) == 0 {
		return false
	}
	if evt.Kind != clientui.EventRunStateChanged || evt.RunState == nil {
		return false
	}
	return !evt.RunState.Busy
}

func (a uiRuntimeAdapter) runtimeRunState() clientui.RuntimeRunState {
	m := a.model
	return clientui.RuntimeRunState{
		Busy:             m.busy,
		Compacting:       m.compacting,
		ReviewerRunning:  m.reviewerRunning,
		ReviewerBlocking: m.reviewerBlocking,
		GoalLoop:         m.goalRun,
	}
}

func (a uiRuntimeAdapter) runtimeConversationState() clientui.RuntimeConversationState {
	return clientui.RuntimeConversationState{Freshness: a.model.conversationFreshness}
}

func (a uiRuntimeAdapter) runtimeReasoningState() clientui.RuntimeReasoningState {
	return clientui.RuntimeReasoningState{StatusHeader: a.model.reasoningStatusHeader}
}

func (a uiRuntimeAdapter) pendingInputState() clientui.PendingInputState {
	m := a.model
	return clientui.PendingInputState{
		Input:             m.input,
		PendingInjected:   m.pendingInjected,
		LockedInjectText:  m.lockedInjectText,
		LockedInjectID:    m.lockedInjectID,
		InputSubmitLocked: m.inputSubmitLocked,
	}
}

func (a uiRuntimeAdapter) applyRuntimeEventReduction(reduction clientui.RuntimeEventReduction) {
	m := a.model
	m.busy = reduction.RunState.State.Busy
	m.goalRun = reduction.RunState.State.GoalLoop
	m.compacting = reduction.RunState.State.Compacting
	m.reviewerRunning = reduction.RunState.State.ReviewerRunning
	m.reviewerBlocking = reduction.RunState.State.ReviewerBlocking
	m.conversationFreshness = reduction.Conversation.State.Freshness
	m.reasoningStatusHeader = reduction.Reasoning.State.StatusHeader
	m.pendingInjected = reduction.PendingInput.State.PendingInjected
	m.lockedInjectText = reduction.PendingInput.State.LockedInjectText
	m.lockedInjectID = reduction.PendingInput.State.LockedInjectID
	m.inputSubmitLocked = reduction.PendingInput.State.InputSubmitLocked
	switch reduction.PendingInput.DraftCommand {
	case clientui.RuntimePendingInputClearDraft:
		m.clearInput()
	}
	switch reduction.RunState.Activity {
	case clientui.RuntimeActivityRunning:
		m.activity = uiActivityRunning
	case clientui.RuntimeActivityIdle:
		m.activity = uiActivityIdle
	}
	switch reduction.BackgroundProcesses.Command {
	case clientui.RuntimeBackgroundProcessRefresh:
		m.refreshProcessEntriesIfOpen()
	}
}

func (a uiRuntimeAdapter) reconcileInterruptFromRunState(evt clientui.Event) {
	m := a.model
	if m == nil || evt.Kind != clientui.EventRunStateChanged || evt.RunState == nil || evt.RunState.Busy {
		return
	}
	if evt.RunState.Status != clientui.RunStatusInterrupted {
		m.pendingInterrupt = false
		return
	}
	if m.pendingInterrupt {
		if m.activeSubmit.restoreOnInterrupt && !m.activeSubmit.flushed {
			c := uiInputController{model: m}
			c.restoreSubmittedTextIntoInput(m.activeSubmit.text)
		}
		m.activeSubmit = activeSubmitState{}
		c := uiInputController{model: m}
		c.releaseLockedInjectedInput(true)
		c.restorePendingInjectedIntoInput()
		c.restoreQueuedMessagesIntoInput()
		m.pendingInterrupt = false
	}
	m.activity = uiActivityInterrupted
	m.clearReviewerState()
}

func (a uiRuntimeAdapter) effectiveRuntimeTranscriptSync(evt clientui.Event, proposed clientui.RuntimeTranscriptSyncCommand) clientui.RuntimeTranscriptSyncCommand {
	if evt.Kind != clientui.EventConversationUpdated {
		return proposed
	}
	if !shouldRecoverCommittedTranscriptFromConversationUpdate(a.model, evt) {
		return clientui.RuntimeTranscriptSyncCommand{}
	}
	if proposed.IsSet() {
		return proposed
	}
	return clientui.RuntimeTranscriptSyncCommand{Reason: clientui.RuntimeTranscriptSyncCommittedAdvance}
}
