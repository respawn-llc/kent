package app

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"core/cli/tui/ongoing"
	"core/shared/clientui"
	"core/shared/protoapi"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	tea "github.com/charmbracelet/bubbletea"
	xansi "github.com/charmbracelet/x/ansi"
)

func TestAskCompletePromptFitIncludesFinalAllocation(t *testing.T) {
	var output bytes.Buffer
	model := sizedTestUIModel(newProjectedStaticUIModel(
		WithUIOngoingSurface(ongoing.NewSurface(&output))), 80, 10)
	event := testQuestionAskEvent("fit", "Question", "Continue")
	testSetActiveAsk(model, &event)
	model.ask.activeProjection.rows = []string{"one", "two", "three"}
	if !model.askFitsInline(nil) {
		t.Fatal("complete prompt did not fit without competing sections")
	}
	model.ongoingTranscript = newNoopOngoingTranscriptController(model.ongoingSurface, model.ongoingFrameInput)
	model.ongoingTranscript.liveReadModel.addPendingTool(&transcriptpb.ToolStart{
		ToolCallId: "competing-one", ToolName: "shell",
	})
	model.ongoingTranscript.liveReadModel.addPendingTool(&transcriptpb.ToolStart{
		ToolCallId: "competing-two", ToolName: "shell",
	})
	if model.askFitsInline(nil) {
		t.Fatal("fit ignored rows removed by final competing-section allocation")
	}
	if output.Len() != 0 {
		t.Fatal("fit query wrote terminal output")
	}
}

func TestAskOversizedPromptOpensReadingWhileShortPromptStaysInline(t *testing.T) {
	model, control := newProjectedPromptTestUIModel(t)
	model = sizedTestUIModel(model, 80, 10)
	event := testQuestionAskEvent("overflow", "Question", "Continue")
	model = updateUIModel(t, model, askEventMsg{event: event})
	if model.surface() != uiSurfaceOngoingTranscript {
		t.Fatal("short prompt left ongoing")
	}
	model.ask.activeProjection.rows = make([]string, 12)
	model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 10})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if !model.surface().wantsAltScreen() || model.ask.dedicated == nil || model.ask.dedicated.scroll != 0 {
		t.Fatal("oversized prompt did not open at document top")
	}
	if len(control.batchRequests) != 0 || testActiveAsk(model) == nil {
		t.Fatal("initial dedicated reading submitted an answer")
	}
	document := model.layout().askDocument(80)
	if len(document.lines) != 15 {
		t.Fatalf("complete document = %d rows, want all question and answer rows", len(document.lines))
	}
}

func TestAskDedicatedMinimumSizeKeepsDraftAndNavigation(t *testing.T) {
	model, _ := newProjectedPromptTestUIModel(t, WithUITerminalCursorState(newUITerminalCursorState()))
	model = sizedTestUIModel(model, 80, 12)
	event := testQuestionAskEvent("minimum-size", "Question", "Continue")
	testSetActiveAsk(model, &event)
	model.ask.activeProjection.rows = make([]string, 20)
	model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 12})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("draft")})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	delivery := model.ask.activeDelivery
	if delivery == nil {
		t.Fatal("test did not establish in-flight delivery")
	}
	t.Cleanup(delivery.cancelPending)
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgUp})
	model.ask.dedicated.scroll = 3
	current := model.ask.current
	for _, size := range []tea.WindowSizeMsg{{Width: 39, Height: 10}, {Width: 40, Height: 9}} {
		model = updateUIModel(t, model, size)
		if strings.TrimSpace(xansi.Strip(model.View())) != "" {
			t.Fatal("undersized dedicated frame was not blank")
		}
		if model.ask.current != current || model.ask.dedicated.scroll != 3 || model.ask.dedicated.focus != askFocusReading || model.ask.editor.Text() != "draft" || model.ask.activeDelivery != delivery {
			t.Fatal("undersized frame changed pending prompt state")
		}
		if _, visible := model.terminalCursor.Snapshot(); visible {
			t.Fatal("undersized frame exposed the answer cursor")
		}
	}
	model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 40, Height: 10})
	if model.surface() != uiSurfaceAsk || strings.TrimSpace(xansi.Strip(model.View())) == "" {
		t.Fatal("dedicated prompt did not resume at usable geometry")
	}
}

