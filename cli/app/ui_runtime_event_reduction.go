package app

import (
	"builder/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

func shouldRefreshDeferredCommittedTailOnRunEnd(m *uiModel, evt clientui.Event) bool {
	if m == nil || !m.hasRuntimeClient() || len(m.deferredCommittedTail) == 0 {
		return false
	}
	if evt.Kind != clientui.EventRunStateChanged || evt.RunState == nil {
		return false
	}
	return !evt.RunState.Lifecycle.IsRunning()
}

func (a uiRuntimeAdapter) runtimeRunState() clientui.RuntimeRunState {
	m := a.model
	if err := m.runtimeLifecycle.Run.Validate(); err != nil {
		panic(err)
	}
	if err := m.runtimeLifecycle.Reviewer.Validate(); err != nil {
		panic(err)
	}
	return clientui.RuntimeRunState{
		Run:        m.runtimeLifecycle.Run,
		Compaction: m.runtimeLifecycle.Compaction,
		Reviewer:   m.runtimeLifecycle.Reviewer,
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
		Input:            m.input,
		PendingInjected:  m.pendingInjected,
		LockedInjectText: m.lockedInjectText,
		LockedInjectID:   m.lockedInjectID,
		Submission:       clientui.NewInputSubmissionLifecycle(m.isInputSubmitLocked()),
	}
}

func (a uiRuntimeAdapter) applyRuntimeEventReduction(reduction clientui.RuntimeEventReduction) tea.Cmd {
	m := a.model
	var cmd tea.Cmd
	if reduction.RunState.Err != nil {
		m.activity = uiActivityError
		cmd = m.setTransientStatusWithKind("invalid runtime lifecycle: "+reduction.RunState.Err.Error(), uiStatusNoticeError)
	} else if err := m.setRunLifecycle(reduction.RunState.State.Run); err != nil {
		m.activity = uiActivityError
		cmd = m.setTransientStatusWithKind("invalid runtime lifecycle: "+err.Error(), uiStatusNoticeError)
	}
	m.setCompacting(reduction.RunState.State.Compaction.IsRunning())
	m.setReviewerRunning(reduction.RunState.State.Reviewer.IsRunning())
	m.setReviewerBlocking(reduction.RunState.State.Reviewer.IsBlocking())
	m.conversationFreshness = reduction.Conversation.State.Freshness
	m.reasoningStatusHeader = reduction.Reasoning.State.StatusHeader
	m.pendingInjected = reduction.PendingInput.State.PendingInjected
	m.lockedInjectText = reduction.PendingInput.State.LockedInjectText
	m.lockedInjectID = reduction.PendingInput.State.LockedInjectID
	m.setInputSubmitLocked(reduction.PendingInput.State.Submission.IsLocked())
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
	return cmd
}

func (a uiRuntimeAdapter) reconcileInterruptFromRunState(evt clientui.Event) {
	m := a.model
	if m == nil || evt.Kind != clientui.EventRunStateChanged || evt.RunState == nil || evt.RunState.Lifecycle.IsRunning() {
		return
	}
	if evt.RunState.Status != clientui.RunStatusInterrupted {
		m.setPendingInterrupt(false)
		return
	}
	if m.hasPendingInterrupt() {
		if m.activeSubmit.restoreOnInterrupt && !m.activeSubmit.flushed {
			c := uiInputController{model: m}
			c.restoreSubmittedTextIntoInput(m.activeSubmit.text)
		}
		m.activeSubmit = activeSubmitState{}
		c := uiInputController{model: m}
		c.releaseLockedInjectedInput(true)
		c.restorePendingInjectedIntoInput()
		c.restoreQueuedMessagesIntoInput()
		m.setPendingInterrupt(false)
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
