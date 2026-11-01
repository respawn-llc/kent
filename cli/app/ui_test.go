package app

import (
	"core/cli/tui"
	"core/cli/tui/transcriptrender"
	tuitest "core/internal/testharness/pty"
	"core/shared/clientui"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	goruntime "runtime"
	"strings"
	"testing"
)

func TestCustomKeyShiftEnterInsertsNewline(t *testing.T) {
	m := newProjectedStaticUIModel()
	testSetMainInput(m, "hello")

	next, _ := m.Update(customKeyMsg{Kind: customKeyShiftEnter})
	updated := next.(*uiModel)

	if updated.isBusy() {
		t.Fatal("did not expect busy after shift+enter CSI sequence")
	}
	if testMainInput(updated) != "hello\n" {
		t.Fatalf("expected newline insertion from shift+enter CSI sequence, got %q", testMainInput(updated))
	}
}

func TestCustomKeyShiftEnterThenEnterDoesNotSubmitTrailingNewline(t *testing.T) {
	m := newProjectedStaticUIModel()
	testSetMainInput(m, "hello")

	next, _ := m.Update(customKeyMsg{Kind: customKeyShiftEnter})
	updated := next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)

	if !updated.hasPendingInputAdmission() || updated.injectedQueue[0].Text != "hello" {
		t.Fatal("expected admission without the synthetic trailing newline")
	}
}

func TestCustomKeyCtrlBackspaceDeletesCurrentLine(t *testing.T) {
	m := newProjectedStaticUIModel()
	testSetMainInputAtRuneCursor(m, "one\ntwo\nthree", 5) // inside "two"

	next, _ := m.Update(customKeyMsg{Kind: customKeyCtrlBackspace})
	updated := next.(*uiModel)

	if testMainInput(updated) != "one\nthree" {
		t.Fatalf("expected ctrl+backspace CSI to remove current line, got %q", testMainInput(updated))
	}
	if testMainInputRuneCursor(updated) != 4 {
		t.Fatalf("expected cursor at start of joined line after delete, got %d", testMainInputRuneCursor(updated))
	}
}

func TestMainComposerMutatesTokenOnlyForEditsAndKeepsByteCursor(t *testing.T) {
	m := newProjectedStaticUIModel()
	initialToken := m.mainInputDraftToken

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyBackspace})
	if got := m.mainInputDraftToken; got != initialToken {
		t.Fatalf("empty backspace token = %d, want unchanged %d", got, initialToken)
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("界")})
	if got, want := testMainInput(m), "界"; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
	if got, want := m.mainEditor.Cursor(), len("界"); got != want {
		t.Fatalf("byte cursor after unicode insert = %d, want %d", got, want)
	}
	if m.mainInputDraftToken == initialToken {
		t.Fatal("unicode insert did not advance main draft token")
	}
	afterInsertToken := m.mainInputDraftToken

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyLeft})
	if got, want := m.mainEditor.Cursor(), 0; got != want {
		t.Fatalf("byte cursor after left = %d, want %d", got, want)
	}
	if got := m.mainInputDraftToken; got != afterInsertToken {
		t.Fatalf("cursor movement token = %d, want unchanged %d", got, afterInsertToken)
	}

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyDelete})
	if got, want := testMainInput(m), ""; got != want {
		t.Fatalf("input after delete = %q, want %q", got, want)
	}
	if m.mainInputDraftToken == afterInsertToken {
		t.Fatal("delete did not advance main draft token")
	}
	afterDeleteToken := m.mainInputDraftToken

	m = updateUIModel(t, m, tea.KeyMsg{Type: tea.KeyDelete})
	if got := m.mainInputDraftToken; got != afterDeleteToken {
		t.Fatalf("empty delete token = %d, want unchanged %d", got, afterDeleteToken)
	}
}

func TestSharedEditorsRemoveSplitMouseSGRSequencesAroundUnicode(t *testing.T) {
	const prefix = "界"
	const suffix = "尾"
	const first = "\x1b[<64;"
	const second = "63;24M" + suffix

	t.Run("main", func(t *testing.T) {
		m := newProjectedStaticUIModel()
		m.insertInputRunes([]rune(prefix + first))
		m.insertInputRunes([]rune(second))
		if got, want := m.mainEditor.Text(), prefix+suffix; got != want {
			t.Fatalf("main editor = %q, want %q", got, want)
		}
	})

	t.Run("ask", func(t *testing.T) {
		m := newProjectedStaticUIModel()
		m.insertAskInputRunes([]rune(prefix + first))
		m.insertAskInputRunes([]rune(second))
		if got, want := m.ask.editor.Text(), prefix+suffix; got != want {
			t.Fatalf("ask editor = %q, want %q", got, want)
		}
	})

	t.Run("single line", func(t *testing.T) {
		editor := newSingleLineEditor("")
		updateSingleLineEditorWithAppKeys(&editor, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(prefix + first)})
		updateSingleLineEditorWithAppKeys(&editor, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(second)})
		if got, want := editor.Text(), prefix+suffix; got != want {
			t.Fatalf("single-line editor = %q, want %q", got, want)
		}
	})
}

