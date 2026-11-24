package app

import (
	"fmt"
	"testing"

	"core/shared/clientui"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"

	tea "github.com/charmbracelet/bubbletea"
)

func TestAskNumberSelectsChoiceUntilEnterConfirms(t *testing.T) {
	for _, dedicated := range []bool{false, true} {
		name := "inline"
		if dedicated {
			name = "dedicated"
		}
		t.Run(name, func(t *testing.T) {
			model, control := newProjectedPromptTestUIModel(t)
			model = sizedTestUIModel(model, 80, 10)
			model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("shortcut", "Question", "First", "Second")})
			if dedicated {
				model.ask.activeProjection.rows = make([]string, 20)
				model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 10})
			}
			model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
			if model.ask.selectedOption == nil || *model.ask.selectedOption != 1 {
				t.Fatal("number did not select the corresponding choice")
			}
			if model.ask.freeform || model.ask.editor.Text() != "" || !model.askHasVisibleAnswerFocus() {
				t.Fatal("number did not focus and reveal the choice")
			}
			if len(control.batchRequests) != 0 || model.ask.activeDelivery != nil {
				t.Fatal("number submitted without Enter")
			}
			next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			runPromptDeliveryCommand(t, next.(*uiModel), command)
			answer := requireQuestionAnswerEntry(t, requirePromptAnswerBatchRequest(t, control)).GetQuestionAnswer()
			if answer.GetSelectedOptionNumber() != 2 {
				t.Fatalf("Enter submitted the wrong choice: %+v", answer)
			}
		})
	}
}

func TestAskShortcutPreservesEditableDigits(t *testing.T) {
	for _, dedicated := range []bool{false, true} {
		t.Run(fmt.Sprint(dedicated), func(t *testing.T) {
			model := sizedTestUIModel(newProjectedStaticUIModel(), 80, 10)
			model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("digits", "Question", "First", "Second")})
			if dedicated {
				model.ask.activeProjection.rows = make([]string, 20)
				model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 10})
			}
			model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
			for _, digit := range "1209" {
				model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{digit}})
			}
			if model.ask.editor.Text() != "1209" || !model.ask.freeform || model.ask.selectedOption != nil {
				t.Fatal("number shortcuts intercepted editable text")
			}
		})
	}
}

func TestAskShortcutRejectsNonChoiceKeys(t *testing.T) {
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune{'9'}},
		{Type: tea.KeyRunes, Runes: []rune{'2'}, Alt: true},
		{Type: tea.KeyRunes, Runes: []rune{'2'}, Paste: true},
		{Type: tea.KeyRunes, Runes: []rune("12")},
	} {
		model := sizedTestUIModel(newProjectedStaticUIModel(), 80, 20)
		model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("non-choice", "Question", "First", "Second")})
		model = updateUIModel(t, model, key)
		if model.ask.selectedOption != nil {
			t.Fatalf("non-choice input selected an option: %+v", key)
		}
	}
}

func TestApprovalNumberSelectsDecisionWithoutSubmitting(t *testing.T) {
	for _, dedicated := range []bool{false, true} {
		t.Run(fmt.Sprint(dedicated), func(t *testing.T) {
			model, control := newProjectedPromptTestUIModel(t)
			model = sizedTestUIModel(model, 80, 10)
			model = updateUIModel(t, model, askEventMsg{event: testApprovalAskEvent("approval-number", "Approve?", clientui.ApprovalDecisionAllowOnce, clientui.ApprovalDecisionDeny)})
			if dedicated {
				model.ask.activeProjection.rows = make([]string, 20)
				model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 10})
			}
			model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
			if model.ask.selectedOption != nil || model.ask.chosenApproval != nil {
				t.Fatal("zero invented an Approval decision")
			}
			model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
			if model.ask.selectedOption == nil || *model.ask.selectedOption != 1 ||
				model.ask.chosenApproval == nil || *model.ask.chosenApproval != clientui.ApprovalDecisionDeny ||
				!model.askHasVisibleAnswerFocus() {
				t.Fatal("number did not focus the corresponding Approval decision")
			}
			if len(control.batchRequests) != 0 || model.ask.activeDelivery != nil {
				t.Fatal("number submitted an Approval without Enter")
			}
			_, request := submitAskPromptKey(t, model, control, tea.KeyMsg{Type: tea.KeyEnter})
			answer := requireApprovalAnswerEntry(t, request).GetApprovalAnswer()
			if answer.Decision != promptpb.ApprovalDecision_APPROVAL_DECISION_DENY {
				t.Fatal("Enter submitted the wrong Approval decision")
			}
		})
	}
}

func TestAskZeroSelectsFreeformChoice(t *testing.T) {
	for _, count := range []int{2, 9, 12} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			model := sizedTestUIModel(newProjectedStaticUIModel(), 80, 40)
			options := make([]string, count)
			for i := range options {
				options[i] = fmt.Sprintf("Choice %d", i+1)
			}
			model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("custom", "Question", options...)})
			if count >= 9 {
				model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'9'}})
				if model.ask.selectedOption == nil || *model.ask.selectedOption != 8 {
					t.Fatal("nine did not select the ninth choice")
				}
			}
			if count < 9 {
				model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{rune('1' + count)}})
				if model.ask.selectedOption == nil || *model.ask.selectedOption != count || model.ask.freeform {
					t.Fatal("ordinary number did not select the freeform choice")
				}
				model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
			}
			model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'0'}})
			if model.ask.selectedOption == nil || *model.ask.selectedOption != count || model.ask.freeform {
				t.Fatal("zero did not select the freeform choice without opening its editor")
			}
			model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
			if !model.ask.freeform || !model.ask.hasCurrent() {
				t.Fatal("Enter did not open the selected freeform editor")
			}
		})
	}
}
