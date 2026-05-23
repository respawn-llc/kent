package app

import (
	"strconv"
	"strings"
	"time"

	"builder/cli/tui"
	"builder/shared/clientui"
	"builder/shared/transcriptdiag"
	tea "github.com/charmbracelet/bubbletea"
)

var uiRuntimeHydrationRetryDelay = 750 * time.Millisecond

func (m *uiModel) requestRuntimeMainViewRefresh() tea.Cmd {
	if !m.hasRuntimeClient() {
		return nil
	}
	m.runtimeMainViewToken++
	token := m.runtimeMainViewToken
	client := m.runtimeClient()
	return func() tea.Msg {
		view, err := client.RefreshMainView()
		return runtimeMainViewRefreshedMsg{token: token, view: view, err: err}
	}
}

func (m *uiModel) requestRuntimeTranscriptPage(request clientui.TranscriptPageRequest) tea.Cmd {
	return m.startRuntimeTranscriptPageRequest(request, true, runtimeTranscriptSyncCauseManualTranscriptRefresh, clientui.TranscriptRecoveryCauseNone)
}

func (m *uiModel) requestRuntimeBootstrapTranscriptSync() tea.Cmd {
	if !m.hasRuntimeClient() {
		return nil
	}
	return m.startRuntimeTranscriptPageRequest(m.transcriptRequestForCurrentMode(), false, runtimeTranscriptSyncCauseBootstrap, clientui.TranscriptRecoveryCauseNone)
}

func (m *uiModel) requestRuntimeCommittedConversationSync() tea.Cmd {
	if !m.hasRuntimeClient() {
		return nil
	}
	return m.startRuntimeTranscriptPageRequest(m.transcriptRequestForCurrentMode(), false, runtimeTranscriptSyncCauseCommittedConversation, clientui.TranscriptRecoveryCauseNone)
}

func (m *uiModel) requestRuntimeCommittedTranscriptSuffix(req clientui.CommittedTranscriptSuffixRequest) tea.Cmd {
	if !m.hasRuntimeClient() {
		return nil
	}
	client, ok := m.runtimeClient().(interface {
		RefreshCommittedTranscriptSuffix(clientui.CommittedTranscriptSuffixRequest) (clientui.CommittedTranscriptSuffix, error)
	})
	if !ok {
		return nil
	}
	req = clientui.NormalizeCommittedTranscriptSuffixRequest(req)
	m.runtimeCommittedSuffixToken++
	token := m.runtimeCommittedSuffixToken
	return func() tea.Msg {
		suffix, err := client.RefreshCommittedTranscriptSuffix(req)
		return runtimeCommittedTranscriptSuffixRefreshedMsg{token: token, req: req, suffix: suffix, err: err}
	}
}

func (m *uiModel) requestRuntimeCommittedGapSync() tea.Cmd {
	if !m.hasRuntimeClient() {
		return nil
	}
	return m.startRuntimeTranscriptPageRequest(m.transcriptRequestForCurrentMode(), false, runtimeTranscriptSyncCauseCommittedGap, clientui.TranscriptRecoveryCauseNone)
}

func (m *uiModel) requestRuntimeQueuedDrainTranscriptSync() tea.Cmd {
	if !m.hasRuntimeClient() {
		return nil
	}
	return m.startRuntimeTranscriptPageRequest(m.transcriptRequestForCurrentMode(), false, runtimeTranscriptSyncCauseQueuedDrain, clientui.TranscriptRecoveryCauseNone)
}

func (m *uiModel) requestRuntimeTranscriptSyncForContinuityLoss(cause clientui.TranscriptRecoveryCause) tea.Cmd {
	if !m.hasRuntimeClient() {
		return nil
	}
	return m.startRuntimeTranscriptPageRequest(m.transcriptRequestForCurrentMode(), false, runtimeTranscriptSyncCauseContinuityRecovery, cause)
}

func (m *uiModel) requestRuntimeDirtyFollowUpTranscriptSync(cause clientui.TranscriptRecoveryCause) tea.Cmd {
	if !m.hasRuntimeClient() {
		return nil
	}
	return m.startRuntimeTranscriptPageRequest(m.transcriptRequestForCurrentMode(), false, runtimeTranscriptSyncCauseDirtyFollowUp, cause)
}