func TestAskCompletePromptFitBoundaries(t *testing.T) {
	for _, test := range []struct {
		name         string
		questionRows int
		options      []string
		draft        string
		want         uiSurface
	}{
		{name: "exact", questionRows: 3, options: []string{"Continue"}, want: uiSurfaceOngoingTranscript},
		{name: "one-row-overflow", questionRows: 4, options: []string{"Continue"}, want: uiSurfaceAsk},
		{name: "options-overflow", questionRows: 1, options: []string{"First", "Second", "Third", "Fourth"}, want: uiSurfaceAsk},
		{name: "draft-overflow", questionRows: 4, draft: "first\nsecond", want: uiSurfaceAsk},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := sizedTestUIModel(newProjectedStaticUIModel(), 80, 10)
			model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("boundary", "Question", test.options...)})
			model.ask.activeProjection.rows = make([]string, test.questionRows)
			model.ask.editor.Replace(test.draft)
			model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 10})
			if model.surface() != test.want {
				t.Fatalf("complete prompt surface = %v, want %v", model.surface(), test.want)
			}
		})
	}
}

func TestAskDedicatedNavigationFollowsRenderedRows(t *testing.T) {
	model, control := newProjectedPromptTestUIModel(t, WithUITerminalCursorState(newUITerminalCursorState()))
	model = sizedTestUIModel(model, 80, 10)
	event := testQuestionAskEvent("navigation", "Question", strings.Repeat("first wrapped choice ", 12), strings.Repeat("second wrapped choice ", 12))
	testSetActiveAsk(model, &event)
	model.ask.activeProjection.rows = make([]string, 12)
	model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 10})
	document := model.layout().askDocument(80)
	checkRow := func(row int) {
		t.Helper()
		document = model.layout().askDocument(80)
		var option *int
		for index, rows := range document.options {
			if row >= rows.start && row < rows.end {
				selected := index
				option = &selected
			}
		}
		if model.askHasAnswerFocus() != (option != nil) || (option != nil && model.ask.cursor != *option) {
			t.Fatalf("row %d did not select only the option under the reading cursor", row)
		}
		frame := model.layout().composeAskFrame(80, 10, uiThemeStyles(model.theme))
		if !frame.inputCursor.Visible || frame.inputCursor.Row != framedInputContentCursorRow(row-model.ask.dedicated.scroll) {
			t.Fatalf("row %d did not retain a visible reading cursor", row)
		}
	}
	checkRow(0)
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if len(control.batchRequests) != 0 {
		t.Fatal("reading cursor on question text submitted an answer")
	}
	for row := 1; row < len(document.lines); row++ {
		model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
		checkRow(row)
	}
	for row := len(document.lines) - 2; row >= 0; row-- {
		model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
		checkRow(row)
	}
	for row := 0; row < len(document.lines)-1; {
		row = min(row+askDocumentCapacity(10), len(document.lines)-1)
		model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
		checkRow(row)
	}
	for row := len(document.lines) - 1; row > 0; {
		row = max(0, row-askDocumentCapacity(10))
		model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgUp})
		checkRow(row)
	}
	for row := 0; row < document.options[1].start; row++ {
		model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	}
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	runPromptDeliveryCommand(t, next.(*uiModel), command)
	answer := requireQuestionAnswerEntry(t, requirePromptAnswerBatchRequest(t, control)).GetQuestionAnswer()
	if answer.GetSelectedOptionNumber() != 2 {
		t.Fatalf("Enter did not submit the option under the reading cursor: %+v", answer)
	}
}