func TestParseUserShellCommand(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantCmd string
		wantOK  bool
	}{
		{name: "basic", input: "$ pwd", wantCmd: "pwd", wantOK: true},
		{name: "leading spaces", input: "   $   echo hi", wantCmd: "echo hi", wantOK: true},
		{name: "empty", input: "$", wantCmd: "", wantOK: false},
		{name: "not shell prefix", input: "echo $HOME", wantCmd: "", wantOK: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			gotCmd, gotOK := parseUserShellCommand(tc.input)
			if gotOK != tc.wantOK {
				t.Fatalf("ok = %v, want %v", gotOK, tc.wantOK)
			}
			if gotCmd != tc.wantCmd {
				t.Fatalf("command = %q, want %q", gotCmd, tc.wantCmd)
			}
		})
	}
}

func TestAskQuestionTabFreeformFlow(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	event := testQuestionAskEvent("ask-1", "Pick one", "a", "b")

	updated := updateUIModel(t, m, askEventMsg{event: event})
	if testAskFreeform(updated) {
		t.Fatal("expected picker mode first")
	}

	next, _ := updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated = next.(*uiModel)
	if !testAskFreeform(updated) {
		t.Fatal("expected tab to open freeform commentary")
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("[<64;55;24M[<64;56;26M[<65;56;26M")})
	updated = next.(*uiModel)
	if testAskInput(updated) != "" {
		t.Fatalf("expected mouse sgr sequence ignored in ask freeform input, got %q", testAskInput(updated))
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("custom")})
	updated = next.(*uiModel)
	updated, request := submitAskPromptKey(t, updated, control, tea.KeyMsg{Type: tea.KeyEnter})
	entry := requireQuestionAnswerEntry(t, request)
	if entry.GetQuestionAnswer().Freeform == nil || *entry.GetQuestionAnswer().Freeform != "custom" {
		t.Fatalf("unexpected freeform answer: %+v", entry.GetQuestionAnswer())
	}
	if entry.GetQuestionAnswer().SelectedOptionNumber != nil {
		t.Fatalf("freeform submission without selection invented an option: %+v", request)
	}
	if testActiveAsk(updated) != nil {
		t.Fatal("successful batch did not immediately remove the ask")
	}
}

func TestAskQuestionRecommendedOptionStartsUnselectedAndEnterDoesNothing(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	event := testQuestionAskEvent("ask-1", "Pick one", "first", "second")
	recommended := int32(2)
	event.prompt.GetQuestion().RecommendedOptionIndex = &recommended

	updated := updateUIModel(t, m, askEventMsg{event: event})
	recommendedUnselected := false
	for _, line := range updated.askController().renderPriorityPromptLines() {
		if line.Kind == askPromptLineKindOption && line.Recommended {
			recommendedUnselected = !line.Selected
		}
	}
	if !recommendedUnselected {
		t.Fatal("recommended option did not remain visibly unselected")
	}

	next, command := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if command != nil || len(control.batchRequests) != 0 || testActiveAsk(updated) == nil || updated.ask.freeform {
		t.Fatal("Enter without a selected Question option changed or submitted the prompt")
	}
}

func TestAskQuestionDownSelectsFirstOption(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	event := testQuestionAskEvent("ask-1", "Pick one", "first", "second")
	recommended := int32(2)
	event.prompt.GetQuestion().RecommendedOptionIndex = &recommended
	updated := updateUIModel(t, m, askEventMsg{event: event})

	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyDown})
	lines := updated.askController().renderPriorityPromptLines()
	if len(lines) < 2 || lines[0].Kind != askPromptLineKindOption || !lines[0].Selected ||
		!lines[1].Recommended || lines[1].Selected {
		t.Fatalf("Down did not select the first answer independently of the recommendation: %+v", lines)
	}
	updated, request := submitAskPromptKey(t, updated, control, tea.KeyMsg{Type: tea.KeyEnter})
	entry := requireQuestionAnswerEntry(t, request)
	if entry.GetQuestionAnswer().SelectedOptionNumber == nil || *entry.GetQuestionAnswer().SelectedOptionNumber != 1 {
		t.Fatalf("Down answer = %+v, want first option", entry.GetQuestionAnswer())
	}
	if testActiveAsk(updated) != nil {
		t.Fatal("successful batch did not immediately remove the ask")
	}
}

func TestAskQuestionUpSelectsLastVisibleOption(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	event := testQuestionAskEvent("ask-1", "Pick one", "first", "second")
	recommended := int32(1)
	event.prompt.GetQuestion().RecommendedOptionIndex = &recommended
	updated := updateUIModel(t, m, askEventMsg{event: event})

	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyUp})
	lines := updated.askController().renderPriorityPromptLines()
	last := lines[len(lines)-2]
	if last.Kind != askPromptLineKindOption || !last.Selected || last.Option == nil ||
		*last.Option != len(event.prompt.GetQuestion().GetSuggestions()) {
		t.Fatalf("Up did not select the last visible option: %+v", lines)
	}
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyUp})
	lines = updated.askController().renderPriorityPromptLines()
	if !lines[1].Selected {
		t.Fatalf("Up from the last option did not move to the preceding answer: %+v", lines)
	}
	updated, request := submitAskPromptKey(t, updated, control, tea.KeyMsg{Type: tea.KeyEnter})
	entry := requireQuestionAnswerEntry(t, request)
	if entry.GetQuestionAnswer().SelectedOptionNumber == nil || *entry.GetQuestionAnswer().SelectedOptionNumber != 2 {
		t.Fatalf("Up answer = %+v, want second option", entry.GetQuestionAnswer())
	}
	if testActiveAsk(updated) != nil {
		t.Fatal("successful batch did not immediately remove the ask")
	}
}

