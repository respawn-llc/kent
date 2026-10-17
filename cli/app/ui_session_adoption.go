package app

import (
	"context"

	"core/shared/clientui"
	"core/shared/runtimeids"

	tea "github.com/charmbracelet/bubbletea"
)

type sessionObservationLoadedMsg struct {
	scope       context.Context
	sessionID   runtimeids.SessionID
	plan        sessionLaunchPlan
	observation *runtimeLaunchPlan
	err         error
}

func (m *uiModel) applyNewSessionActionDone(msg newSessionActionDoneMsg) tea.Cmd {
	if msg.origin != m {
		return nil
	}
	m.pendingNewChatActions--
	if m.exitAction != UIActionNone {
		return nil
	}
	m.observeRuntimeRequestResult(msg.err)
	var cmds []tea.Cmd
	if msg.err != nil {
		if msg.submittedText != nil {
			m.inputController().restoreSubmittedTextIntoInput(*msg.submittedText)
		}
		cmds = append(cmds, m.sendTransientStatusWithNoticeID(msg.err.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, ""))
	}
	if msg.diagnostic != nil {
		cmds = append(cmds, m.acceptedSubmissionDiagnosticCmd(msg.diagnostic))
	}
	if msg.session != nil {
		if m.currentRuntimeSessionID() == msg.session.SessionId {
			return tea.Batch(append(cmds, m.reconcileSpinnerTicking(false))...)
		}
		if m.drafts != nil && m.currentRuntimeSessionID() != msg.session.SessionId {
			m.drafts = newUIDraftPersistence(m.drafts.client, nil)
		}
		m.stopSessionObservation()
		m.setKnownSessionTarget(msg.session.SessionId)
		m.resetPendingWorkRefreshScope(m.presentationTarget.(uiKnownSessionTarget).id)
		m.reconcileMissingPromptRecoveryScope()
		m.engine = nil
		m.runtimeActivityProjection = nil
		m.runtimeLifecycle.Run = clientui.IdleRunLifecycle()
		m.sessionExecutionTarget = nil
		m.eventDispatcher = newUIEventDispatcher(nil)
		m.ongoingTranscript = nil
		m.runtimeMainViewToken++
		m.runtimeMainViewBusy = false
		m.runtimeMainViewPendingSet = false
		cmds = append(cmds, m.resetSessionTranscriptPresentation())
		cmds = append(cmds, m.observeAdoptedSessionCmd())
	}
	cmds = append(cmds, m.reconcileSpinnerTicking(false))
	return tea.Batch(cmds...)
}

func (m *uiModel) stopSessionObservation() {
	if m.stopAttention != nil {
		m.stopAttention()
		m.stopAttention = nil
	}
	if m.cancelObservation != nil {
		m.cancelObservation()
		m.cancelObservation = nil
	}
	if m.sessionObservation != nil {
		m.sessionObservation.Close()
		m.sessionObservation = nil
	}
}

func (m *uiModel) isCurrentObservation(scope context.Context) bool {
	return scope == m.observationScope && (scope == nil || scope.Err() == nil)
}

func (m *uiModel) observeAdoptedSessionCmd() tea.Cmd {
	if m.observeSession == nil {
		return nil
	}
	id := m.presentationTarget.(uiKnownSessionTarget).id
	parent := m.observationContext
	if parent == nil {
		parent = context.Background()
	}
	scope, cancel := context.WithCancel(parent)
	m.observationScope = scope
	m.cancelObservation = cancel
	observe := m.observeSession
	return func() tea.Msg {
		plan, observation, err := observe(scope, id)
		return sessionObservationLoadedMsg{
			scope: scope, sessionID: id, plan: plan, observation: observation, err: err,
		}
	}
}

func (m *uiModel) sessionObservationLoading() bool {
	return m.cancelObservation != nil && m.engine == nil
}

func (m *uiModel) applySessionObservationLoaded(msg sessionObservationLoadedMsg) tea.Cmd {
	if msg.scope.Err() != nil || m.exitAction != UIActionNone || m.currentRuntimeSessionID() != msg.sessionID.String() {
		msg.observation.Close()
		return nil
	}
	if msg.err != nil {
		msg.observation.Close()
		m.stopSessionObservation()
		m.observeRuntimeRequestResult(msg.err)
		return m.sendTransientStatusWithNoticeID(msg.err.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	m.sessionObservation = msg.observation
	m.observeRuntimeRequestResult(nil)
	return m.installSessionObservation(msg.observation.Wiring, msg.plan)
}

func (m *uiModel) installSessionObservation(wiring *runtimeWiring, plan sessionLaunchPlan) tea.Cmd {
	m.engine = wiring.runtimeClient
	m.bindRuntimeConnectionObserver()
	m.eventDispatcher = wiring.eventDispatcher
	m.eventDispatcher.lifecycleHookIssues = wiring.lifecycleHookIssues
	m.eventDispatcher.lifecycleHookDone = wiring.lifecycleHookDone
	if wiring.attentionNotifications != nil {
		m.eventDispatcher.attentionEvents, m.stopAttention = startSessionAttentionEvents(m.observationScope, m.currentRuntimeSessionID(), wiring.attentionNotifications)
	}
	m.requestOngoingOpen = wiring.requestTranscriptOpen
	m.turnQueueHook = wiring.turnQueueHook
	m.worktreeClient = wiring.worktrees
	events := m.runtimeConnectionEvents
	m.promptAnswers = wiring.promptAnswers.withConnectionOutcomeSink(func(err error) {
		enqueueRuntimeConnectionStateChange(events, err)
	})
	m.promptAttention = wiring.promptAttention
	if m.terminalFocus.Known() {
		if m.terminalFocus.FocusedForAttention() {
			wiring.terminalFocus.MarkFocused()
		} else {
			wiring.terminalFocus.MarkBlurred()
		}
	}
	m.terminalFocus = wiring.terminalFocus
	m.statusConfig = plan.StatusConfig
	client := wiring.runtimeClient.(*sessionRuntimeClient)
	m.ongoingTranscript = newOngoingTranscriptController(m.ongoingSurface, m.ongoingFrameInput, client.admitTranscriptMessageState, m.applyAdmittedTranscriptMessageState)
	return m.startSessionObservationLoads()
}

func (m *uiModel) startSessionObservationLoads() tea.Cmd {
	return tea.Batch(
		m.eventDispatcher.wait(),
		waitRuntimeConnectionStateChange(m.runtimeConnectionEvents, m.observationScope),
		m.loadPromptHistoryCmd(),
		m.startRuntimeMainViewRefresh(nil),
	)
}

func withUISessionObservation(wiring *runtimeWiring, plan sessionLaunchPlan, parent context.Context) UIOption {
	return func(m *uiModel) {
		m.observationScope, m.cancelObservation = context.WithCancel(parent)
		m.startupCmds = append(m.startupCmds, m.installSessionObservation(wiring, plan))
	}
}
