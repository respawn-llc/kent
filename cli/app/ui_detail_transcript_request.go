package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"core/shared/protoapi"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/runtimeids"

	tea "github.com/charmbracelet/bubbletea"
)

type uiPendingDetailTranscriptRequest struct {
	sessionID runtimeids.SessionID
	request   *transcriptpb.PageRequest
	// Session replacement may warm this request outside Detail Mode.
	detailMode bool
	cancel     context.CancelFunc
}

func (m *uiModel) loadDetailTranscriptPageCmd(req *transcriptpb.PageRequest) tea.Cmd {
	return m.loadDetailTranscriptPageWithOptionsCmd(req, uiDetailTranscriptLoadOptions{
		noticePolicy: uiDetailTranscriptLoadingNoticeVisible,
	})
}

type uiDetailTranscriptLoadingNoticePolicy uint8

const (
	uiDetailTranscriptLoadingNoticeVisible uiDetailTranscriptLoadingNoticePolicy = iota
	uiDetailTranscriptLoadingNoticeSilent
)

type uiDetailTranscriptLoadOptions struct {
	noticePolicy uiDetailTranscriptLoadingNoticePolicy
	deadline     *time.Time
}

func (m *uiModel) loadDetailTranscriptPageWithNoticePolicyCmd(
	req *transcriptpb.PageRequest,
	noticePolicy uiDetailTranscriptLoadingNoticePolicy,
) tea.Cmd {
	return m.loadDetailTranscriptPageWithOptionsCmd(req, uiDetailTranscriptLoadOptions{
		noticePolicy: noticePolicy,
	})
}

func (m *uiModel) loadDetailTranscriptPageWithOptionsCmd(
	req *transcriptpb.PageRequest,
	options uiDetailTranscriptLoadOptions,
) tea.Cmd {
	if m == nil || m.pendingDetailTranscript != nil {
		return nil
	}
	sessionID, err := runtimeids.ParseSessionID(m.currentRuntimeSessionID())
	if err != nil {
		return m.sendTransientStatusWithNoticeID(
			err.Error(),
			uiStatusNoticeError,
			transientStatusDuration,
			uiStatusNoticeReplace,
			"",
		)
	}
	request := cloneTranscriptPageRequest(req)
	request.SessionId = protoapi.SessionSelectorToProto(sessionID)
	parentCtx, cancel := context.WithCancel(context.Background())
	pending := &uiPendingDetailTranscriptRequest{
		sessionID:  sessionID,
		request:    request,
		detailMode: m.surface() == uiSurfaceTranscriptDetail,
		cancel:     cancel,
	}
	m.pendingDetailTranscript = pending
	if options.noticePolicy == uiDetailTranscriptLoadingNoticeVisible &&
		request.Direction != nil {
		m.showDetailTranscriptLoadingNotice(pending)
	}
	var deadline *time.Time
	if options.deadline != nil {
		deadlineCopy := *options.deadline
		deadline = &deadlineCopy
	}
	client := m.statusConfig.SessionViews
	return func() tea.Msg {
		if client == nil {
			return detailTranscriptLoadMsg{
				pending: pending,
				err:     errors.New("session view client is required"),
			}
		}
		var (
			ctx           context.Context
			timeoutCancel context.CancelFunc
		)
		if deadline == nil {
			ctx, timeoutCancel = context.WithTimeout(parentCtx, uiRuntimeHydrationReadTimeout)
		} else {
			ctx, timeoutCancel = context.WithDeadline(parentCtx, *deadline)
		}
		defer timeoutCancel()
		resp, err := client.GetSessionTranscriptPage(ctx, request)
		var page *transcriptpb.Page
		if err == nil {
			page = resp.Transcript
		}
		if deadline != nil && errors.Is(err, context.DeadlineExceeded) {
			err = fmt.Errorf("%w: %w", errRollbackNavigationTimedOut, err)
		}
		return detailTranscriptLoadMsg{pending: pending, page: page, err: err}
	}
}

func validateDetailTranscriptPageResponse(
	requestSessionID runtimeids.SessionID,
	page *transcriptpb.Page,
) error {
	responseSessionID := runtimeids.SessionIDFromProto(page.SessionId)
	if responseSessionID != requestSessionID {
		return fmt.Errorf(
			"transcript page session %s does not match requested session %s",
			responseSessionID.String(),
			requestSessionID.String(),
		)
	}
	return nil
}

func (m *uiModel) takePendingDetailTranscriptRequest(request *uiPendingDetailTranscriptRequest) (uiPendingDetailTranscriptRequest, bool) {
	if m == nil || m.pendingDetailTranscript == nil || m.pendingDetailTranscript != request {
		return uiPendingDetailTranscriptRequest{}, false
	}
	pending := *m.pendingDetailTranscript
	m.pendingDetailTranscript = nil
	pending.cancel()
	return pending, true
}

func (m *uiModel) cancelPendingDetailTranscriptRequest() tea.Cmd {
	if m == nil || m.pendingDetailTranscript == nil {
		return nil
	}
	pending := m.pendingDetailTranscript
	m.pendingDetailTranscript = nil
	pending.cancel()
	return m.clearDetailTranscriptLoadingNotice(pending)
}

func (m *uiModel) showDetailTranscriptLoadingNotice(pending *uiPendingDetailTranscriptRequest) {
	_ = m.showTransientStatusNotice(uiStatusNotice{
		Text:          "Loading transcript history",
		Kind:          uiStatusNoticeInfo,
		DetailRequest: pending,
	})
}

func (m *uiModel) clearDetailTranscriptLoadingNotice(pending *uiPendingDetailTranscriptRequest) tea.Cmd {
	if m == nil || m.transientStatusDetailRequest == nil || m.transientStatusDetailRequest != pending {
		return nil
	}
	return m.advanceTransientStatusQueue()
}