func TestQueuedAskQuestionStartsUnselectedWhenPromoted(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	updated := updateUIModel(t, m, askEventMsg{event: testQuestionAskEvent(
		"ask-a",
		"First question",
		"first",
		"second",
	)})
	queued := testQuestionAskEvent("ask-b", "Second question", "first", "second")
	recommended := int32(2)
	queued.prompt.GetQuestion().RecommendedOptionIndex = &recommended
	updated = updateUIModel(t, updated, askEventMsg{event: queued})
	if updated.ask.current == nil || transcriptPromptToolCallID(updated.ask.current.prompt) != "ask-a" ||
		updated.ask.selectedOption != nil {
		t.Fatalf(
			"queued question changed active selection: current=%+v selection=%v",
			updated.ask.current,
			updated.ask.selectedOption,
		)
	}

	updated = updateUIModel(t, updated, askEventMsg{event: askEvent{resolvedToolCallID: "ask-a"}})
	recommendedUnselected := false
	for _, line := range updated.askController().renderPriorityPromptLines() {
		if line.Kind == askPromptLineKindOption && line.Recommended {
			recommendedUnselected = !line.Selected
		}
	}
	if !recommendedUnselected {
		t.Fatal("promoted question initialized its recommendation as selected")
	}
	next, command := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if command != nil || len(control.batchRequests) != 0 || !updated.ask.hasCurrent() {
		t.Fatal("Enter without a promoted Question selection submitted an answer")
	}
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyDown})
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyDown})
	if updated.ask.selectedOption == nil || *updated.ask.selectedOption != 1 {
		t.Fatal("explicit navigation did not select the promoted recommendation")
	}

	updated, request := submitAskPromptKey(t, updated, control, tea.KeyMsg{Type: tea.KeyEnter})
	entry := requireQuestionAnswerEntry(t, request)
	if entry.GetQuestionAnswer().SelectedOptionNumber == nil || *entry.GetQuestionAnswer().SelectedOptionNumber != recommended {
		t.Fatalf("selected option = %+v, want %d", entry.GetQuestionAnswer().SelectedOptionNumber, recommended)
	}
	if testActiveAsk(updated) != nil {
		t.Fatal("successful batch did not immediately remove the ask")
	}
}

func TestAskQuestionPickerSubmitPreservesPendingFreeformDraft(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	event := testQuestionAskEvent("ask-1", "Pick one", "a", "b")

	updated := updateUIModel(t, m, askEventMsg{event: event})
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyDown})
	next, _ := updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("custom")})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated = next.(*uiModel)

	if testAskFreeform(updated) {
		t.Fatal("expected tab to return to picker mode")
	}
	if testAskInput(updated) != "custom" {
		t.Fatalf("expected pending freeform draft preserved, got %q", testAskInput(updated))
	}
	promptLines := updated.askController().renderPriorityPromptLines()
	hasDisabledDraftPreview := false
	hasHintLine := false
	for _, line := range promptLines {
		if line.Kind == askPromptLineKindInput && line.Disabled && line.InputEditor.Text() == "custom" {
			hasDisabledDraftPreview = true
		}
		if line.Kind == askPromptLineKindHint {
			hasHintLine = true
		}
	}
	if !hasDisabledDraftPreview {
		t.Fatalf("expected disabled draft preview in picker, got %+v", promptLines)
	}
	if hasHintLine {
		t.Fatalf("expected draft preview to replace picker hint, got %+v", promptLines)
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	updated, request := submitAskPromptKey(t, updated, control, tea.KeyMsg{Type: tea.KeyEnter})
	entry := requireQuestionAnswerEntry(t, request)
	if entry.GetQuestionAnswer().SelectedOptionNumber == nil || *entry.GetQuestionAnswer().SelectedOptionNumber != 2 {
		t.Fatalf("expected selected option number 2, got %+v", request)
	}
	if entry.GetQuestionAnswer().Freeform == nil || *entry.GetQuestionAnswer().Freeform != "custom" {
		t.Fatalf("expected pending freeform draft submitted with picker answer, got %+v", request)
	}
	if testActiveAsk(updated) != nil {
		t.Fatal("successful batch did not immediately remove the ask")
	}
}