func TestAskDedicatedFreeformReadingAndEditing(t *testing.T) {
	model := sizedTestUIModel(newProjectedStaticUIModel(
		WithUITerminalCursorState(newUITerminalCursorState())), 80, 10)
	event := testQuestionAskEvent("freeform-reading", "Question")
	model = updateUIModel(t, model, askEventMsg{event: event})
	model.ask.activeProjection.rows = make([]string, 12)
	model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 10})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("one\ntwo")})
	if !model.askHasAnswerFocus() || model.ask.editor.Text() != "one\ntwo" {
		t.Fatal("typing from reading did not focus the editor and apply input")
	}
	document := model.layout().askDocument(80)
	if model.ask.dedicated.scroll != len(document.lines)-askDocumentCapacity(10) {
		t.Fatal("typing did not jump to document bottom")
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
	if !model.askHasAnswerFocus() {
		t.Fatal("Up within the editor entered reading before the editor boundary")
	}
	model.ask.editor.SetCursor(0)
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
	if model.askHasAnswerFocus() {
		t.Fatal("Up at editor boundary did not enter reading")
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if testActiveAsk(model) == nil || model.transientStatusKind == uiStatusNoticeError {
		t.Fatal("hidden editor Enter attempted an answer")
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyEnd})
	if !model.askHasAnswerFocus() || model.ask.editor.Text() != "one\ntwo" {
		t.Fatal("editor movement did not restore focus without changing the draft")
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune(strings.Repeat("\nline", 20))})
	for step := 0; step < 22; step++ {
		model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyUp})
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRight})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if testActiveAsk(model) == nil || model.transientStatusKind == uiStatusNoticeError {
		t.Fatal("Enter attempted submission with an editor cursor outside the document window")
	}
	document = model.layout().askDocument(80)
	draft := model.ask.editor.Text()
	for step := 0; step < len(document.lines); step += askDocumentCapacity(10) {
		model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgUp})
	}
	for row := 0; row < len(document.lines)-1; {
		row = min(row+askDocumentCapacity(10), len(document.lines)-1)
		model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
		frame := model.layout().composeAskFrame(80, 10, uiThemeStyles(model.theme))
		if !frame.inputCursor.Visible || frame.inputCursor.Row != framedInputContentCursorRow(row-model.ask.dedicated.scroll) {
			t.Fatal("page movement through the editor lost the rendered-row cursor")
		}
	}
	if model.ask.editor.Text() != draft {
		t.Fatal("page movement through the editor changed the draft")
	}
}

func TestAskDedicatedTabRoundTripRestoresReading(t *testing.T) {
	model := sizedTestUIModel(newProjectedStaticUIModel(), 80, 10)
	model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("reading-tab", "Question", "First")})
	model.ask.activeProjection.rows = make([]string, 12)
	model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 10})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if !model.ask.freeform || !model.askHasAnswerFocus() {
		t.Fatal("Tab from reading did not focus freeform")
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("draft")})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgUp})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if !model.ask.freeform || !model.askHasAnswerFocus() {
		t.Fatal("Tab while reading an existing freeform draft did not focus the editor")
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if model.ask.freeform || model.askHasAnswerFocus() || model.ask.editor.Text() != "draft" {
		t.Fatal("Tab back did not restore reading and preserve the draft")
	}
}

func TestAskDedicatedReadingFreeformDoesNotInventAnOption(t *testing.T) {
	model, control := newProjectedPromptTestUIModel(t)
	model = sizedTestUIModel(model, 80, 10)
	model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent(
		"reading-freeform-answer", strings.Repeat("Paragraph.\n\n", 20), "First", "Second",
	)})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("independent answer")})
	next, command := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = runPromptDeliveryCommand(t, next.(*uiModel), command)
	answer := requireQuestionAnswerEntry(t, requirePromptAnswerBatchRequest(t, control)).GetQuestionAnswer()
	if answer.SelectedOptionNumber != nil || answer.GetFreeform() != "independent answer" {
		t.Fatalf("freeform entered from reading invented an option: %+v", answer)
	}
}

func TestAskDedicatedOptionQuestionTypingEntersFreeformFromReading(t *testing.T) {
	for _, test := range []struct {
		name  string
		key   tea.KeyMsg
		draft string
		want  string
	}{
		{name: "typing", key: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("typed directly")}, want: "typed directly"},
		{name: "bracketed-paste", key: tea.KeyMsg{Type: tea.KeyRunes, Paste: true, Runes: []rune("pasted directly")}, want: "pasted directly"},
		{name: "clipboard", key: tea.KeyMsg{Type: tea.KeyCtrlV}, want: "clipboard directly"},
		{name: "editing", key: tea.KeyMsg{Type: tea.KeyBackspace}, draft: "draft", want: "draf"},
		{name: "movement", key: tea.KeyMsg{Type: tea.KeyLeft}, draft: "draft", want: "draft"},
		{name: "line-start", key: tea.KeyMsg{Type: tea.KeyHome}, draft: "draft", want: "draft"},
	} {
		t.Run(test.name, func(t *testing.T) {
			model, control := newProjectedPromptTestUIModel(t, WithUIClipboardPaster(clipboardPasterFunc(func(context.Context) (uiClipboardContent, error) {
				return uiClipboardText{Text: "clipboard directly"}, nil
			})))
			model = sizedTestUIModel(model, 80, 10)
			model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent(
				"reading-typing", strings.Repeat("Paragraph.\n\n", 20), "First", "Second",
			)})
			model.ask.editor.Replace(test.draft)
			next, command := model.Update(test.key)
			model = next.(*uiModel)
			for _, message := range collectCmdMessages(t, command) {
				model = updateUIModel(t, model, message)
			}
			if !model.ask.freeform || !model.askHasAnswerFocus() || model.ask.editor.Text() != test.want {
				t.Fatal("typing from option Question reading did not enter freeform")
			}
			document := model.layout().askDocument(80)
			if model.ask.dedicated.scroll != len(document.lines)-askDocumentCapacity(10) {
				t.Fatal("typing from option Question reading did not jump to bottom")
			}
			next, command = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
			model = runPromptDeliveryCommand(t, next.(*uiModel), command)
			answer := requireQuestionAnswerEntry(t, requirePromptAnswerBatchRequest(t, control)).GetQuestionAnswer()
			if answer.SelectedOptionNumber != nil || answer.GetFreeform() != test.want {
				t.Fatalf("direct typing invented an option or lost the draft: %+v", answer)
			}
		})
	}
}

