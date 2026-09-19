package app

import (
	"fmt"
	"strings"

	"core/cli/tui"
	"core/shared/clientui"
	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"
	"core/shared/textutil"

	tea "github.com/charmbracelet/bubbletea"
)

func (m *uiModel) applyAdmittedTranscriptMessageState(
	message *transcriptpb.Message,
	admission runtimeTupleMergeResult,
) tea.Cmd {
	if m == nil {
		return nil
	}
	if m.turnQueueHook != nil {
		if message.Event.GetHydration() != nil {
			m.turnQueueHook.OnTurnQueueAborted()
		} else {
			m.turnQueueHook.OnTranscriptMessage(message)
		}
	}
	switch message.Event.Payload.(type) {
	case *transcriptpb.Event_Hydration:
		return m.applyTranscriptHydration(message.Event.GetHydration(), admission)
	case *transcriptpb.Event_ThinkingStatusUpdate:
		m.applyTranscriptThinkingStatusUpdate(message.Event.GetThinkingStatusUpdate())
	case *transcriptpb.Event_ReasoningTraceReset:
		// A reset replaces the live reasoning body. The typed status remains
		// current until another status arrives or the owning step finishes.
	case *transcriptpb.Event_UserMessageFlushed:
		return m.applyTranscriptUserMessageFlushed(message.Event.GetUserMessageFlushed())
	case *transcriptpb.Event_QueuedMessageState:
		return m.applyTranscriptQueuedMessageState(message.Event.GetQueuedMessageState())
	case *transcriptpb.Event_PendingWorkChanged:
		return m.requestPendingWorkRefresh(m.pendingWorkRefresh.sessionID)
	case *transcriptpb.Event_PendingWorkRestored:
		restoration := message.Event.GetPendingWorkRestored().Restoration
		m.inputController().restoreServerOrderedTextBeforeComposer(restoration.CanonicalInput)
	case *transcriptpb.Event_SessionSettingFeedback:
		return m.applySessionSettingFeedback(message.Event.GetSessionSettingFeedback())
	case *transcriptpb.Event_HumanInputInterrupted:
		return m.applyTranscriptHumanInputInterrupted(message.Event.GetHumanInputInterrupted())
	case *transcriptpb.Event_StepState:
		m.applyTranscriptStepState(message.Event.GetStepState())
	case *transcriptpb.Event_RuntimeReadModelUpdate:
		cmd := m.applyTranscriptRuntimeReadModelUpdate(admission)
		return sequenceCmds(cmd, m.missingPromptRehydrationCmd(admission.view.Session.SessionId))
	case *transcriptpb.Event_SessionStatus:
		m.applyTranscriptSessionStatus(message.Event.GetSessionStatus())
	case *transcriptpb.Event_SessionIdentity:
		return m.applyTranscriptSessionIdentity(message.Event.GetSessionIdentity())
	case *transcriptpb.Event_CompactionStatus:
		// RuntimeActivity is the authoritative compaction lifecycle. This event
		// carries lifecycle notification facts, not live-session state.
		status := message.Event.GetCompactionStatus()
		if status.Mode == transcriptpb.CompactionMode_COMPACTION_MODE_MANUAL && status.RequestId != nil {
			requestID, err := runtimeids.ParseCompactionRequestID(*status.RequestId)
			if err != nil {
				panic(err)
			}
			switch status.State {
			case transcriptpb.CompactionState_COMPACTION_STATE_COMPLETED:
				if m.clearPendingCompactionRequest(requestID) && m.turnQueueHook != nil {
					m.turnQueueHook.OnUserCompactionCompleted(m.inputController().turnQueueDrained())
				}
			case transcriptpb.CompactionState_COMPACTION_STATE_FAILED:
				m.clearPendingCompactionRequest(requestID)
			}
		}
	case *transcriptpb.Event_ContextUsage:
		m.applyTranscriptContextUsage(message.Event.GetContextUsage())
	case *transcriptpb.Event_GoalStatus:
		// The runtime-client main-view cache is the goal read model used by the
		// status line and goal flow.
	case *transcriptpb.Event_BackgroundActivity:
		m.applyTranscriptBackgroundActivity(message.Event.GetBackgroundActivity())
		if m.processList.open {
			return m.requestProcessListRefresh()
		}
	case *transcriptpb.Event_Prompt:
		prompt := message.Event.GetPrompt()
		if prompt.Status == transcriptpb.PromptStatus_PROMPT_STATUS_RESOLVED {
			return m.askController().resolvePrompt(string(transcriptPromptToolCallID(prompt)))
		}
		cmd := m.askController().acceptEvent(m.transcriptPromptEvent(prompt))
		m.reconcileMissingPromptRecoveryScope()
		return cmd
	case *transcriptpb.Event_WorktreeTransitionOutcome:
		return m.reconcileTranscriptWorktreeTransitionOutcome(message.Event.GetWorktreeTransitionOutcome())
	case *transcriptpb.Event_OperationalDiagnostic:
		return m.applyTranscriptOperationalDiagnostic(message.Event.GetOperationalDiagnostic())
	case *transcriptpb.Event_ConnectionReplaced:
		replacement := message.Event.GetConnectionReplaced()
		return m.sendTransientStatusWithNoticeID(
			fmt.Sprintf("Connection %s is unavailable; using %s.", replacement.PreviousId, replacement.CurrentId),
			uiStatusNoticeInfo, transientStatusDuration, uiStatusNoticeReplace, "",
		)
	}
	return nil
}

