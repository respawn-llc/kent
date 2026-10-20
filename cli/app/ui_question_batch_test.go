package app

import (
	"fmt"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestQuestionBatchAnswerMakesNextQuestionActionable(t *testing.T) {
	for _, hydrated := range []bool{false, true} {
		for _, resolutionFirst := range []bool{false, true} {
			for _, freeform := range []bool{false, true} {
				t.Run(fmt.Sprintf("hydrated=%t/resolution_first=%t/freeform=%t", hydrated, resolutionFirst, freeform), func(t *testing.T) {
					testQuestionBatchAnswers(t, hydrated, resolutionFirst, freeform)
				})
			}
		}
	}
}

func testQuestionBatchAnswers(t *testing.T, hydrated, resolutionFirst, freeform bool) {
	t.Helper()
	model, control := newProjectedPromptTestUIModel(t)
	model = sizedTestUIModel(model, 80, 24)
	model.setRuntimeActivityBusyForTest(true)
	first := testQuestionPrompt("batch-first", "First?", "Continue", "Stop")
	second := testQuestionPrompt("batch-second", "Second?", "Proceed", "Wait")
	if freeform {
		second.GetQuestion().Suggestions = nil
	}

	apply := func(msg tea.Msg) tea.Cmd {
		next, command := model.Update(msg)
		model = next.(*uiModel)
		return command
	}
	if hydrated {
		model.ongoingTranscript = newOngoingTranscriptController(
			&ongoingSurfaceSpy{},
			model.ongoingFrameInput,
			noopOngoingTranscriptRuntimeAdmission,
			model.applyAdmittedTranscriptMessageState,
		)
		message := questionHydrationMessage(first)
		message.Event.GetHydration().PendingPrompts = append(message.Event.GetHydration().PendingPrompts, second)
		command := apply(ongoingTranscriptEvent{Kind: ongoingTranscriptEventMessage, Message: message})
		for _, msg := range collectCmdMessages(t, command) {
			if projection, ok := msg.(questionRenderResultMsg); ok {
				apply(projection)
			}
		}
	} else {
		projection := apply(askEventMsg{event: model.transcriptPromptEvent(first)})
		apply(requireQuestionRenderResult(t, projection))
		apply(askEventMsg{event: model.transcriptPromptEvent(second)})
	}

	delivery := apply(tea.KeyMsg{Type: tea.KeyEnter})
	if delivery == nil {
		t.Fatal("first Question did not submit an answer")
	}
	result := delivery()
	var nextProjection tea.Cmd
	if resolutionFirst {
		nextProjection = apply(askEventMsg{event: askEvent{resolvedToolCallID: "batch-first"}})
		apply(result)
	} else {
		nextProjection = apply(result)
	}
	firstRequest := requirePromptAnswerBatchRequest(t, control)
	if entry := requireQuestionAnswerEntry(t, firstRequest); entry.ToolCallId != first.GetQuestion().ToolCallId ||
		firstRequest.StepId != first.GetQuestion().StepId {
		t.Fatalf("first answer targeted the wrong Question: %v", firstRequest)
	}
	apply(requireQuestionRenderResult(t, nextProjection))

	if model.inputMode() != uiInputModeAsk || model.activity != uiActivityQuestion {
		t.Fatal("second Question did not retain input focus")
	}
	visible, cursor := testVisibleAskPaneContent(model, 80)
	options := 0
	inputs := 0
	for _, line := range visible {
		if line.prompt.Kind == askPromptLineKindOption {
			options++
		}
		if line.prompt.Kind == askPromptLineKindInput {
			inputs++
		}
	}
	if freeform {
		if inputs != 1 || cursor == nil {
			t.Fatal("second Question did not expose its freeform editor")
		}
		apply(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("answer")})
	} else if options != len(second.GetQuestion().Suggestions)+1 {
		t.Fatalf("second Question has %d actionable options", options)
	}

	// A late resolution for the first Question must not dismiss its successor.
	apply(askEventMsg{event: askEvent{resolvedToolCallID: "batch-first"}})
	delivery = apply(tea.KeyMsg{Type: tea.KeyEnter})
	if delivery == nil {
		t.Fatal("second Question did not submit an answer")
	}
	apply(delivery())
	secondRequest := requirePromptAnswerBatchRequest(t, control)
	if entry := requireQuestionAnswerEntry(t, secondRequest); entry.ToolCallId != second.GetQuestion().ToolCallId ||
		secondRequest.StepId != firstRequest.StepId {
		t.Fatalf("second answer targeted the wrong Question or Step: %v", secondRequest)
	}
	if model.ask.hasCurrent() || model.inputMode() != uiInputModeMain || model.activity != uiActivityRunning {
		t.Fatal("answering the batch did not restore the main composer and running activity")
	}
}