func (m *uiModel) startRuntimeTranscriptPageRequest(request clientui.TranscriptPageRequest, allowDuplicateSkip bool, syncCause runtimeTranscriptSyncCause, recoveryCause clientui.TranscriptRecoveryCause) tea.Cmd {
	fetchRequest := m.transcriptHydrationRequest(request, allowDuplicateSkip)
	if m.runtimeTranscriptBusy {
		m.runtimeTranscriptDirty = true
		if recoveryCause != clientui.TranscriptRecoveryCauseNone {
			m.runtimeTranscriptDirtyRecoveryCause = recoveryCause
		}
		m.logf("ui.runtime.transcript.mark_dirty sync_cause=%s recovery_cause=%s", syncCause, recoveryCause)
		return nil
	}
	if allowDuplicateSkip && m.shouldSkipTranscriptPageRequest(request) {
		m.logf("ui.runtime.transcript.skip_duplicate mode=%s offset=%d limit=%d window=%s sync_cause=%s", m.view.Mode(), request.Offset, request.Limit, request.Window, syncCause)
		return nil
	}
	m.runtimeTranscriptBusy = true
	m.runtimeTranscriptDirty = false
	m.runtimeTranscriptDirtyRecoveryCause = clientui.TranscriptRecoveryCauseNone
	m.runtimeTranscriptToken++
	token := m.runtimeTranscriptToken
	client := m.runtimeClient()
	if client == nil {
		m.runtimeTranscriptBusy = false
		return nil
	}
	m.logf("ui.runtime.transcript.start token=%d sync_cause=%s", token, syncCause)
	fields := map[string]string{
		"session_id":            m.sessionID,
		"mode":                  string(m.view.Mode()),
		"path":                  "hydrate",
		"token":                 strconv.FormatUint(token, 10),
		"sync_cause":            string(syncCause),
		"recovery_cause":        string(recoveryCause),
		"current_revision":      strconv.FormatInt(m.transcriptRevision, 10),
		"transcript_live_dirty": strconv.FormatBool(m.transcriptLiveDirty),
		"reasoning_live_dirty":  strconv.FormatBool(m.reasoningLiveDirty),
	}
	for key, value := range transcriptdiag.RequestFields(fetchRequest) {
		fields[key] = value
	}
	m.logTranscriptDiag(transcriptdiag.FormatLine("transcript.diag.client.hydrate_start", fields))
	return func() tea.Msg {
		var (
			transcript clientui.TranscriptPage
			err        error
		)
		if allowDuplicateSkip {
			transcript, err = client.LoadTranscriptPage(fetchRequest)
		} else {
			transcript, err = client.RefreshTranscriptPage(fetchRequest)
		}
		return runtimeTranscriptRefreshedMsg{token: token, req: fetchRequest, syncCause: syncCause, transcript: transcript, recoveryCause: recoveryCause, err: err}
	}
}

func (m *uiModel) transcriptHydrationRequest(request clientui.TranscriptPageRequest, allowDuplicateSkip bool) clientui.TranscriptPageRequest {
	if m == nil || allowDuplicateSkip || request.Window != clientui.TranscriptWindowOngoingTail {
		return request
	}
	request.KnownRevision, request.KnownCommittedEntryCount = committedTranscriptStateIncludingDeferredTail(m)
	return request
}

func (m *uiModel) shouldSkipTranscriptPageRequest(req clientui.TranscriptPageRequest) bool {
	if m.runtimeTranscriptDirty || m.transcriptLiveDirty || m.reasoningLiveDirty {
		return false
	}
	if m.view.Mode() != tui.ModeDetail {
		return false
	}
	if !m.detailTranscript.loaded {
		return false
	}
	return pageRequestEqual(m.detailTranscript.lastRequest, req)
}

func (m *uiModel) scheduleRuntimeTranscriptRetry(syncCause runtimeTranscriptSyncCause, cause clientui.TranscriptRecoveryCause) tea.Cmd {
	if !m.hasRuntimeClient() {
		return nil
	}
	m.runtimeTranscriptRetry++
	token := m.runtimeTranscriptRetry
	m.logf("ui.runtime.transcript.retry_scheduled token=%d sync_cause=%s recovery_cause=%s delay=%s", token, syncCause, cause, uiRuntimeHydrationRetryDelay)
	return tea.Tick(uiRuntimeHydrationRetryDelay, func(time.Time) tea.Msg {
		return runtimeTranscriptRetryMsg{token: token, syncCause: syncCause, recoveryCause: cause}
	})
}

func (m *uiModel) handleRuntimeMainViewRefreshed(msg runtimeMainViewRefreshedMsg) tea.Cmd {
	if msg.token != m.runtimeMainViewToken {
		return nil
	}
	if msg.err != nil {
		m.observeRuntimeRequestResult(msg.err)
		m.logf("ui.runtime.main_view err=%q", msg.err.Error())
		return nil
	}
	m.observeRuntimeRequestResult(nil)
	m.applyRuntimeMainViewState(msg.view)
	return m.runtimeAdapter().applyProjectedSessionMetadata(msg.view.Session)
}

