package app

import (
	"core/cli/app/internal/submissionerror"
	"core/cli/app/internal/worktreecreate"
	"core/cli/app/internal/worktreecreateresolve"
	"core/cli/app/internal/worktreeview"

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
		if !m.worktrees.open || msg.token != m.worktrees.refreshToken {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.worktrees.loading = false
		if msg.err != nil {
			m.worktrees.errorText = submissionerror.Format(msg.err)
			m.syncViewport()
			return handledUIFeatureUpdate(m, m.reconcileSpinnerTicking(false))
		}
		m.worktrees.errorText = ""
		m.applyWorktreeListResponse(msg.resp)
		cmd := m.applyWorktreeIntent()
		m.syncViewport()
		return handledUIFeatureUpdate(m, tea.Batch(cmd, m.reconcileSpinnerTicking(false)))
	case worktreeCreateDoneMsg:
		if msg.token != m.worktrees.mutationToken {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.worktrees.create.submitting = false
		if msg.err != nil {
			if !m.worktrees.open {
				status := submissionerror.Format(msg.err)
				m.syncViewport()
				return handledUIFeatureUpdate(m, m.sendTransientStatusWithNoticeID(status, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, ""))
			}
			m.worktrees.create.errorText = submissionerror.Format(msg.err)
			m.syncViewport()
			return handledUIFeatureUpdate(m, m.reconcileSpinnerTicking(false))
		}
		var overlayCmd tea.Cmd
		if m.worktrees.open {
			overlayCmd = m.restoreTranscriptSurface()
			m.closeWorktreeOverlay()
		}
		status := "Created worktree " + worktreeview.DisplayName(msg.resp.Worktree)
		if msg.resp.SetupScheduled {
			status += " and started setup"
		}
		feedbackCmd := m.sendTransientStatusWithNoticeID(status, uiStatusNoticeSuccess, transientStatusDuration, uiStatusNoticeReplace, "")
		m.syncViewport()
		return handledUIFeatureUpdate(m, tea.Batch(overlayCmd, feedbackCmd, m.startRuntimeMainViewRefreshRequest(runtimeMainViewRefreshRequestForCause(runtimeMainViewRefreshCauseWorktreeMutation)).cmd, m.reconcileSpinnerTicking(false)))
	case worktreeSwitchDoneMsg:
		if msg.token != m.worktrees.switchToken {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.worktrees.switchPending = false
		followUp := tea.Cmd(nil)
		if msg.err != nil {
			followUp = m.takeQueuedWorktreeSwitchCmd()
			if !m.worktrees.open {
				status := submissionerror.Format(msg.err)
				m.syncViewport()
				return handledUIFeatureUpdate(m, tea.Batch(m.sendTransientStatusWithNoticeID(status, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, ""), followUp))
			}
			m.worktrees.errorText = submissionerror.Format(msg.err)
			m.syncViewport()
			return handledUIFeatureUpdate(m, tea.Batch(followUp, m.reconcileSpinnerTicking(false)))
		}
		var overlayCmd tea.Cmd
		if m.worktrees.open {
			overlayCmd = m.restoreTranscriptSurface()
			m.closeWorktreeOverlay()
		}
		status := "Switched to " + worktreeview.DisplayName(msg.resp.Worktree)
		feedbackCmd := m.sendTransientStatusWithNoticeID(status, uiStatusNoticeSuccess, transientStatusDuration, uiStatusNoticeReplace, "")
		followUp = m.takeQueuedWorktreeSwitchCmd()
		m.syncViewport()
		return handledUIFeatureUpdate(m, tea.Batch(overlayCmd, feedbackCmd, m.startRuntimeMainViewRefreshRequest(runtimeMainViewRefreshRequestForCause(runtimeMainViewRefreshCauseWorktreeMutation)).cmd, followUp, m.reconcileSpinnerTicking(false)))
	case worktreeDeleteDoneMsg:
		if msg.token != m.worktrees.mutationToken {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.worktrees.deleteConfirm.submitting = false
		if msg.err != nil {
			if !m.worktrees.open {
				status := submissionerror.Format(msg.err)
				m.syncViewport()
				return handledUIFeatureUpdate(m, m.sendTransientStatusWithNoticeID(status, uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, ""))
			}
			m.worktrees.deleteConfirm.errorText = submissionerror.Format(msg.err)
			m.syncViewport()
			return handledUIFeatureUpdate(m, m.reconcileSpinnerTicking(false))
		}
		var listCmd tea.Cmd
		if m.worktrees.open {
			m.closeWorktreeDialog()
			m.worktrees.selectedID = worktreeCreateRowID
			listCmd = m.requestWorktreeListCmd()
		}
		feedbackCmd := m.sendTransientStatusWithNoticeID(worktreeDeleteSuccessStatus(msg.resp), uiStatusNoticeSuccess, transientStatusDuration, uiStatusNoticeReplace, "")
		m.syncViewport()
		return handledUIFeatureUpdate(m, tea.Batch(feedbackCmd, listCmd, m.startRuntimeMainViewRefreshRequest(runtimeMainViewRefreshRequestForCause(runtimeMainViewRefreshCauseWorktreeMutation)).cmd, m.reconcileSpinnerTicking(false)))
	case worktreeCreateTargetResolveDebounceMsg:
		if !m.worktrees.open || m.worktrees.phase != uiWorktreeOverlayPhaseCreate {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		state, outcome := worktreecreateresolve.DebounceReady(m.worktrees.create.resolveState(), msg.token, m.worktrees.create.branchTarget.Text())
		m.worktrees.create.applyResolveState(state)
		if outcome.Ignored || !outcome.Start {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		m.syncViewport()
		return handledUIFeatureUpdate(m, m.worktreeCreateTargetResolveCmd(outcome.Query, outcome.Token))
	case worktreeCreateTargetResolveDoneMsg:
		if !m.worktrees.open || m.worktrees.phase != uiWorktreeOverlayPhaseCreate {
			m.syncViewport()
			return handledUIFeatureUpdate(m, nil)
		}
		errorText := ""
		if msg.err != nil {
			errorText = submissionerror.Format(msg.err)
		}
		state, outcome := worktreecreateresolve.Done(m.worktrees.create.resolveState(), worktreecreateresolve.DoneInput{
			Token:         msg.token,
			CurrentQuery:  m.worktrees.create.branchTarget.Text(),
			ResponseQuery: msg.query,
			Resolution:    msg.resp.Resolution,
			HasError:      msg.err != nil,
			ErrorText:     errorText,
		})
		m.worktrees.create.applyResolveState(state)
		m.syncViewport()
		if outcome.Submit {
			req, err := worktreecreate.Request(m.worktrees.create.branchTarget.Text(), m.worktrees.create.baseRef.Text(), outcome.SubmitKind)
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