func TestAskQuestionTabRoundTripRestoresPendingFreeformDraftAndCursor(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	event := testQuestionAskEvent("ask-1", "Pick one", "a", "b")

	updated := updateUIModel(t, m, askEventMsg{event: event})
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyDown})
	next, _ := updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("custom")})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyLeft})
	updated = next.(*uiModel)
	wantCursor := testAskInputCursor(updated)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated = next.(*uiModel)

	if !testAskFreeform(updated) {
		t.Fatal("expected tab to restore freeform editing")
	}
	if selection := testAskSelection(updated); selection == nil || *selection != 1 {
		t.Fatalf("expected changed picker selection preserved, got %v", selection)
	}
	if testAskInput(updated) != "custom" {
		t.Fatalf("expected pending freeform draft restored, got %q", testAskInput(updated))
	}
	if testAskInputCursor(updated) != wantCursor {
		t.Fatalf("expected freeform cursor restored, got %d want %d", testAskInputCursor(updated), wantCursor)
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("X")})
	updated = next.(*uiModel)
	updated, request := submitAskPromptKey(t, updated, control, tea.KeyMsg{Type: tea.KeyEnter})
	entry := requireQuestionAnswerEntry(t, request)
	if entry.GetQuestionAnswer().SelectedOptionNumber == nil || *entry.GetQuestionAnswer().SelectedOptionNumber != 2 {
		t.Fatalf("expected selected option number 2 after round-trip, got %+v", request)
	}
	if entry.GetQuestionAnswer().Freeform == nil || *entry.GetQuestionAnswer().Freeform != "custoXm" {
		t.Fatalf("expected restored draft to remain editable, got %+v", request)
	}
	if testActiveAsk(updated) != nil {
		t.Fatal("successful batch did not immediately remove the ask")
	}
}

func TestAskQuestionFreeformSelectionEnterDropsIntoFreeformWhenEmpty(t *testing.T) {
	m := newProjectedStaticUIModel()
	event := testQuestionAskEvent("ask-1", "Pick one", "a", "b")

	updated := updateUIModel(t, m, askEventMsg{event: event})
	next, _ := updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)

	if cmd != nil {
		t.Fatal("did not expect validation error when opening freeform from Freeform answer")
	}
	if !testAskFreeform(updated) {
		t.Fatal("expected Freeform answer to switch into freeform mode")
	}
	if updated.transientStatus != "" {
		t.Fatalf("did not expect transient status while opening freeform, got %q", updated.transientStatus)
	}
	if testActiveAsk(updated) == nil {
		t.Fatal("expected ask to remain active after switching to freeform")
	}
}

func TestAskQuestionFreeformSelectionEmptySubmitRequiresCommentary(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	event := testQuestionAskEvent("ask-1", "Pick one", "a", "b")

	updated := updateUIModel(t, m, askEventMsg{event: event})
	next, _ := updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	next, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)

	if cmd == nil {
		t.Fatal("expected transient error status cmd")
	}
	if strings.TrimSpace(updated.transientStatus) == "" {
		t.Fatal("expected non-empty transient validation status")
	}
	if updated.transientStatusKind != uiStatusNoticeError {
		t.Fatalf("expected error notice kind, got %d", updated.transientStatusKind)
	}
	if testActiveAsk(updated) == nil {
		t.Fatal("expected ask to remain active after validation error")
	}
	select {
	case request := <-control.batchRequests:
		t.Fatalf("did not expect request on validation error, got %+v", request)
	default:
	}
}

func TestAskQuestionFreeformSelectionSubmitsFreeformOnly(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	event := testQuestionAskEvent("ask-1", "Pick one", "a", "b")

	updated := updateUIModel(t, m, askEventMsg{event: event})
	next, _ := updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyDown})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("custom")})
	updated = next.(*uiModel)
	updated, request := submitAskPromptKey(t, updated, control, tea.KeyMsg{Type: tea.KeyEnter})
	entry := requireQuestionAnswerEntry(t, request)
	if entry.GetQuestionAnswer().SelectedOptionNumber != nil {
		t.Fatalf("expected freeform selection to submit without selected option number, got %+v", request)
	}
	if entry.GetQuestionAnswer().Freeform == nil || *entry.GetQuestionAnswer().Freeform != "custom" {
		t.Fatalf("unexpected freeform selection response: %+v", request)
	}
	if testActiveAsk(updated) != nil {
		t.Fatal("successful batch did not immediately remove the ask")
	}
}

func TestAskFreeformUsesMainEditingStack(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	event := testQuestionAskEvent("ask-1", "Type answer")

	updated := updateUIModel(t, m, askEventMsg{event: event})
	if !testAskFreeform(updated) {
		t.Fatal("expected freeform ask input")
	}

	next, _ := updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello world")})
	updated = next.(*uiModel)
	for range 5 {
		next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyLeft})
		updated = next.(*uiModel)
	}
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("_")})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyHome})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(">")})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyEnd})
	updated = next.(*uiModel)
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	updated = next.(*uiModel)
	updated, request := submitAskPromptKey(t, updated, control, tea.KeyMsg{Type: tea.KeyEnter})
	entry := requireQuestionAnswerEntry(t, request)
	if entry.GetQuestionAnswer().Freeform == nil || *entry.GetQuestionAnswer().Freeform != ">hello _worl" {
		t.Fatalf("unexpected inline edit result: %+v", entry.GetQuestionAnswer())
	}
	if testActiveAsk(updated) != nil {
		t.Fatal("successful batch did not immediately remove the ask")
	}
}

