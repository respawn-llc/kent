package app

import (
	"errors"
	"fmt"
	"strconv"

	"core/cli/app/internal/runtimeattach"
	"core/cli/tui/ongoing"

	tea "github.com/charmbracelet/bubbletea"
)

const ongoingHistoryNoticeID = "ongoing-history"

func (m *uiModel) reduceOngoingMessage(msg tea.Msg) uiFeatureUpdateResult {
	switch msg := msg.(type) {
	case ongoingTranscriptEvent:
		cmd := m.handleOngoingTranscriptEvent(msg)
		return handledUIFeatureUpdate(m, cmd)
	case ongoingNormalBufferOwnedMsg:
		_ = msg
		return handledUIFeatureUpdate(m, m.reconcileOngoingOwnership())
	case ongoingOwnershipReconcileMsg:
		return handledUIFeatureUpdate(m, m.reconcileOngoingOwnership())
	case ongoingResizeDebounceMsg:
		if msg.token != m.ongoingRehydration.token || m.ongoingRehydration.pending == nil || m.ongoingRehydration.closed {
			return handledUIFeatureUpdate(m, nil)
		}
		m.ongoingRehydration.pending.due = true
		m.admitDueOngoingResize()
		return handledUIFeatureUpdate(m, nil)
	}
	return uiFeatureUpdateResult{}
}

func (m *uiModel) handleOngoingTranscriptEvent(event ongoingTranscriptEvent) tea.Cmd {
	if m != nil && event.Kind == ongoingTranscriptEventClosed {
		m.closeOngoingRehydration()
		return nil
	}
	if m == nil || m.ongoingTranscript == nil {
		return nil
	}
	var (
		result   ongoing.Result
		err      error
		recovery tea.Cmd
	)
	switch event.Kind {
	case ongoingTranscriptEventHistory:
		var noticeCmd tea.Cmd
		if m.transientStatusNoticeID == ongoingHistoryNoticeID {
			noticeCmd = m.advanceTransientStatusQueue()
		}
		if event.Err != nil {
			m.logf("ongoing.history.read.error err=%q", event.Err.Error())
			return tea.Batch(noticeCmd, m.batchWithNativeOngoingRepaint(m.sendTransientStatusWithNoticeID(
				fmt.Sprintf("Could not load conversation history: %v", event.Err),
				uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "",
			)))
		}
		result, err = m.ongoingTranscript.AcceptHistory(event.Page)
		if err != nil {
			return m.handleOngoingSurfaceError(err)
		}
		return tea.Batch(noticeCmd, m.handleOngoingResult(result))
	case ongoingTranscriptEventCutover:
		if event.Reason == nil {
			frame := m.ongoingFrameInput()
			return m.handleOngoingDeveloperError(ongoing.NewDeveloperError(
				"transcript_cutover", "transcript cutover requires reset reason", map[string]any{
					"terminal_size":  frame.Size,
					"quoted_payload": strconv.Quote(fmt.Sprintf("%+v", event)),
					"quoted_frame":   strconv.Quote(fmt.Sprintf("%+v", frame)),
				},
			))
		}
		if m.ongoingRehydration.reopen != nil {
			*m.ongoingRehydration.reopen = ongoingReopenHydrating
		}
		result, err = m.ongoingTranscript.AcceptCutover(*event.Reason)
	case ongoingTranscriptEventMessage:
		wasRuntimeDisconnected := m.runtimeDisconnectStatusVisible()
		var stateCmd tea.Cmd
		result, stateCmd, err = m.ongoingTranscript.Accept(event.Message)
		if err != nil {
			var developerErr ongoing.DeveloperError
			if errors.As(err, &developerErr) {
				return m.handleOngoingDeveloperError(developerErr)
			}
			return m.handleOngoingSurfaceError(err)
		}
		acceptedHydration := m.ongoingTranscript.acceptedHydration(event.Message)
		if acceptedHydration {
			m.observeRuntimeRequestResult(nil)
			m.finishOngoingRehydration()
		}
		m.layout().syncViewport()
		var connectionStatusCmd tea.Cmd
		if wasRuntimeDisconnected && acceptedHydration {
			connectionStatusCmd = m.renderNativeOngoingSurface()
		}
		return tea.Batch(stateCmd, m.handleOngoingResult(result), m.reconcileSpinnerTicking(true), connectionStatusCmd)
	case ongoingTranscriptEventLoss:
		m.ongoingRehydration.reopen = nil
		if runtimeattach.IsRuntimeConnectionError(event.Err) {
			m.observeRuntimeRequestResult(event.Err)
			recovery = m.startRuntimeMainViewRefresh()
		}
		result = m.ongoingTranscript.HandleSubscriptionLoss()
	case ongoingTranscriptEventFailure:
		if runtimeattach.IsRuntimeProtocolMismatch(event.Err) || !runtimeattach.IsRuntimeConnectionError(event.Err) {
			m.closeOngoingRehydration()
		}
		err = fmt.Errorf("open transcript subscription: %w", event.Err)
		m.logf("ongoing.transcript.open.error err=%q", err.Error())
		if runtimeattach.IsRuntimeProtocolMismatch(event.Err) {
			m.observeRuntimeRequestResult(event.Err)
			notice := m.sendTransientStatusWithNoticeID(event.Err.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
			m.layout().syncViewport()
			return m.batchWithNativeOngoingRepaint(notice)
		}
		if runtimeattach.IsRuntimeConnectionError(event.Err) {
			m.observeRuntimeRequestResult(event.Err)
			m.layout().syncViewport()
			return tea.Batch(m.renderNativeOngoingSurface(), m.startRuntimeMainViewRefresh())
		}
		return m.handleFatalUIError(fmt.Sprintf("ongoing transcript failed: %v", err), err)
	default:
		if m.debugMode {
			panic("unknown ongoing transcript event kind")
		}
		return nil
	}
	if err != nil {
		return m.handleOngoingSurfaceError(err)
	}
	return tea.Batch(m.handleOngoingResult(result), recovery)
}