func (m *uiModel) applyTranscriptHydration(
	hydration *transcriptpb.Hydration,
	admission runtimeTupleMergeResult,
) tea.Cmd {
	var cmds []tea.Cmd
	sessionID, err := runtimeids.ParseSessionID(hydration.SessionIdentity.SessionId)
	if err != nil {
		panic(err)
	}
	cmds = append(cmds, m.advancePendingWorkRefreshScope(sessionID))
	cmds = append(cmds, m.applyTranscriptSessionIdentity(hydration.SessionIdentity))
	m.applyTranscriptSessionStatus(hydration.SessionStatus)
	cmds = append(cmds, m.applyTranscriptRuntimeReadModelUpdate(admission))

	m.reasoningStatusHeader = ""
	if hydration.ActiveThinkingStatus != nil {
		m.applyTranscriptThinkingStatusUpdate(hydration.ActiveThinkingStatus)
	}
	if hydration.ActiveStep != nil {
		m.applyTranscriptStepState(hydration.ActiveStep)
	}

	cmds = append(cmds, m.reconcileTranscriptPrompts(hydration.PendingPrompts))
	cmds = append(cmds, m.missingPromptRehydrationCmd(hydration.SessionIdentity.SessionId))
	currentSessionID := strings.TrimSpace(m.sessionID)
	preserved := m.processList.entries[:0]
	for _, entry := range m.processList.entries {
		if strings.TrimSpace(entry.OwnerSessionId) != currentSessionID {
			preserved = append(preserved, entry)
		}
	}
	m.processList.entries = preserved
	m.processList.selection = 0
	for _, background := range hydration.BackgroundActivities {
		m.applyTranscriptBackgroundActivity(background)
	}
	if hydration.ContextUsage == nil {
		m.setRuntimeContextUsage("", &runtimepb.ContextUsage{})
	} else {
		m.applyTranscriptContextUsage(hydration.ContextUsage)
	}
	if m.processList.open {
		cmds = append(cmds, m.requestProcessListRefresh())
	}
	return batchCmds(cmds...)
}

