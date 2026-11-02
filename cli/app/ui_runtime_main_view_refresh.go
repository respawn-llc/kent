package app

import runtimepb "core/shared/protoapi/gen/kent/api/runtime"

import (
	"strings"
	"time"

	"core/cli/app/internal/runtimeattach"
	"core/cli/tui"

	tea "github.com/charmbracelet/bubbletea"
)

type runtimeMainViewCandidateClient interface {
	fetchMainView() (*runtimepb.MainView, error)
}

func (m *uiModel) startRuntimeMainViewRefresh() tea.Cmd {
	if m == nil || !m.hasRuntimeClient() {
		return nil
	}
	if m.runtimeMainViewBusy {
		m.runtimeMainViewPendingSet = true
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
	return m.startRuntimeMainViewRefresh()
}

func (m *uiModel) handleRuntimeMainViewRefreshed(msg runtimeMainViewRefreshedMsg) tea.Cmd {
	if m == nil || msg.token != m.runtimeMainViewToken {
		return nil
	}
	m.runtimeMainViewBusy = false
	if msg.err != nil {
		m.observeRuntimeRequestResult(msg.err)
		if m.runtimeDisconnectStatusVisible() &&
			(runtimeattach.IsRuntimeConnectionError(msg.err) || runtimeattach.IsRuntimeTimeoutError(msg.err)) {
			return tea.Tick(transcriptSubscriptionRetryDelay, func(time.Time) tea.Msg {
				return runtimeMainViewRetryMsg{token: msg.token}
			})
		}
		return tea.Batch(
			m.drainPendingRuntimeMainViewRefresh(),
			m.sendTransientStatusWithNoticeID(
				"Runtime state refresh failed: "+runtimeattach.FormatSubmissionError(msg.err),
				uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "",
			),
		)
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
	return sequenceCmds(applyCmd, m.applyRuntimeSessionMetadata(canonical.Session), m.drainPendingRuntimeMainViewRefresh())
}

func (m *uiModel) applyRuntimeSessionMetadata(session *runtimepb.SessionView) tea.Cmd {
	if m == nil {
		return nil
	}
	previousSessionID := strings.TrimSpace(m.currentRuntimeSessionID())
	nextSessionID := strings.TrimSpace(session.SessionId)
	if nextSessionID != "" {
		m.setKnownSessionTarget(nextSessionID)
	}
	m.sessionName = session.SessionName
	m.conversationFreshness = session.ConversationFreshness
	if previousSessionID == "" || previousSessionID == nextSessionID {
		return nil
	}
	rollbackCmd := m.discardRollbackStateForSessionReplacement()
	cancelCmd := m.cancelPendingDetailTranscriptRequest()
	m.detailTranscript.reset()
	resetCmd := m.forwardToView(tui.ResetDetailTranscriptMsg{})
	loadCmd := m.loadDetailTranscriptPageCmd(m.detailTranscript.requestedPageForDetailEntry())
	return sequenceCmds(rollbackCmd, cancelCmd, resetCmd, loadCmd)
}