func TestAskFreeformCtrlUEditingMatchesMainInput(t *testing.T) {
	m := newProjectedStaticUIModel()
	event := testQuestionAskEvent("ask-1", "Type answer")

	updated := updateUIModel(t, m, askEventMsg{event: event})
	testSetAskInputAtRuneCursor(updated, "top\ncurrent\nbottom", len([]rune("top\ncur")))

	next, _ := updated.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	updated = next.(*uiModel)

	if goruntime.GOOS == "darwin" {
		if testAskInput(updated) != "top\nbottom" {
			t.Fatalf("expected ctrl+u to delete current ask line on darwin, got %q", testAskInput(updated))
		}
		if testAskInputRuneCursor(updated) != len([]rune("top\n")) {
			t.Fatalf("expected cursor at joined ask line on darwin, got %d", testAskInputRuneCursor(updated))
		}
		return
	}
	if testAskInput(updated) != "top\nrent\nbottom" {
		t.Fatalf("expected ctrl+u to kill to ask line start, got %q", testAskInput(updated))
	}
	if testAskInputRuneCursor(updated) != len([]rune("top\n")) {
		t.Fatalf("expected cursor at ask line start, got %d", testAskInputRuneCursor(updated))
	}
}

func TestApprovalAskUsesSingleDenyOptionAndTabCommentary(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	m.setRuntimeActivityBusyForTest(true)
	event := testApprovalAskEvent(
		"approval-1",
		"Approve?",
		clientui.ApprovalDecisionAllowOnce,
		clientui.ApprovalDecisionAllowSession,
		clientui.ApprovalDecisionDeny,
	)

	updated := updateUIModel(t, m, askEventMsg{event: event})
	promptLines := updated.askController().renderPriorityPromptLines()
	optionLines := 0
	hintLines := 0
	for _, line := range promptLines {
		if line.Kind == askPromptLineKindOption {
			optionLines++
		}
		if line.Kind == askPromptLineKindHint {
			hintLines++
		}
	}
	if optionLines != 3 {
		t.Fatalf("expected exactly 3 approval options, got %+v", promptLines)
	}
	if hintLines != 1 {
		t.Fatalf("expected one approval picker hint line, got %+v", promptLines)
	}

	for i := 0; i < 3; i++ {
		next, _ := updated.Update(tea.KeyMsg{Type: tea.KeyDown})
		updated = next.(*uiModel)
	}
	next, _ := updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated = next.(*uiModel)
	if !testAskFreeform(updated) {
		t.Fatal("expected tab on deny selection to switch to commentary input")
	}
	promptLines = updated.askController().renderPriorityPromptLines()
	if len(promptLines) != 2 || promptLines[0].Kind != askPromptLineKindHint || promptLines[1].Kind != askPromptLineKindInput {
		t.Fatalf("expected commentary prompt to collapse to hint+input, got %+v", promptLines)
	}
	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("blocked by policy")})
	updated = next.(*uiModel)
	next, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if cmd == nil {
		t.Fatal("deny commentary did not create a direct approval delivery command")
	}
	updated = runPromptDeliveryCommand(t, updated, cmd)
	request := requirePromptAnswerBatchRequest(t, control)
	entry := requireApprovalAnswerEntry(t, request)
	if entry.GetApprovalAnswer().Decision != promptpb.ApprovalDecision_APPROVAL_DECISION_DENY ||
		entry.GetApprovalAnswer().Commentary == nil ||
		*entry.GetApprovalAnswer().Commentary != "blocked by policy" {
		t.Fatalf("unexpected approval request: %+v", request)
	}
	if len(updated.injectedQueue) != 0 {
		t.Fatalf("deny commentary created a duplicate queued user message: %+v", updated.injectedQueue)
	}
	if testActiveAsk(updated) != nil {
		t.Fatal("successful batch did not immediately remove the Approval")
	}
}

func TestOutsideWorkspaceApprovalRendersDistinctSuppliedPathsBeforeOptions(t *testing.T) {
	event := testApprovalAskEvent("approval-paths", "", clientui.ApprovalDecisionAllowOnce, clientui.ApprovalDecisionDeny)
	targets := []clientui.FileAccessTarget{
		{RequestedPath: "/alias/a", ResolvedPath: "/real/file"},
		{RequestedPath: "/alias/b", ResolvedPath: "/real/file"},
		{RequestedPath: "/real/other", ResolvedPath: "/real/other"},
	}
	for _, target := range targets {
		event.prompt.GetApproval().AccessTargets = append(event.prompt.GetApproval().AccessTargets, &promptpb.FileAccessTarget{RequestedPath: target.RequestedPath, ResolvedPath: target.ResolvedPath})
	}
	m := sizedTestUIModel(newProjectedStaticUIModel(), 100, 24)
	next, projection := m.Update(askEventMsg{event: event})
	viewModel := updateUIModel(t, next.(*uiModel), projection())
	identity, ok := viewModel.currentQuestionRenderIdentity()
	if !ok {
		t.Fatal("outside-workspace Approval has no render identity")
	}
	want := clientui.FormatFileAccessApprovalMarkdown(targets)
	if identity.questionSource != want {
		t.Fatalf("Approval Markdown = %q, want %q", identity.questionSource, want)
	}
	view := stripANSIAndTrimRight(viewModel.View())
	previous := -1
	for _, text := range []string{"/alias/a → /real/file", "/alias/b → /real/file", "/real/other"} {
		index := strings.Index(view, text)
		if index <= previous {
			t.Fatalf("Approval disclosure is missing or out of order at %q:\n%s", text, view)
		}
		previous = index
	}
}

