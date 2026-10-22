package app

import (
	"context"
	"errors"
	"testing"

	"core/cli/app/commands"
	"core/shared/clientui"
	"core/shared/runtimeids"
	"core/shared/runtimeinput"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInputShortcutsSelectServerAdmissionIntent(t *testing.T) {
	for _, key := range []struct {
		name   string
		msg    tea.Msg
		intent runtimeinput.PendingWorkLane
	}{
		{"Enter", tea.KeyMsg{Type: tea.KeyEnter}, runtimeinput.PendingWorkLaneSteer},
		{"Tab", tea.KeyMsg{Type: tea.KeyTab}, runtimeinput.PendingWorkLaneQueue},
		{"CtrlEnter", customKeyMsg{Kind: customKeyCtrlEnter}, runtimeinput.PendingWorkLaneQueue},
	} {
		for _, input := range []struct {
			text string
			kind runtimeinput.Kind
		}{
			{"ordinary input", runtimeinput.KindText},
			{"/prompt:check arguments", runtimeinput.KindPromptCommand},
			{"$echo ready", runtimeinput.KindShell},
		} {
			t.Run(key.name+"/"+string(input.kind), func(t *testing.T) {
				client := &runtimeControlFakeClient{}
				model := newProjectedTestUIModel(client,
					WithUIPromptCommandCatalogEntries([]commands.PromptCommandCatalogEntry{{Name: "prompt:check"}}))
				model.commandRegistry = commands.NewDefaultRegistryWithPromptCatalog(model.promptCatalogEntries)
				model.setRuntimeActivityBusyForTest(true)
				testSetMainInput(model, input.text)
				_, cmd := model.Update(key.msg)
				collectCmdMessages(t, cmd)
				if client.submitCalls != 1 || client.submitRequest.Intent != key.intent ||
					client.submitRequest.Input.Kind != input.kind || client.submitRequest.QueueItemID.IsZero() {
					t.Fatalf("submission = %+v; calls = %d", client.submitRequest, client.submitCalls)
				}
				if len(model.injectedQueue) != 1 || model.injectedQueue[0].Request.QueueItemID != client.submitRequest.QueueItemID {
					t.Fatal("admission did not retain the dispatched identity")
				}
				if len(model.queued) != 0 || model.mainEditor.Text() != "" {
					t.Fatal("input remained in a local queue or composer")
				}
			})
		}
	}
}

type inputAdmissionTestClient struct {
	runtimeControlFakeClient
	rejections map[runtimeids.QueueItemID]error
}

func (c *inputAdmissionTestClient) SubmitRuntimeInput(_ context.Context, request clientui.RuntimeSubmitRequest) (clientui.UserTurnSubmission, error) {
	if err := c.rejections[request.QueueItemID]; err != nil {
		return clientui.UserTurnSubmission{}, err
	}
	return clientui.UserTurnSubmission{
		ResultKind: clientui.UserTurnResultKindQueued,
		Queued:     clientui.QueuedUserMessage{ID: request.QueueItemID.String(), Text: runtimeSubmitInputText(request)},
	}, nil
}

func TestInputAdmissionsSettleIndependently(t *testing.T) {
	client := &inputAdmissionTestClient{rejections: make(map[runtimeids.QueueItemID]error)}
	model := newProjectedTestUIModel(client)
	testSetMainInput(model, "first")
	_, first := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	testSetMainInput(model, "second")
	_, second := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	if len(model.injectedQueue) != 2 {
		t.Fatal("second admission was blocked by the outstanding first request")
	}
	firstID := model.injectedQueue[0].Request.QueueItemID
	secondID := model.injectedQueue[1].Request.QueueItemID
	client.rejections[firstID] = errors.New("admission rejected")
	testSetMainInput(model, "new draft")
	for _, command := range []tea.Cmd{second, first} {
		for _, message := range collectCmdMessages(t, command) {
			model = updateUIModel(t, model, message)
		}
	}
	if model.mainEditor.Text() != "new draft\n\nfirst" {
		t.Fatalf("failed input restoration = %q", model.mainEditor.Text())
	}
	if len(model.injectedQueue) != 1 || model.injectedQueue[0].Request.QueueItemID != secondID {
		t.Fatal("rejection discarded the unrelated accepted input")
	}
}