func (m *uiModel) missingPromptRehydrationCmd(sessionID string) tea.Cmd {
	m.reconcileMissingPromptRecoveryScope()
	scope, missing := m.missingPromptRecoveryScopeFor(sessionID)
	if !missing {
		return nil
	}
	if m.missingPromptRecovery != nil && *m.missingPromptRecovery == scope {
		return nil
	}
	m.missingPromptRecovery = &scope
	return func() tea.Msg { return missingPromptRehydrationMsg{scope: scope} }
}

func (m *uiModel) reconcileMissingPromptRecoveryScope() {
	if m == nil || m.missingPromptRecovery == nil {
		return
	}
	if !m.matchesMissingPromptRecoveryScope(*m.missingPromptRecovery) {
		m.missingPromptRecovery = nil
	}
}

func (m *uiModel) missingPromptRecoveryScopeFor(sessionID string) (missingPromptRecoveryScope, bool) {
	if m == nil ||
		sessionID == "" ||
		m.runtimeActivityProjection == nil ||
		m.runtimeActivityProjection.State != runtimepb.ActivityState_RUNTIME_ACTIVITY_AWAITING_PROMPT ||
		m.runtimeActivityProjection.ActiveStep == nil ||
		m.ask.hasCurrent() {
		return missingPromptRecoveryScope{}, false
	}
	activeStep := m.runtimeActivityProjection.ActiveStep
	runID, err := runtimeids.ParseRunID(activeStep.RunId)
	if err != nil {
		panic(err)
	}
	stepID, err := runtimeids.ParseStepID(activeStep.StepId)
	if err != nil {
		panic(err)
	}
	return missingPromptRecoveryScope{
		sessionID: sessionID,
		runID:     runID,
		stepID:    stepID,
	}, true
}

func (m *uiModel) matchesMissingPromptRecoveryScope(scope missingPromptRecoveryScope) bool {
	current, missing := m.missingPromptRecoveryScopeFor(scope.sessionID)
	if !missing || current != scope {
		return false
	}
	if sessionID := m.currentRuntimeSessionID(); sessionID != "" && sessionID != scope.sessionID {
		return false
	}
	return true
}

func (m *uiModel) applyTranscriptRuntimeReadModelUpdate(admission runtimeTupleMergeResult) tea.Cmd {
	switch admission.decision {
	case runtimeTupleRefresh:
		return m.startRuntimeMainViewRefreshRequest(runtimeReadModelResetMainViewRefreshRequest()).cmd
	}
	if !admission.project {
		return nil
	}
	view := admission.view
	if err := m.applyRuntimeActivityProjection(view.Activity); err != nil {
		m.activity = uiActivityError
		return m.sendTransientStatusWithNoticeID(
			"invalid runtime activity: "+err.Error(),
			uiStatusNoticeError,
			transientStatusDuration,
			uiStatusNoticeReplace,
			"",
		)
	}
	if protoapi.RuntimeActivityActiveForControl(view.Activity) {
		return nil
	}
	var cmd tea.Cmd
	if m.hasPendingInterrupt() {
		cmd = m.acknowledgePendingInterrupt()
	}
	return tea.Batch(cmd, m.releaseDeferredRuntimeSyncs())
}

func (m *uiModel) applyTranscriptStepState(state *transcriptpb.StepState) {
	if state.Lifecycle == transcriptpb.StepLifecycle_STEP_LIFECYCLE_STARTED {
		return
	}
	m.reasoningStatusHeader = ""
}

func (m *uiModel) applyTranscriptThinkingStatusUpdate(update *transcriptpb.ThinkingStatusUpdate) {
	m.reasoningStatusHeader = strings.TrimSpace(update.Text)
}

func (m *uiModel) applyTranscriptSessionStatus(status *transcriptpb.SessionStatus) {
	m.reviewerMode = status.ReviewerFrequency
	m.reviewerEnabled = status.ReviewerEnabled
	m.autoCompactionEnabled = status.AutoCompactionEnabled
	m.questionsEnabled = status.QuestionsEnabled
	m.fastModeAvailable = status.FastModeAvailable
	m.fastModeEnabled = status.FastModeEnabled
	m.thinkingLevel = status.ThinkingLevel
}