func TestApprovalAskStartsUnselectedAndRequiresDownBeforeEnter(t *testing.T) {
	tests := []struct {
		name            string
		decisions       []clientui.ApprovalDecision
		wantFirstChoice promptpb.ApprovalDecision
	}{
		{
			name: "allow once is not implicitly selected",
			decisions: []clientui.ApprovalDecision{
				clientui.ApprovalDecisionDeny,
				clientui.ApprovalDecisionAllowSession,
				clientui.ApprovalDecisionAllowOnce,
			},
			wantFirstChoice: promptpb.ApprovalDecision_APPROVAL_DECISION_DENY,
		},
		{
			name: "first decision requires explicit navigation",
			decisions: []clientui.ApprovalDecision{
				clientui.ApprovalDecisionAllowSession,
				clientui.ApprovalDecisionDeny,
			},
			wantFirstChoice: promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_SESSION,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m, control := newProjectedPromptTestUIModel(t)
			updated := updateUIModel(t, m, askEventMsg{event: testApprovalAskEvent(
				"approval-1",
				"Approve?",
				test.decisions...,
			)})

			for _, line := range updated.askController().renderPriorityPromptLines() {
				if line.Kind == askPromptLineKindOption && line.Selected {
					t.Fatal("Approval decision was selected before operator input")
				}
			}
			next, command := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
			updated = next.(*uiModel)
			if command != nil || len(control.batchRequests) != 0 || !updated.ask.hasCurrent() {
				t.Fatal("Enter without an Approval decision submitted an answer")
			}
			updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyDown})
			if updated.ask.selectedOption == nil || *updated.ask.selectedOption != 0 {
				t.Fatal("Down did not select the first Approval decision")
			}
			next, command = updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
			updated = runPromptDeliveryCommand(t, next.(*uiModel), command)
			request := requirePromptAnswerBatchRequest(t, control)
			entry := requireApprovalAnswerEntry(t, request)
			if entry.GetApprovalAnswer().Decision != test.wantFirstChoice || entry.GetApprovalAnswer().Commentary != nil {
				t.Fatalf(
					"approval answer = %+v, want first offered decision %q without commentary",
					entry.GetApprovalAnswer(),
					test.wantFirstChoice,
				)
			}
		})
	}
}

func TestApprovalAskAllowsCommentaryBeforeDecision(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	updated := updateUIModel(t, m, askEventMsg{event: testApprovalAskEvent(
		"approval-1",
		"Approve?",
		clientui.ApprovalDecisionAllowOnce,
		clientui.ApprovalDecisionDeny,
	)})
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyTab})
	if !updated.ask.freeform || updated.ask.freeformMode != askFreeformModeApprovalCommentary {
		t.Fatal("Tab did not open Approval commentary without a selected decision")
	}
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("explain first")})
	next, command := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if command != nil || len(control.batchRequests) != 0 || !updated.ask.hasCurrent() {
		t.Fatal("Approval commentary submitted without a selected decision")
	}
	if testAskInput(updated) != "explain first" {
		t.Fatal("Enter without a decision discarded drafted commentary")
	}
	hasInput := false
	for _, line := range updated.askController().renderPriorityPromptLines() {
		hasInput = hasInput || line.Kind == askPromptLineKindInput
	}
	if !hasInput {
		t.Fatal("Approval commentary input is not visible before choosing a decision")
	}
}

func TestApprovalAskReturnsToDecisionPickerWithDraft(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	updated := updateUIModel(t, m, askEventMsg{event: testApprovalAskEvent(
		"approval-1",
		"Approve?",
		clientui.ApprovalDecisionAllowOnce,
		clientui.ApprovalDecisionDeny,
	)})
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyTab})
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("explain first")})
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyTab})

	if updated.ask.freeform || updated.ask.selectedOption != nil || testAskInput(updated) != "explain first" {
		t.Fatal("returning to the Approval picker did not retain the unbound commentary draft")
	}
	hasDisabledDraft := false
	for _, line := range updated.askController().renderPriorityPromptLines() {
		hasDisabledDraft = hasDisabledDraft ||
			(line.Kind == askPromptLineKindInput && line.Disabled && line.InputEditor.Text() == "explain first")
	}
	if !hasDisabledDraft {
		t.Fatal("Approval picker did not present the pending commentary draft")
	}
	if len(control.batchRequests) != 0 {
		t.Fatal("Approval picker navigation submitted the prompt")
	}
}

