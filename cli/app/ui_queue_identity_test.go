package app

import (
	"testing"

	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	"core/shared/textutil"

	tea "github.com/charmbracelet/bubbletea"
)

func TestInputTerminalBeforeAdmissionReceipt(t *testing.T) {
	for _, status := range []transcriptpb.QueuedMessageStatus{
		transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_SUBMITTED,
		transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_DISCARDED,
		transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_FAILED,
	} {
		t.Run(status.String(), func(t *testing.T) {
			client := &inputAdmissionTestClient{}
			model := newProjectedTestUIModel(client)
			testSetMainInput(model, "pending input")
			_, command := model.Update(tea.KeyMsg{Type: tea.KeyTab})
			id := model.injectedQueue[0].Request.QueueItemID.String()
			testSetMainInput(model, "newer draft")
			state := &transcriptpb.QueuedMessageState{QueueItemId: id, Status: status}
			if status == transcriptpb.QueuedMessageStatus_QUEUED_MESSAGE_STATUS_FAILED {
				state.Text = textutil.Value("pending input")
			}
			model.applyTranscriptQueuedMessageState(state)
			for _, message := range collectCmdMessages(t, command) {
				model = updateUIModel(t, model, message)
			}
			expected := "newer draft"
			if state.Text != nil {
				expected += "\n\npending input"
			}
			if model.mainEditor.Text() != expected || len(model.injectedQueue) != 0 {
				t.Fatalf("terminal receipt: draft=%q outstanding=%d", model.mainEditor.Text(), len(model.injectedQueue))
			}
			model.applyTranscriptQueuedMessageState(state)
			if model.mainEditor.Text() != expected {
				t.Fatal("duplicate terminal observation restored input again")
			}
		})
	}
}

func TestStopBeforeReceiptsRestoresServerOrderWithoutResubmission(t *testing.T) {
	model := newProjectedTestUIModel(&inputAdmissionTestClient{})
	testSetMainInput(model, "first")
	_, first := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	testSetMainInput(model, "$echo second")
	_, second := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	firstID, secondID := model.injectedQueue[0].Request.QueueItemID.String(), model.injectedQueue[1].Request.QueueItemID.String()
	testSetMainInput(model, "draft")
	model.applyTranscriptHumanInputInterrupted(&transcriptpb.HumanInputInterrupted{
		Items: []*transcriptpb.InterruptedHumanInputItem{
			{QueueItemId: secondID, Text: "$echo second"},
			{QueueItemId: firstID, Text: "first"},
		},
	})
	for _, command := range []tea.Cmd{second, first} {
		for _, message := range collectCmdMessages(t, command) {
			model = updateUIModel(t, model, message)
		}
	}
	if model.mainEditor.Text() != "$echo second\n\nfirst\n\ndraft" || len(model.injectedQueue) != 0 {
		t.Fatalf("Stop restoration=%q outstanding=%d", model.mainEditor.Text(), len(model.injectedQueue))
	}
}
