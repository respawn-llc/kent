package app

import (
	"builder/cli/app/internal/worktreecreateresolve"

	tea "github.com/charmbracelet/bubbletea"
)

type uiWorktreeFeatureReducer struct {
	model *uiModel
}

func (m *uiModel) worktreeReducer() uiWorktreeFeatureReducer {
	return uiWorktreeFeatureReducer{model: m}
}

func (r uiWorktreeFeatureReducer) Update(msg tea.Msg) uiFeatureUpdateResult {
	m := r.model
	switch msg := msg.(type) {
	case worktreeListDoneMsg:
		if !m.worktrees.isOpen() || msg.token != m.worktrees.refreshToken {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.worktrees.loading = false
		if msg.err != nil {
			m.worktrees.errorText = formatSubmissionError(msg.err)
			m.syncViewport()
			return handledUIFeatureUpdate(m, m.ensureSpinnerTicking())
		}
		m.worktrees.errorText = ""
		m.applyWorktreeListResponse(msg.resp)
		cmd := m.applyWorktreeIntent()
		m.syncViewport()
		return handledUIFeatureUpdate(m, tea.Batch(cmd, m.ensureSpinnerTicking()))
	case worktreeCreateDoneMsg:
		if msg.token != m.worktrees.mutationToken {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.worktrees.create.submitting = false
		if msg.err != nil {
			if !m.worktrees.isOpen() {
				status := formatSubmissionError(msg.err)
				m.syncViewport()
				return handledUIFeatureUpdate(m, m.setTransientStatusWithKind(status, uiStatusNoticeError))
			}
			m.worktrees.create.errorText = formatSubmissionError(msg.err)
			m.syncViewport()
			return handledUIFeatureUpdate(m, m.ensureSpinnerTicking())
		}
		var overlayCmd tea.Cmd
		if m.worktrees.isOpen() {
			overlayCmd = m.popWorktreeOverlayIfNeeded()
			m.closeWorktreeOverlay()
		}
		status := "Created worktree " + worktreeDisplayName(msg.resp.Worktree)
		if msg.resp.SetupScheduled {
			status += " and started setup"
		}
		feedbackCmd := m.setTransientStatusWithKind(status, uiStatusNoticeSuccess)
		m.syncViewport()
		return handledUIFeatureUpdate(m, tea.Batch(overlayCmd, feedbackCmd, m.requestRuntimeMainViewRefresh(), m.ensureSpinnerTicking()))
	case worktreeSwitchDoneMsg:
		if msg.token != m.worktrees.mutationToken {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.worktrees.switchPending = false
		if msg.err != nil {
			if !m.worktrees.isOpen() {
				status := formatSubmissionError(msg.err)
				m.syncViewport()
				return handledUIFeatureUpdate(m, m.setTransientStatusWithKind(status, uiStatusNoticeError))
			}
			m.worktrees.errorText = formatSubmissionError(msg.err)
			m.syncViewport()
			return handledUIFeatureUpdate(m, m.ensureSpinnerTicking())
		}
		var overlayCmd tea.Cmd
		if m.worktrees.isOpen() {
			overlayCmd = m.popWorktreeOverlayIfNeeded()
			m.closeWorktreeOverlay()
		}
		status := "Switched to " + worktreeDisplayName(msg.resp.Worktree)
		feedbackCmd := m.setTransientStatusWithKind(status, uiStatusNoticeSuccess)
		m.syncViewport()
		return handledUIFeatureUpdate(m, tea.Batch(overlayCmd, feedbackCmd, m.requestRuntimeMainViewRefresh(), m.ensureSpinnerTicking()))
	case worktreeDeleteDoneMsg:
		if msg.token != m.worktrees.mutationToken {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.worktrees.deleteConfirm.submitting = false
		if msg.err != nil {
			if !m.worktrees.isOpen() {
				status := formatSubmissionError(msg.err)
				m.syncViewport()
				return handledUIFeatureUpdate(m, m.setTransientStatusWithKind(status, uiStatusNoticeError))
			}
			m.worktrees.deleteConfirm.errorText = formatSubmissionError(msg.err)
			m.syncViewport()
			return handledUIFeatureUpdate(m, m.ensureSpinnerTicking())
		}
		var listCmd tea.Cmd
		if m.worktrees.isOpen() {
			m.closeWorktreeDialog()
			m.worktrees.selectedID = worktreeCreateRowID
			listCmd = m.requestWorktreeListCmd()
		}
		feedbackCmd := m.setTransientStatusWithKind(worktreeDeleteSuccessStatus(msg.resp), uiStatusNoticeSuccess)
		m.syncViewport()
		return handledUIFeatureUpdate(m, tea.Batch(feedbackCmd, listCmd, m.requestRuntimeMainViewRefresh(), m.ensureSpinnerTicking()))
	case worktreeCreateTargetResolveDebounceMsg:
		if !m.worktrees.isOpen() || m.worktrees.phase != uiWorktreeOverlayPhaseCreate {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		state, outcome := worktreecreateresolve.DebounceReady(m.worktrees.create.resolveState(), msg.token, singleLineEditorValue(m.worktrees.create.branchTarget))
		m.worktrees.create.applyResolveState(state)
		if outcome.Ignored || !outcome.Start {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.syncViewport()
		return handledUIFeatureUpdate(m, m.worktreeCreateTargetResolveCmd(outcome.Query, outcome.Token))
	case worktreeCreateTargetResolveDoneMsg:
		if !m.worktrees.isOpen() || m.worktrees.phase != uiWorktreeOverlayPhaseCreate {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		errorText := ""
		if msg.err != nil {
			errorText = formatSubmissionError(msg.err)
		}
		state, outcome := worktreecreateresolve.Done(m.worktrees.create.resolveState(), worktreecreateresolve.DoneInput{
			Token:         msg.token,
			CurrentQuery:  singleLineEditorValue(m.worktrees.create.branchTarget),
			ResponseQuery: msg.query,
			Resolution:    msg.resp.Resolution,
			HasError:      msg.err != nil,
			ErrorText:     errorText,
		})
		m.worktrees.create.applyResolveState(state)
		m.syncViewport()
		if outcome.Submit {
			req, err := m.worktrees.create.request(outcome.SubmitKind)
			if err != nil {
				m.worktrees.create.errorText = err.Error()
				m.syncViewport()
				return handledUIFeatureUpdate(m, nil)
			}
			return handledUIFeatureUpdate(m, m.worktreeCreateCmd(req))
		}
		return handledUIFeatureUpdate(m, nil)
	}
	return uiFeatureUpdateResult{}
}