func TestApprovalAskSelectionConfirmsWithDraftedCommentary(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	updated := updateUIModel(t, m, askEventMsg{event: testApprovalAskEvent(
		"approval-1",
		"Approve?",
		clientui.ApprovalDecisionAllowOnce,
		clientui.ApprovalDecisionDeny,
	)})
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyTab})
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("explain first")})
	next, command := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if command != nil || len(control.batchRequests) != 0 || !updated.ask.hasCurrent() {
		t.Fatal("Enter without an Approval decision submitted drafted commentary")
	}
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyTab})
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyDown})
	if updated.ask.selectedOption == nil || *updated.ask.selectedOption != 0 ||
		testAskInput(updated) != "explain first" || len(control.batchRequests) != 0 {
		t.Fatal("selecting the first Approval decision lost commentary or submitted it")
	}
	updated, request := submitAskPromptKey(t, updated, control, tea.KeyMsg{Type: tea.KeyEnter})
	entry := requireApprovalAnswerEntry(t, request)
	if entry.GetApprovalAnswer().Decision != promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_ONCE ||
		entry.GetApprovalAnswer().Commentary == nil ||
		*entry.GetApprovalAnswer().Commentary != "explain first" {
		t.Fatalf("Approval answer did not include the selected decision and commentary: %+v", entry.GetApprovalAnswer())
	}
	if testActiveAsk(updated) != nil {
		t.Fatal("successful batch did not immediately remove the Approval")
	}
}

func TestApprovalAskUpSelectsLastDecision(t *testing.T) {
	m, control := newProjectedPromptTestUIModel(t)
	updated := updateUIModel(t, m, askEventMsg{event: testApprovalAskEvent(
		"approval-1",
		"Approve?",
		clientui.ApprovalDecisionAllowOnce,
		clientui.ApprovalDecisionDeny,
	)})
	updated = updateUIModel(t, updated, tea.KeyMsg{Type: tea.KeyUp})
	lines := updated.askController().renderPriorityPromptLines()
	if len(lines) < 2 || !lines[len(lines)-2].Selected {
		t.Fatalf("Up did not select the last Approval decision: %+v", lines)
	}
	updated, request := submitAskPromptKey(t, updated, control, tea.KeyMsg{Type: tea.KeyEnter})
	entry := requireApprovalAnswerEntry(t, request)
	if entry.GetApprovalAnswer().Decision != promptpb.ApprovalDecision_APPROVAL_DECISION_DENY {
		t.Fatalf("Up selected Approval decision %v, want Deny", entry.GetApprovalAnswer().Decision)
	}
	if testActiveAsk(updated) != nil {
		t.Fatal("successful batch did not immediately remove the Approval")
	}
}

func TestDetailModeHidesInputBox(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.terminalGeometry = terminalGeometryKnown(80, 16)
	testSetMainInput(m, "draft input should be hidden")
	m.layout().syncViewport()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	updated := next.(*uiModel)
	if updated.view.Mode() != tui.ModeDetail {
		t.Fatalf("mode=%q want detail", updated.view.Mode())
	}

	view := ansi.Strip(updated.View())
	if strings.Contains(view, "draft input should be hidden") {
		t.Fatalf("expected detail mode to hide input text, got %q", view)
	}
	if strings.Contains(view, "› ") {
		t.Fatalf("expected detail mode to hide input prompt, got %q", view)
	}
}

func TestTabInsideDetailReturnsToOngoingMode(t *testing.T) {
	m := newProjectedStaticUIModel()

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyShiftTab})
	updated := next.(*uiModel)
	if updated.view.Mode() != tui.ModeDetail {
		t.Fatalf("mode=%q want detail", updated.view.Mode())
	}

	next, _ = updated.Update(tea.KeyMsg{Type: tea.KeyTab})
	updated = next.(*uiModel)
	if updated.view.Mode() != tui.ModeOngoing {
		t.Fatalf("mode=%q want ongoing after Tab", updated.view.Mode())
	}
}

func TestDetailModeStatusLineOmitsActionForCompleteAssistantRow(t *testing.T) {
	page := &transcriptpb.Page{SessionId: detailTestSessionID,
		Entries: []*transcriptpb.CommittedRow{
			detailTestAssistantRow("line one\nline two\nline three\nline four")}, ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH}
	m := newProjectedStaticUIModel(
		WithUISessionID(detailTestSessionID),
		WithUIModelName("gpt-6-sol"),
	)
	m.statusConfig.SessionViews = &countingSessionViewClient{page: page}
	m.terminalGeometry = terminalGeometryKnown(100, 16)
	m.layout().syncViewport()

	cmd := m.transitionTranscriptModeWithOptions(transcriptModeTransitionOptions{
		target:            tui.ModeDetail,
		suppressAltScreen: true,
		preserveSurface:   true,
	})
	updated := m
	for _, msg := range collectCmdMessages(t, cmd) {
		if load, ok := msg.(detailTranscriptLoadMsg); ok {
			updated = updateUIModel(t, updated, load)
		}
	}
	if updated.view.Mode() != tui.ModeDetail {
		t.Fatalf("mode=%q want detail", updated.view.Mode())
	}
	if got := updated.view.DetailSelectionAction(); got != tui.DetailSelectionActionNone {
		t.Fatalf("detail selection action = %v, want no action for complete row", got)
	}

	next, _ := updated.Update(tea.KeyMsg{Type: tea.KeyEnter})
	updated = next.(*uiModel)
	if got := updated.view.DetailSelectionAction(); got != tui.DetailSelectionActionNone {
		t.Fatalf("detail selection action after Enter = %v, want no action for complete row", got)
	}
}

