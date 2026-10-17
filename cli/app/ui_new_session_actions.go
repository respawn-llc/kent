package app

import (
	"context"

	chatpb "core/shared/protoapi/gen/kent/api/chat"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"
	"core/shared/textutil"

	tea "github.com/charmbracelet/bubbletea"
	"google.golang.org/protobuf/proto"
)

type newSessionActionDoneMsg struct {
	origin        *uiModel
	token         uint64
	action        chatAction
	submittedText *string
	session       *chatpb.ExistingSessionTarget
	err           error
	diagnostic    error
}

func (m *uiModel) newSessionInputCmd(text string, input runtimeinput.Input, intent newSessionInputIntent) tea.Cmd {
	if command, shell := parseUserShellCommand(text); shell {
		return m.newSessionActionCmd(chatShellAction{command: command}, &text)
	}
	activation, err := chatActivationFromInput(input)
	if err != nil {
		return m.sendTransientStatusWithNoticeID(err.Error(), uiStatusNoticeError, transientStatusDuration, uiStatusNoticeReplace, "")
	}
	return m.newSessionActionCmd(chatInputAction{input: activation, intent: intent, queueItemID: runtimeids.NewQueueItemID()}, &text)
}

func (m *uiModel) newSessionActionCmd(action chatAction, submittedText *string) tea.Cmd {
	if m.newSessionTarget() != nil && !m.newSessionConnectionReady() {
		if submittedText != nil {
			m.inputController().restoreSubmittedTextIntoInput(*submittedText)
		}
		return nil
	}
	var target *chatpb.ChatTarget
	switch presentation := m.presentationTarget.(type) {
	case *uiNewSessionTarget:
		target = &chatpb.ChatTarget{Target: &chatpb.ChatTarget_NewChat{
			NewChat: proto.Clone(presentation.creation).(*chatpb.NewChatTarget),
		}}
	case uiKnownSessionTarget:
		target = m.knownChatTarget()
	default:
		panic("Chat action requires a presentation target")
	}
	service := m.chatMutations
	draft := m.mainEditor.Text()
	if _, shell := action.(chatShellAction); shell && submittedText != nil {
		draft = *submittedText
	}
	m.submitToken++
	m.pendingNewChatActions++
	result := newSessionActionDoneMsg{
		origin: m, token: m.submitToken, action: action, submittedText: textutil.Pointer(submittedText),
	}
	return func() tea.Msg {
		settled := (chatActionTransport{actions: service, goals: service}).execute(context.Background(), target, &draft, action)
		result.session, result.err, result.diagnostic = settled.session, settled.err, settled.diagnostic
		return result
	}
}