func (m *uiModel) handleRuntimeTranscriptRefreshed(msg runtimeTranscriptRefreshedMsg) tea.Cmd {
	if msg.token != m.runtimeTranscriptToken {
		return nil
	}
	m.runtimeTranscriptBusy = false
	if msg.err != nil {
		m.invalidateTransientTranscriptState()
		m.observeRuntimeRequestResult(msg.err)
		m.logf("ui.runtime.transcript err=%q sync_cause=%s recovery_cause=%s", msg.err.Error(), msg.syncCause, msg.recoveryCause)
		m.logTranscriptDiag(transcriptdiag.FormatLine("transcript.diag.client.hydrate_response", map[string]string{
			"session_id":     m.sessionID,
			"mode":           string(m.view.Mode()),
			"path":           "hydrate",
			"token":          strconv.FormatUint(msg.token, 10),
			"sync_cause":     string(msg.syncCause),
			"recovery_cause": string(msg.recoveryCause),
			"err":            msg.err.Error(),
		}))
		resumeCmd := tea.Cmd(nil)
		if m.waitRuntimeEventAfterHydration {
			m.waitRuntimeEventAfterHydration = false
			resumeCmd = m.waitRuntimeEventCmd()
		}
		return tea.Batch(m.scheduleRuntimeTranscriptRetry(msg.syncCause, msg.recoveryCause), resumeCmd)
	}
	m.observeRuntimeRequestResult(nil)
	m.logTranscriptPageDiag("transcript.diag.client.hydrate_response", msg.req, msg.transcript, map[string]string{
		"path":           "hydrate",
		"token":          strconv.FormatUint(msg.token, 10),
		"sync_cause":     string(msg.syncCause),
		"recovery_cause": string(msg.recoveryCause),
	})
	recovered := m.runtimeTranscriptRetry != 0
	if m.runtimeTranscriptRetry != 0 {
		m.runtimeTranscriptRetry++
	}
	if recovered {
		m.logf("ui.runtime.transcript.recovered token=%d", msg.token)
	}
	applyCmd := m.runtimeAdapter().applyRuntimeTranscriptPageWithRecovery(msg.req, msg.transcript, msg.recoveryCause)
	if !m.runtimeTranscriptDirty {
		if m.pendingQueuedDrainAfterHydration {
			m.queuedDrainReadyAfterHydration = true
		}
		resumeCmd := tea.Cmd(nil)
		if m.waitRuntimeEventAfterHydration {
			m.waitRuntimeEventAfterHydration = false
			resumeCmd = m.waitRuntimeEventCmd()
		}
		return sequenceCmds(applyCmd, m.flushQueuedInputsAfterHydration(), resumeCmd)
	}
	m.runtimeTranscriptDirty = false
	m.logf("ui.runtime.transcript.repeat_after_dirty token=%d sync_cause=%s", msg.token, runtimeTranscriptSyncCauseDirtyFollowUp)
	followUpRecoveryCause := m.runtimeTranscriptDirtyRecoveryCause
	return sequenceCmds(applyCmd, m.requestRuntimeDirtyFollowUpTranscriptSync(followUpRecoveryCause))
}

func (m *uiModel) handleRuntimeCommittedTranscriptSuffixRefreshed(msg runtimeCommittedTranscriptSuffixRefreshedMsg) tea.Cmd {
	if msg.token <= 0 {
		return nil
	}
	if suffixSessionChanged(m, msg.suffix) {
		return nil
	}
	if msg.err != nil {
		if msg.token < m.runtimeCommittedSuffixToken {
			return nil
		}
		m.observeRuntimeRequestResult(msg.err)
		m.logf("ui.runtime.committed_suffix err=%q after_entry_count=%d limit=%d", msg.err.Error(), msg.req.AfterEntryCount, msg.req.Limit)
		return m.requestRuntimeCommittedConversationSync()
	}
	m.observeRuntimeRequestResult(nil)
	if committedTranscriptSuffixStartsAfterDeliveryCursor(m, msg.suffix) {
		return m.requestRuntimeCommittedGapSync()
	}
	staleResponse := msg.token < m.runtimeCommittedSuffixToken
	trimmedSuffix := m.trimCommittedTranscriptSuffixToDeliveryCursor(msg.suffix)
	applyCmd := m.applyCommittedTranscriptSuffixAppend(msg.suffix)
	hasMore := msg.suffix.HasMore
	nextEntryCount := msg.suffix.NextEntryCount
	if staleResponse {
		hasMore = trimmedSuffix.HasMore
		nextEntryCount = trimmedSuffix.NextEntryCount
		if trimmedSuffix.NextEntryCount <= trimmedSuffix.StartEntryCount {
			return applyCmd
		}
	}
	if !hasMore {
		return applyCmd
	}
	nextReq := clientui.NormalizeCommittedTranscriptSuffixRequest(clientui.CommittedTranscriptSuffixRequest{
		AfterEntryCount: nextEntryCount,
		Limit:           msg.req.Limit,
	})
	return sequenceCmds(applyCmd, m.requestRuntimeCommittedTranscriptSuffix(nextReq))
}

func (m *uiModel) flushQueuedInputsAfterHydration() tea.Cmd {
	if m == nil || !m.pendingQueuedDrainAfterHydration {
		return nil
	}
	if !m.queuedDrainReadyAfterHydration {
		return nil
	}
	if m.isBusy() || m.isInputLocked() {
		if len(m.queued) == 0 || strings.TrimSpace(m.activeSubmit.text) != "" {
			m.pendingQueuedDrainAfterHydration = false
			m.queuedDrainReadyAfterHydration = false
		}
		return nil
	}
	m.pendingQueuedDrainAfterHydration = false
	m.queuedDrainReadyAfterHydration = false
	if len(m.queued) == 0 {
		m.inputController().notifyTurnQueueDrainedIfIdle()
		return nil
	}
	_, cmd := m.inputController().flushQueuedInputs(queueDrainAuto)
	m.inputController().notifyTurnQueueDrainedIfIdle()
	return cmd
}
