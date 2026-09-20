package app

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

import (
	"strings"

	"core/cli/tui"
	"core/shared/protoapi"

	tea "github.com/charmbracelet/bubbletea"
)

type runtimeMainViewCandidateClient interface {
	fetchMainView() (*runtimepb.MainView, error)
}

func (m *uiModel) startRuntimeMainViewRefresh(interruptedSubmitToken *uint64) tea.Cmd {
	if m == nil || !m.hasRuntimeClient() {
		return nil
	}
	if m.runtimeMainViewBusy {
		m.runtimeMainViewPendingSet = true
		if interruptedSubmitToken != nil {
			m.runtimeMainViewPendingInterruptedSubmitToken = interruptedSubmitToken
		}
		return nil
	}
	m.runtimeMainViewToken++
	token := m.runtimeMainViewToken
	client := m.runtimeClient()
	m.runtimeMainViewBusy = true
	var metadataBaselineRevision *uint64
	if sessionClient, ok := client.(*sessionRuntimeClient); ok {
		revision := sessionClient.mainViewMetadataRevision()
		metadataBaselineRevision = &revision
	}
	return func() tea.Msg {
		var (
			view *runtimepb.MainView
			err  error
		)
		if candidateClient, ok := client.(runtimeMainViewCandidateClient); ok {
			view, err = candidateClient.fetchMainView()
		} else {
			view, err = client.RefreshMainView()
		}
		return runtimeMainViewRefreshedMsg{
			token:                    token,
			interruptedSubmitToken:   interruptedSubmitToken,
			metadataBaselineRevision: metadataBaselineRevision,
			view:                     view,
			err:                      err,
		}
	}
}

func (m *uiModel) drainPendingRuntimeMainViewRefresh() tea.Cmd {
	if m == nil || !m.runtimeMainViewPendingSet || m.runtimeMainViewBusy {
		return nil
	}
	m.runtimeMainViewPendingSet = false
	interruptedSubmitToken := m.runtimeMainViewPendingInterruptedSubmitToken
	m.runtimeMainViewPendingInterruptedSubmitToken = nil
	return m.startRuntimeMainViewRefresh(interruptedSubmitToken)
}

func (m *uiModel) handleRuntimeMainViewRefreshed(msg runtimeMainViewRefreshedMsg) tea.Cmd {
	if m == nil || msg.token != m.runtimeMainViewToken {
		return nil
	}
	m.runtimeMainViewBusy = false
	if msg.err != nil {
		m.observeRuntimeRequestResult(msg.err)
		return m.drainPendingRuntimeMainViewRefresh()
	}
	m.observeRuntimeRequestResult(nil)
	canonical := msg.view
	if client, ok := m.runtimeClient().(*sessionRuntimeClient); ok {
		canonical = client.mergeMainViewCandidate(
			msg.view,
			runtimeTupleIngressAuthoritativeSnapshot,
			msg.metadataBaselineRevision,
		).view
	}
	applyCmd := m.applyRuntimeMainViewState(canonical)
	var restoreCmd tea.Cmd
	if msg.interruptedSubmitToken != nil && m.activeSubmit.token == *msg.interruptedSubmitToken &&
		canonical.Activity != nil && !protoapi.RuntimeActivityActiveForControl(canonical.Activity) {
		m.activeSubmit = activeSubmitState{}
		restoreCmd = m.inputController().restoreInterruptedInputsIntoComposer()
	}
	return sequenceCmds(applyCmd, restoreCmd, m.applyRuntimeSessionMetadata(canonical.Session), m.drainPendingRuntimeMainViewRefresh())
}

func (m *uiModel) applyRuntimeSessionMetadata(session *runtimepb.SessionView) tea.Cmd {
	if m == nil {
		return nil
	}
	previousSessionID := strings.TrimSpace(m.sessionID)
	nextSessionID := strings.TrimSpace(session.SessionId)
	if nextSessionID != "" {
		m.sessionID = nextSessionID
	}
	if strings.TrimSpace(session.GetSessionName()) != "" {
		m.sessionName = strings.TrimSpace(session.GetSessionName())
	}
	m.conversationFreshness = session.ConversationFreshness
	if previousSessionID == "" || nextSessionID == "" || previousSessionID == nextSessionID {
		return nil
	}
	rollbackCmd := m.discardRollbackStateForSessionReplacement()
	cancelCmd := m.cancelPendingDetailTranscriptRequest()
	m.detailTranscript.reset()
	resetCmd := m.forwardToView(tui.ResetDetailTranscriptMsg{})
	loadCmd := m.loadDetailTranscriptPageCmd(m.detailTranscript.requestedPageForDetailEntry())
	return sequenceCmds(rollbackCmd, cancelCmd, resetCmd, loadCmd)
}