func (m *uiModel) applyTranscriptSessionIdentity(identity *transcriptpb.SessionIdentity) tea.Cmd {
	previousSessionID := strings.TrimSpace(m.sessionID)
	nextSessionID := identity.SessionId
	previousTarget := m.sessionExecutionTarget
	m.sessionExecutionTarget = nil
	if identity.ExecutionTarget != nil {
		normalized := clientui.NormalizeSessionExecutionTarget(identity.ExecutionTarget)
		m.sessionExecutionTarget = normalized
	}
	m.sessionID = nextSessionID
	m.reconcileMissingPromptRecoveryScope()
	m.sessionName = ""
	if identity.SessionName != nil {
		m.sessionName = strings.TrimSpace(*identity.SessionName)
	}
	m.conversationFreshness = identity.ConversationFreshness
	titleCmd := tea.SetWindowTitle(sessionTitle(m.sessionName))
	if previousTarget != nil && m.sessionExecutionTarget != nil &&
		(!textutil.EqualOptional(previousTarget.WorkspaceId, m.sessionExecutionTarget.WorkspaceId) ||
			previousTarget.WorkspaceRoot != m.sessionExecutionTarget.WorkspaceRoot) {
		m.sessionRetargeted = true
		m.nextSessionID = nextSessionID
		m.exitAction = UIActionOpenSession
		return tea.Batch(titleCmd, tea.Quit)
	}
	if previousSessionID == "" || previousSessionID == nextSessionID {
		return titleCmd
	}
	m.pendingCompactionRequestIDs = nil
	m.askController().cancelActiveDelivery()
	promptCmd := m.reconcileTranscriptPrompts(nil)
	rollbackCmd := m.discardRollbackStateForSessionReplacement()
	cancelCmd := m.cancelPendingDetailTranscriptRequest()
	m.detailTranscript.reset()
	resetCmd := m.forwardToView(tui.ResetDetailTranscriptMsg{})
	loadCmd := m.loadDetailTranscriptPageCmd(m.detailTranscript.requestedPageForDetailEntry())
	return tea.Batch(
		promptCmd,
		sequenceCmds(titleCmd, rollbackCmd, cancelCmd, resetCmd, loadCmd),
	)
}

func (m *uiModel) applyTranscriptContextUsage(usage *runtimepb.ContextUsage) {
	m.setRuntimeContextUsage(m.currentRuntimeSessionID(), usage)
}

func (m *uiModel) applyTranscriptUserMessageFlushed(flushed *transcriptpb.UserMessageFlushed) tea.Cmd {
	m.conversationFreshness = runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_ESTABLISHED
	m.localConversationTurn = true
	return nil
}

func (m *uiModel) applyTranscriptQueuedMessageState(state *transcriptpb.QueuedMessageState) tea.Cmd {
	if state.Status == transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_ACCEPTED {
		m.registerSteeredQueuedUserMessage(clientui.QueuedUserMessage{
			ID:   state.QueueItemId,
			Text: dereferenceTranscriptText(state.Text),
		})
		return nil
	}
	ids := []string{state.QueueItemId}
	index := m.injectedQueueIndexByAnyID(state.QueueItemId)
	if index < 0 {
		m.retainUnownedQueuedTerminalState(state)
		return nil
	}
	localText := m.injectedQueue[index].Text
	m.removeInjectedQueueItemsByIDs(ids)
	if state.Status != transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_FAILED {
		return nil
	}
	m.inputController().restoreInjectedTextIntoInput(localText)
	return m.sendTransientStatusWithNoticeID(
		"queued message was not submitted; restored to input",
		uiStatusNoticeError,
		transientStatusDuration,
		uiStatusNoticeReplace,
		"",
	)
}