func TestAskDedicatedApprovalReadingKeepsChosenCommentary(t *testing.T) {
	model, control := newProjectedPromptTestUIModel(t)
	model = sizedTestUIModel(model, 80, 10)
	model = updateUIModel(t, model, askEventMsg{event: model.transcriptPromptEvent(
		testApprovalPrompt("approval-reading", "Approve?", clientui.ApprovalDecisionAllowOnce, clientui.ApprovalDecisionDeny),
	)})
	model.ask.activeProjection.rows = make([]string, 12)
	model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 10})
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("unbound commentary")},
		{Type: tea.KeyRunes, Paste: true, Runes: []rune("unbound paste")},
		{Type: tea.KeyCtrlV},
	} {
		model = updateUIModel(t, model, key)
	}
	if model.ask.freeform || model.ask.chosenApproval != nil || model.ask.editor.Text() != "" {
		t.Fatal("initial Approval reading accepted commentary without a chosen decision")
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if model.ask.freeform || model.askHasAnswerFocus() {
		t.Fatal("initial approval Tab invented a chosen decision")
	}
	for step := 0; step <= len(model.layout().askDocument(80).lines) && !model.askHasAnswerFocus(); step++ {
		model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("deny draft")})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgUp})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	for step := 0; step <= len(model.layout().askDocument(80).lines) && !model.askHasAnswerFocus(); step++ {
		model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	}
	if model.ask.freeform || model.ask.cursor != 0 || model.ask.editor.Text() != "deny draft" {
		t.Fatal("reading did not return to option one with the commentary draft intact")
	}
	if model.ask.chosenApproval == nil || *model.ask.chosenApproval != clientui.ApprovalDecisionDeny {
		t.Fatal("first-option navigation rebound the commentary")
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgUp})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	if model.ask.freeform || model.askHasAnswerFocus() || model.ask.editor.Text() != "deny draft" {
		t.Fatal("Approval commentary entered from reading did not return to reading with its draft")
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if len(control.batchRequests) != 0 || !model.ask.hasCurrent() {
		t.Fatal("Enter after returning from Approval commentary submitted an answer")
	}
	for _, key := range []tea.KeyMsg{
		{Type: tea.KeyRunes, Runes: []rune("!")},
		{Type: tea.KeyBackspace},
		{Type: tea.KeyRunes, Paste: true, Runes: []rune("!")},
		{Type: tea.KeyBackspace},
		{Type: tea.KeyLeft},
		{Type: tea.KeyRight},
	} {
		model = updateUIModel(t, model, key)
		document := model.layout().askDocument(80)
		if !model.ask.freeform || !model.askHasAnswerFocus() ||
			model.ask.dedicated.scroll != max(0, len(document.lines)-askDocumentCapacity(10)) {
			t.Fatalf("chosen Approval reading did not focus the bottom editor for %v", key)
		}
		if model.ask.chosenApproval == nil || *model.ask.chosenApproval != clientui.ApprovalDecisionDeny {
			t.Fatal("editing from reading rebound the chosen Approval decision")
		}
		model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
		if model.ask.freeform || model.askHasAnswerFocus() {
			t.Fatal("editing from reading lost its reading return destination")
		}
	}
	if model.ask.editor.Text() != "deny draft" {
		t.Fatal("editing from reading did not apply operations to the existing draft")
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	next, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model = runPromptDeliveryCommand(t, next.(*uiModel), cmd)
	answer := requireApprovalAnswerEntry(t, requirePromptAnswerBatchRequest(t, control)).GetApprovalAnswer()
	decision, err := protoapi.ApprovalDecisionFromProto(answer.Decision)
	if err != nil || decision != clientui.ApprovalDecisionDeny || approvalCommentary(answer) != "deny draft" {
		t.Fatalf("commentary answer lost chosen decision or draft: %+v, %v", answer, err)
	}
}

func TestAskDedicatedGroupClosesOnlyAfterFinalResolution(t *testing.T) {
	model := sizedTestUIModel(newProjectedStaticUIModel(), 80, 10)
	model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("first", "Question", "Continue")})
	model.ask.activeProjection.rows = make([]string, 12)
	model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 10})
	model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("second", "Short", "Continue")})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
	model = updateUIModel(t, model, askEventMsg{event: askEvent{resolvedToolCallID: "first"}})
	if model.surface() != uiSurfaceAsk || model.ask.dedicated == nil || model.ask.dedicated.scroll != 0 || model.askHasAnswerFocus() {
		t.Fatal("queued prompt did not retain promotion and reset reading")
	}
	model = updateUIModel(t, model, askEventMsg{event: askEvent{resolvedToolCallID: "second"}})
	if model.surface() != uiSurfaceOngoingTranscript || model.ask.dedicated != nil || model.inputMode() != uiInputModeMain {
		t.Fatal("final canonical resolution did not restore ongoing")
	}
	model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("new-group", "Short", "Continue")})
	if model.surface() != uiSurfaceOngoingTranscript || model.ask.dedicated != nil {
		t.Fatal("new short group inherited dedicated promotion")
	}
}

