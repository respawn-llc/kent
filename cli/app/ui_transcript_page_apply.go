package app

import (
	"strconv"
	"strings"

	appstatus "core/cli/app/internal/status"
	"core/cli/tui"
	"core/shared/clientui"

	tea "github.com/charmbracelet/bubbletea"
)

func (a uiRuntimeAdapter) applyProjectedChatSnapshot(snapshot clientui.ChatSnapshot) tea.Cmd {
	page := a.model.runtimeTranscript()
	page.Entries = cloneTranscriptEntries(snapshot.Entries)
	page.TotalEntries = len(page.Entries)
	page.Offset = 0
	page.NextOffset = 0
	page.HasMore = false
	page.Ongoing = snapshot.Ongoing
	page.OngoingError = snapshot.OngoingError
	return a.applyRuntimeTranscriptPageWithRecovery(clientui.TranscriptPageRequest{}, page, clientui.TranscriptRecoveryCauseNone)
}

func (a uiRuntimeAdapter) applyProjectedSessionMetadata(view clientui.RuntimeSessionView) tea.Cmd {
	m := a.model
	if len(m.startupCmds) > 0 {
		m.startupCmds = nil
	}
	previousWindowTitle := sessionTitle(m.sessionName)

	if transcriptPageSessionChanged(m.sessionID, view.SessionID) {
		m.detailTranscript.reset()
		m.transcriptRevision = 0
		m.transcriptLiveDirty = false
		m.reasoningLiveDirty = false
		m.clearDeferredCommittedTail("session_switch")
		m.nativeHistoryReplayPermit = nativeHistoryReplayPermitNone
	}
	m.sessionID = strings.TrimSpace(view.SessionID)
	m.sessionName = strings.TrimSpace(view.SessionName)
	m.conversationFreshness = view.ConversationFreshness
	targetCmd := a.applyProjectedExecutionTarget(view.ExecutionTarget)
	if view.Transcript.Revision > m.transcriptRevision {
		m.transcriptRevision = view.Transcript.Revision
	}
	titleCmd := tea.Cmd(nil)
	if previousWindowTitle != sessionTitle(m.sessionName) {
		titleCmd = tea.SetWindowTitle(sessionTitle(m.sessionName))
	}
	return sequenceCmds(titleCmd, targetCmd)
}

func (a uiRuntimeAdapter) applyProjectedExecutionTarget(target clientui.SessionExecutionTarget) tea.Cmd {
	m := a.model
	if m == nil {
		return nil
	}
	workdir := strings.TrimSpace(target.EffectiveWorkdir)
	if workdir == "" {
		workdir = strings.TrimSpace(target.WorkspaceRoot)
	}
	previousWorkdir := strings.TrimSpace(m.statusConfig.WorkspaceRoot)
	previousTarget := m.statusConfig.ExecutionTarget
	if workdir == "" {
		if !clientui.SessionExecutionTargetsEqual(previousTarget, target) {
			m.statusConfig.ExecutionTarget = target
		}
		return nil
	}
	targetChanged := !clientui.SessionExecutionTargetsEqual(previousTarget, target)
	workdirChanged := previousWorkdir != workdir
	m.statusConfig.WorkspaceRoot = workdir
	m.statusConfig.ExecutionTarget = target
	if !workdirChanged && !targetChanged {
		return nil
	}
	if workdirChanged {
		m.statusRepository = appstatus.NewMemoryRepository()
		m.clearPathReferenceState()
	}
	if workdirChanged && m.pathReferenceSearch != nil {
		m.pathReferenceSearch.StartPrewarm(workdir)
	}
	return m.statusLineGitRefreshCmd()
}