func (m *uiModel) applyTranscriptHumanInputInterrupted(event *transcriptpb.HumanInputInterrupted) tea.Cmd {
	ids := make([]string, 0, len(event.Items))
	texts := make([]string, 0, len(event.Items))
	for _, item := range event.Items {
		id := item.QueueItemId
		if m.injectedQueueIndexByAnyID(id) < 0 {
			m.retainUnownedQueuedTerminalState(&transcriptpb.QueuedMessageState{
				QueueItemId: item.QueueItemId,
				Status:      transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_DISCARDED,
			})
		} else {
			ids = append(ids, id)
		}
		texts = append(texts, item.Text)
	}
	m.removeInjectedQueueItemsByIDs(ids)
	var cmd tea.Cmd
	m.inputController().restoreServerOrderedTextBeforeComposer(strings.Join(texts, "\n\n"))
	if m.hasPendingInterrupt() {
		cmd = tea.Batch(cmd, m.acknowledgePendingInterrupt())
	}
	return tea.Batch(cmd, m.sendTransientStatusWithNoticeID(
		"interrupted input was restored",
		uiStatusNoticeError,
		transientStatusDuration,
		uiStatusNoticeReplace,
		"",
	))
}

func dereferenceTranscriptText(text *string) string {
	if text == nil {
		return ""
	}
	return *text
}

func (m *uiModel) transcriptPromptEvent(prompt *transcriptpb.Prompt) askEvent {
	if m.promptAnswers != nil {
		return m.promptAnswers.event(prompt)
	}
	return askEvent{
		prompt: cloneTranscriptPromptForAsk(prompt),
	}
}

func (m *uiModel) reconcileTranscriptPrompts(prompts []*transcriptpb.Prompt) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(prompts)+1)
	present := make(map[string]struct{}, len(prompts))
	for _, prompt := range prompts {
		present[string(transcriptPromptToolCallID(prompt))] = struct{}{}
	}
	var stale []string
	if m.ask.hasCurrent() {
		id := m.ask.current.toolCallID()
		if _, exists := present[id]; !exists {
			stale = append(stale, id)
		}
	}
	for _, queued := range m.ask.queue {
		id := queued.toolCallID()
		if _, exists := present[id]; !exists {
			stale = append(stale, id)
		}
	}
	for _, id := range stale {
		cmds = append(cmds, m.askController().resolvePrompt(id))
	}
	for _, prompt := range prompts {
		event := m.transcriptPromptEvent(prompt)
		event.origin = promptDeliveryHydration
		cmds = append(cmds, m.askController().acceptEvent(event))
	}
	return batchCmds(cmds...)
}

func (m *uiModel) applyTranscriptOperationalDiagnostic(diagnostic *transcriptpb.OperationalDiagnostic) tea.Cmd {
	switch diagnostic.Code {
	case transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_SLEEP_GUARD_FAILED:
		return m.sendTransientStatusWithNoticeID(
			"sleep prevention failed: "+diagnostic.Detail,
			uiStatusNoticeError,
			transientStatusDuration,
			uiStatusNoticeReplace,
			"",
		)
	case transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_PROMPT_HISTORY_PERSIST_FAILED:
		return m.sendTransientStatusWithNoticeID(
			"prompt history persistence failed: "+diagnostic.Detail,
			uiStatusNoticeError,
			transientStatusDuration,
			uiStatusNoticeReplace,
			"",
		)
	case transcriptpb.OperationalDiagnosticCode_OPERATIONAL_DIAGNOSTIC_CODE_IN_FLIGHT_CLEAR_FAILED:
		return m.sendTransientStatusWithNoticeID(
			"run cleanup failed: "+diagnostic.Detail,
			uiStatusNoticeError,
			transientStatusDuration,
			uiStatusNoticeReplace,
			"",
		)
	default:
		panic(fmt.Sprintf("unsupported transcript operational diagnostic %q", diagnostic.Code))
	}
}