func TestAskDedicatedRefreshAndResizeClampWithoutDemoting(t *testing.T) {
	model := sizedTestUIModel(newProjectedStaticUIModel(), 80, 10)
	model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("refresh", "Question", "Continue")})
	model.ask.activeProjection.rows = make([]string, 20)
	model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 10})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyTab})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("draft")})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgUp})
	model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("refresh", "Shorter question", "Continue")})
	if model.surface() != uiSurfaceAsk || model.ask.dedicated.scroll != 0 || model.askHasAnswerFocus() || model.ask.editor.Text() != "draft" {
		t.Fatal("short refresh demoted the group or lost focus/draft/clamping")
	}
	model = updateUIModel(t, model, tea.WindowSizeMsg{Width: 80, Height: 40})
	if model.surface() != uiSurfaceAsk || model.ask.dedicated.scroll != 0 {
		t.Fatal("growth demoted the group or left an invalid scroll offset")
	}
}

func TestAskDedicatedRefreshKeepsFocusedOptionVisible(t *testing.T) {
	model := sizedTestUIModel(newProjectedStaticUIModel(), 80, 10)
	model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent(
		"focused-refresh", strings.Repeat("Paragraph.\n\n", 20), "First", "Second",
	)})
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyPgDown})
	for step := 0; step <= len(model.layout().askDocument(80).lines) && !model.askHasAnswerFocus(); step++ {
		model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	}
	model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent(
		"focused-refresh", strings.Repeat("Expanded paragraph.\n\n", 40), "First", "Second",
	)})
	if model.ask.cursor != 1 || !model.askHasVisibleAnswerFocus() {
		t.Fatal("refresh lost the focused option or left it outside the document window")
	}
}

