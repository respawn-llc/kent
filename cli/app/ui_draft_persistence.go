package app

import (
	"context"

	"core/shared/apicontract"
	sessionlaunchpb "core/shared/protoapi/gen/kent/api/session_launch"

	tea "github.com/charmbracelet/bubbletea"
)

type draftContent struct {
	sessionID string
	input     string
}

type uiDraftPersistence struct {
	client      apicontract.SessionLifecycleService
	lastAttempt draftContent
	saving      bool
}

type draftSaveDoneMsg struct{ err error }

func newUIDraftPersistence(client apicontract.SessionLifecycleService, sessionID, input string) *uiDraftPersistence {
	if client == nil {
		return nil
	}
	return &uiDraftPersistence{client: client, lastAttempt: draftContent{sessionID: sessionID, input: input}}
}

func (m *uiModel) saveEditedDraft() tea.Cmd {
	owner := m.drafts
	if owner == nil || owner.saving {
		return nil
	}
	current := draftContent{sessionID: m.sessionID, input: m.mainEditor.Text()}
	if current == owner.lastAttempt {
		return nil
	}
	owner.lastAttempt = current
	owner.saving = true
	client := owner.client
	return func() tea.Msg {
		_, err := client.PersistInputDraft(context.Background(), &sessionlaunchpb.SessionPersistInputDraftRequest{
			SessionId: current.sessionID,
			Input:     current.input,
		})
		return draftSaveDoneMsg{err: err}
	}
}

func (m *uiModel) finishDraftSave(msg draftSaveDoneMsg) tea.Cmd {
	m.drafts.saving = false
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
