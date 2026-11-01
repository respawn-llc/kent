package app

import (
	"context"

	"core/shared/apicontract"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"
	"core/shared/runtimeids"

	tea "github.com/charmbracelet/bubbletea"
)

type draftContent struct {
	sessionID runtimeids.SessionID
	input     string
}

type uiDraftPersistence struct {
	client      apicontract.SessionLifecycleService
	lastAttempt *draftContent
	saving      bool
}

type draftSaveDoneMsg struct {
	owner *uiDraftPersistence
	err   error
}

func newUIDraftPersistence(client apicontract.SessionLifecycleService, initial *draftContent) *uiDraftPersistence {
	if client == nil {
		return nil
	}
	return &uiDraftPersistence{client: client, lastAttempt: initial}
}

func (m *uiModel) saveEditedDraft() tea.Cmd {
	owner := m.drafts
	if owner == nil || owner.saving {
		return nil
	}
	target, known := m.presentationTarget.(uiKnownSessionTarget)
	if !known {
		return nil
	}
	current := draftContent{sessionID: target.id, input: m.mainEditor.Text()}
	if owner.lastAttempt != nil && current == *owner.lastAttempt {
		return nil
	}
	owner.lastAttempt = &current
	owner.saving = true
	client := owner.client
	return func() tea.Msg {
		_, err := client.PersistInputDraft(context.Background(), &sessionlaunchpb.SessionPersistInputDraftRequest{
			SessionId: current.sessionID.String(),
			Input:     current.input,
		})
		return draftSaveDoneMsg{owner: owner, err: err}
	}
}

func (m *uiModel) finishDraftSave(msg draftSaveDoneMsg) tea.Cmd {
	msg.owner.saving = false
	if msg.err == nil {
		return m.renderNativeOngoingSurface()
	}
	m.logf("draft.save.error err=%q", msg.err.Error())
	return m.batchWithNativeOngoingRepaint(m.sendTransientStatusWithNoticeID(
		"Could not save draft: "+msg.err.Error(),
		uiStatusNoticeError,
		transientStatusDuration,
		uiStatusNoticeReplace,
		"",
	))
}