func TestAskIncomingAssistantOverflowPromotesBeforePainting(t *testing.T) {
	var output bytes.Buffer
	surface := ongoing.NewSurface(&output)
	model := sizedTestUIModel(newProjectedStaticUIModel(WithUIOngoingSurface(surface)), 80, 10)
	model.ongoingTranscript = newOngoingTranscriptController(surface, model.ongoingFrameInput,
		func(*transcriptpb.Message) (runtimeTupleMergeResult, error) { return runtimeTupleMergeResult{}, nil },
		model.applyAdmittedTranscriptMessageState)
	if _, _, err := model.ongoingTranscript.Accept(ongoingHydrationMessage(1)); err != nil {
		t.Fatal(err)
	}
	model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("assistant-fit", "Question", "Continue")})
	model.ask.activeProjection.rows = []string{"one", "two", "three"}
	if !model.askFitsInline(nil) {
		t.Fatal("prompt did not initially fit")
	}
	output.Reset()
	message := transcriptTestMessage(2, &transcriptpb.AssistantDelta{
		StepId: ongoingTestStepID().String(), StreamId: "assistant-fit-stream",
		Delta: "first line\nsecond line", Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
	})
	model = updateUIModel(t, model, ongoingTranscriptEvent{Kind: ongoingTranscriptEventMessage, Message: message})
	if model.surface() != uiSurfaceAsk || output.Len() != 0 || len(model.ongoingTranscript.queue) != 1 {
		t.Fatal("incoming assistant painted a clipped prompt instead of promoting and queuing")
	}
	model = updateUIModel(t, model, askEventMsg{event: askEvent{resolvedToolCallID: "assistant-fit"}})
	if output.Len() != 0 {
		t.Fatal("resolution painted native output before the exit/ownership command")
	}
	model = updateUIModel(t, model, ongoingNormalBufferOwnedMsg{owned: true})
	if len(model.ongoingTranscript.queue) != 0 || output.Len() == 0 {
		t.Fatal("existing restoration did not drain the retained assistant update")
	}
	_, _, err := model.ongoingTranscript.Accept(transcriptTestMessage(3, &transcriptpb.CommittedRow{
		Row: &transcriptpb.CommittedRow_Assistant{Assistant: &transcriptpb.AssistantRow{
			StreamId: &message.Event.GetAssistantDelta().StreamId,
			Text:     message.Event.GetAssistantDelta().Delta,
		}},
	}))
	if err != nil {
		t.Fatal(err)
	}
}

func TestAskDeferredReturnMeasuresQueuedAssistantBeforeRestoringOwnership(t *testing.T) {
	for _, test := range []struct {
		name     string
		chunks   []string
		finalize bool
	}{
		{name: "single-update", chunks: []string{"first line\nsecond line"}},
		{name: "cumulative-updates", chunks: []string{"first line", "\nsecond line"}},
		{name: "overflow-before-stream-final", chunks: []string{"first line", "\nsecond line"}, finalize: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			surface := ongoing.NewSurface(&output)
			model := sizedTestUIModel(newProjectedStaticUIModel(WithUIOngoingSurface(surface)), 80, 10)
			model.ongoingTranscript = newOngoingTranscriptController(surface, model.ongoingFrameInput,
				func(*transcriptpb.Message) (runtimeTupleMergeResult, error) { return runtimeTupleMergeResult{}, nil },
				model.applyAdmittedTranscriptMessageState)
			if _, _, err := model.ongoingTranscript.Accept(ongoingHydrationMessage(1)); err != nil {
				t.Fatal(err)
			}
			model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyShiftTab})
			model = updateUIModel(t, model, askEventMsg{event: testQuestionAskEvent("deferred-fit", "Question", "Continue")})
			model.ask.activeProjection.rows = []string{"one", "two", "three"}
			for index, chunk := range test.chunks {
				model = updateUIModel(t, model, ongoingTranscriptEvent{
					Kind: ongoingTranscriptEventMessage,
					Message: transcriptTestMessage(uint64(index+2), &transcriptpb.AssistantDelta{
						StepId: ongoingTestStepID().String(), StreamId: "deferred-fit-stream",
						Delta: chunk, Phase: transcriptpb.AssistantPhase_ASSISTANT_PHASE_COMMENTARY,
					}),
				})
			}
			if test.finalize {
				streamID := "deferred-fit-stream"
				model = updateUIModel(t, model, ongoingTranscriptEvent{
					Kind: ongoingTranscriptEventMessage,
					Message: transcriptTestMessage(uint64(len(test.chunks)+2), &transcriptpb.CommittedRow{
						Row: &transcriptpb.CommittedRow_Assistant{Assistant: &transcriptpb.AssistantRow{
							StreamId: &streamID, Text: strings.Join(test.chunks, ""),
						}},
					}),
				})
			}
			queuedCount := len(model.ongoingTranscript.queue)
			output.Reset()
			model = updateUIModel(t, model, tea.KeyMsg{Type: tea.KeyShiftTab})
			if model.surface() != uiSurfaceAsk || !model.altScreenActive {
				t.Fatal("deferred return did not choose ask before restoring the normal buffer")
			}
			model = updateUIModel(t, model, ongoingNormalBufferOwnedMsg{owned: true})
			if output.Len() != 0 || len(model.ongoingTranscript.queue) != queuedCount {
				t.Fatal("deferred return painted or consumed the overflowing queued update")
			}
		})
	}
}