func TestAskQuestionMarkdownPromptCursorTracksInputAfterExpandedQuestion(t *testing.T) {
	question := strings.Join([]string{
		"Review **this plan** before answer:",
		"",
		"- First item",
		"- Second item",
	}, "\n")
	m := newProjectedStaticUIModel()
	m.terminalGeometry = terminalGeometryKnown(64, 12)
	m.layout().syncViewport()
	event := testQuestionAskEvent("ask-1", question)
	testSetActiveAsk(m, &event)
	testSetAskInput(m, "typed")

	wrapped, cursorLine := testAskPaneContent(m, 64)
	if cursorLine == nil || *cursorLine >= len(wrapped) {
		t.Fatalf("expected cursor line in wrapped prompt, got %v of %d", cursorLine, len(wrapped))
	}
	if wrapped[*cursorLine].prompt.Kind != askPromptLineKindInput {
		t.Fatalf("expected cursor to land on input after markdown-expanded question, got line %+v", wrapped[*cursorLine])
	}

	visible, visibleCursor := testVisibleAskPaneContent(m, 64)
	if visibleCursor == nil || *visibleCursor >= len(visible) {
		t.Fatalf("expected cursor line in visible prompt, got %v of %d", visibleCursor, len(visible))
	}
	if visible[*visibleCursor].prompt.Kind != askPromptLineKindInput {
		t.Fatalf("expected visible cursor to land on input after markdown-expanded question, got line %+v", visible[*visibleCursor])
	}
}

func TestAskQuestionMarkdownLinksWrapIntoIndependentBoundedRows(t *testing.T) {
	const target = "https://github.com/org/repo/pull/456"
	for _, presentation := range []struct {
		name           string
		links          transcriptrender.MarkdownLinkPresentation
		wantLinkedText string
	}{
		{
			name:           "supported terminal",
			links:          transcriptrender.MarkdownLinkLabelOnly,
			wantLinkedText: "PR #456",
		},
		{
			name:           "fallback terminal",
			links:          transcriptrender.MarkdownLinkLabelAndDestination,
			wantLinkedText: "PR #456" + target,
		},
	} {
		t.Run(presentation.name, func(t *testing.T) {
			m := newProjectedStaticUIModel(
				WithUIMarkdownLinkPresentation(presentation.links),
			)
			m.terminalGeometry = terminalGeometryKnown(12, 12)
			m.layout().syncViewport()
			event := testQuestionAskEvent(
				"ask-1",
				"[PR #456](https://github.com/org/repo/pull/456)",
			)
			testSetActiveAsk(m, &event)
			testSetAskInput(m, "answer")

			wrapped, cursorLine := testAskPaneContent(m, 12)
			if cursorLine == nil || wrapped[*cursorLine].prompt.Kind != askPromptLineKindInput {
				t.Fatalf("cursor=%v should remain on answer input: %+v", cursorLine, wrapped)
			}

			var linked strings.Builder
			for _, line := range wrapped {
				if width := lipgloss.Width(line.text); width > 12 {
					t.Fatalf("line width=%d want <=12: %+v", width, line)
				}
				trace := tuitest.TraceTerminalHyperlinks(t, line.text)
				if line.prompt.Kind == askPromptLineKindQuestion {
					linked.WriteString(trace.LinkedText(target))
					continue
				}
				if len(trace.Events) != 0 {
					t.Fatalf("non-question row inherited hyperlink metadata: %+v", line)
				}
			}
			if got := linked.String(); got != presentation.wantLinkedText {
				t.Fatalf("linked visible content = %q, want %q", got, presentation.wantLinkedText)
			}
		})
	}
}

func TestAskQuestionPlainPRReferenceDoesNotCreateHyperlink(t *testing.T) {
	m := newProjectedStaticUIModel()
	event := testQuestionAskEvent("ask-1", "PR #456")
	testSetActiveAsk(m, &event)

	wrapped, _ := testAskPaneContent(m, 12)
	for _, line := range wrapped {
		if line.prompt.Kind != askPromptLineKindQuestion {
			continue
		}
		if trace := tuitest.TraceTerminalHyperlinks(t, line.text); len(trace.Events) != 0 {
			t.Fatalf("plain PR reference emitted hyperlink events: %+v", trace.Events)
		}
	}
}

func TestAskQuestionViewportPrioritizesAnswerOptionsOverQuestionLines(t *testing.T) {
	m := newProjectedStaticUIModel()
	m.terminalGeometry = terminalGeometryKnown(48, 9)
	m.layout().syncViewport()
	event := testQuestionAskEvent(
		"ask-1",
		strings.Repeat("Long **Markdown question** content. ", 8),
		"First",
		"Second",
	)
	testSetActiveAsk(m, &event)

	visible, _ := testVisibleAskPaneContent(m, 48)
	questionLines := 0
	optionLines := 0
	hintLines := 0
	for _, line := range visible {
		switch line.prompt.Kind {
		case askPromptLineKindQuestion:
			questionLines++
		case askPromptLineKindOption:
			optionLines++
		case askPromptLineKindHint:
			hintLines++
		}
	}
	if optionLines != 3 {
		t.Fatalf("visible option lines = %d, want all two suggestions plus freeform: %+v", optionLines, visible)
	}
	if hintLines != 1 {
		t.Fatalf("visible hint lines = %d, want 1: %+v", hintLines, visible)
	}
	if questionLines != 1 {
		t.Fatalf("visible question lines = %d, want remaining one-line capacity: %+v", questionLines, visible)
	}
}