func (a uiRuntimeAdapter) applyRuntimeTranscriptPageWithRecovery(req clientui.TranscriptPageRequest, page clientui.TranscriptPage, recoveryCause clientui.TranscriptRecoveryCause) tea.Cmd {
	m := a.model
	m.logTranscriptPageDiag("transcript.diag.client.apply_page_start", req, page, map[string]string{"path": "hydrate", "recovery_cause": string(recoveryCause)})
	if len(m.startupCmds) > 0 {
		m.startupCmds = nil
	}
	previousWindowTitle := sessionTitle(m.sessionName)

	if transcriptPageSessionChanged(m.sessionID, page.SessionID) {
		m.detailTranscript.reset()
		m.transcriptRevision = 0
		m.transcriptLiveDirty = false
		m.reasoningLiveDirty = false
		m.clearDeferredCommittedTail("session_switch")
		m.nativeHistoryReplayPermit = nativeHistoryReplayPermitNone
	}
	m.sessionID = strings.TrimSpace(page.SessionID)
	if strings.TrimSpace(page.SessionName) != "" {
		m.sessionName = strings.TrimSpace(page.SessionName)
	}
	m.conversationFreshness = page.ConversationFreshness
	reduction := reduceRuntimeTranscriptPage(newRuntimeTranscriptPageState(runtimeTranscriptPageSnapshotFromModel(m)), req, page, recoveryCause)
	pageReq := reduction.request
	page = reduction.page
	entries := reduction.entries
	if reduction.decision == runtimeTranscriptPageDecisionReject {
		m.logTranscriptPageDiag("transcript.diag.client.apply_page_reject", pageReq, page, map[string]string{
			"path":                    "hydrate",
			"reason":                  reduction.rejectReason,
			"recovery_cause":          string(recoveryCause),
			"replacement_branch":      reduction.branch,
			"preserve_live_reasoning": strconv.FormatBool(reduction.preserveLiveReasoning),
		})
		if previousWindowTitle != sessionTitle(m.sessionName) {
			return tea.SetWindowTitle(sessionTitle(m.sessionName))
		}
		return nil
	}
	if reduction.shouldSyncNativeHistory {
		m.armNativeHistoryReplayPermit(reduction.nativeReplayPermit)
		m.clearDeferredCommittedTail("authoritative_hydrate")
		a.applyAuthoritativeOngoingTailPage(page, entries, reduction.preserveLiveReasoning)
	}
	if pageReq.Window == clientui.TranscriptWindowOngoingTail || (pageReq == (clientui.TranscriptPageRequest{}) && m.view.Mode() != tui.ModeDetail) {
		m.detailTranscript.syncTail(page)
		if m.view.Mode() != tui.ModeDetail {
			if !reduction.preserveLiveReasoning {
				m.forwardToView(tui.ClearStreamingReasoningMsg{})
			}
			m.forwardToView(tui.SetConversationMsg{
				BaseOffset:   page.Offset,
				TotalEntries: page.TotalEntries,
				Entries:      entries,
				Ongoing:      page.Ongoing,
				OngoingError: page.OngoingError,
			})
		}
	} else {
		if m.view.Mode() == tui.ModeDetail && m.detailTranscript.matchesPage(page) {
			m.transcriptRevision = max(m.transcriptRevision, page.Revision)
			if previousWindowTitle != sessionTitle(m.sessionName) {
				return tea.SetWindowTitle(sessionTitle(m.sessionName))
			}
			return nil
		}
		m.detailTranscript.apply(page)
		m.transcriptRevision = max(m.transcriptRevision, page.Revision)
		if !reduction.preserveLiveReasoning {
			m.reasoningLiveDirty = false
		}
		detailPage := m.detailTranscript.page()
		detailPage.SessionID = page.SessionID
		detailPage.SessionName = page.SessionName
		detailPage.ConversationFreshness = page.ConversationFreshness
		detailPage.Revision = page.Revision
		if m.view.Mode() == tui.ModeDetail {
			if !reduction.preserveLiveReasoning {
				m.forwardToView(tui.ClearStreamingReasoningMsg{})
			}
			m.forwardToView(tui.SetConversationMsg{
				BaseOffset:   detailPage.Offset,
				TotalEntries: detailPage.TotalEntries,
				Entries:      transcriptEntriesFromPage(detailPage),
				Ongoing:      detailPage.Ongoing,
				OngoingError: detailPage.OngoingError,
			})
			m.refreshRollbackCandidates()
		}
	}
	if m.view.Mode() == tui.ModeOngoing {
		m.forwardToView(tui.SetOngoingScrollMsg{Scroll: m.view.OngoingScroll()})
	}
	if strings.TrimSpace(page.Ongoing) == "" {
		m.sawAssistantDelta = false
	}
	cmds := make([]tea.Cmd, 0, 2)
	if reduction.shouldSyncNativeHistory {
		cmds = append(cmds, m.syncNativeHistoryFromTranscript())
	}
	m.logTranscriptPageDiag("transcript.diag.client.apply_page_commit", pageReq, page, map[string]string{
		"path":                      "hydrate",
		"recovery_cause":            string(recoveryCause),
		"branch":                    reduction.branch,
		"preserve_live_reasoning":   strconv.FormatBool(reduction.preserveLiveReasoning),
		"transcript_revision_after": strconv.FormatInt(m.transcriptRevision, 10),
		"transcript_total_after":    strconv.Itoa(m.transcriptTotalEntries),
		"native_history_sync":       strconv.FormatBool(reduction.shouldSyncNativeHistory),
	})
	if previousWindowTitle != sessionTitle(m.sessionName) {
		cmds = append(cmds, tea.SetWindowTitle(sessionTitle(m.sessionName)))
	}
	return sequenceCmds(cmds...)
}

func (a uiRuntimeAdapter) applyAuthoritativeOngoingTailPage(page clientui.TranscriptPage, entries []tui.TranscriptEntry, preserveLiveReasoning bool) {
	m := a.model
	if m == nil {
		return
	}
	m.transcriptBaseOffset = page.Offset
	m.transcriptEntries = append(m.transcriptEntries[:0], entries...)
	m.transcriptTotalEntries = max(page.TotalEntries, page.Offset+len(entries))
	m.transcriptRevision = max(m.transcriptRevision, page.Revision)
	m.ongoingCommittedDelivery = newOngoingCommittedDeliveryCursor(page.Offset+len(committedTranscriptEntriesForApp(entries)), m.transcriptRevision)
	m.transcriptLiveDirty = false
	if !preserveLiveReasoning {
		m.reasoningLiveDirty = false
	}
	m.refreshRollbackCandidates()
}
