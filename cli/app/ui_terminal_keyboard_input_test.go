package app

import (
	"bytes"
	"io"
	"testing"

	"core/shared/clientui"
	tea "github.com/charmbracelet/bubbletea"
)

type terminalKeyInputTestModel struct {
	ui                   *uiModel
	stopAfterKeyCount    int
	processedKeyMessages int
}

func (m *terminalKeyInputTestModel) Init() tea.Cmd {
	return nil
}

func (m *terminalKeyInputTestModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.stopAfterKeyCount > 0 {
		if key, ok := msg.(tea.KeyMsg); ok && key.Type == tea.KeyCtrlC {
			return m, tea.Quit
		}
	}
	next, cmd := m.ui.Update(msg)
	m.ui = next.(*uiModel)
	if _, ok := msg.(tea.KeyMsg); ok && m.stopAfterKeyCount > 0 {
		m.processedKeyMessages++
		if m.processedKeyMessages == m.stopAfterKeyCount {
			return m, tea.Quit
		}
	}
	return m, cmd
}

func (*terminalKeyInputTestModel) View() string {
	return ""
}

func runTerminalInputProgram(t *testing.T, model *uiModel, inputBytes string) *uiModel {
	return runTerminalInputProgramAfterKeys(t, model, inputBytes, 0)
}

func runTerminalInputProgramAfterKeys(t *testing.T, model *uiModel, inputBytes string, keyCount int) *uiModel {
	t.Helper()
	t.Cleanup(model.Close)
	testModel := &terminalKeyInputTestModel{ui: model, stopAfterKeyCount: keyCount}
	program := tea.NewProgram(
		testModel,
		tea.WithInput(bytes.NewBufferString(inputBytes)),
		tea.WithOutput(io.Discard),
		tea.WithoutRenderer(),
		tea.WithFilter(terminalCursorProgramFilter(nil)),
	)

	final, err := program.Run()
	if err != nil {
		t.Fatalf("run terminal input program: %v", err)
	}
	return final.(*terminalKeyInputTestModel).ui
}

func TestProgramAltEnterInsertsNewline(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgram(t, model, "\x1b[13;3u\x03")
	if got, want := testMainInput(result), "draft\n"; got != want {
		t.Fatalf("input after Alt+Enter = %q, want %q", got, want)
	}
}

func TestProgramKittyShiftEnterInsertsNewline(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgramAfterKeys(t, model, "\x1b[13;2u\x03", 1)
	if got, want := testMainInput(result), "draft\n"; got != want {
		t.Fatalf("input after Kitty Shift+Enter = %q, want %q", got, want)
	}
}

func TestProgramXtermShiftEnterInsertsNewline(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgramAfterKeys(t, model, "\x1b[27;2;13~\x03", 1)
	if got, want := testMainInput(result), "draft\n"; got != want {
		t.Fatalf("input after xterm Shift+Enter = %q, want %q", got, want)
	}
	if result.isBusy() {
		t.Fatal("xterm Shift+Enter submitted the composer")
	}
}

func TestProgramXtermCtrlEnterSubmitsDraft(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgramAfterKeys(t, model, "\x1b[27;5;13~\x03", 1)
	if !result.hasPendingInputAdmission() {
		t.Fatal("xterm Ctrl+Enter did not submit the composer")
	}
}

func TestProgramAltEnterInsertsNewlineWithVisiblePathCompletion(t *testing.T) {
	model, _ := newPathReferenceTestModel("inspect @ab")
	model = deliverPathReferenceTestMatches(t, model, 1, []uiPathReferenceCandidate{{Path: "cli/app/ui.go"}})
	if !model.pathReferencePicker().visible {
		t.Fatal("expected a visible path completion")
	}

	result := runTerminalInputProgramAfterKeys(t, model, "\x1b[13;3u\x03", 1)
	if got, want := testMainInput(result), "inspect @ab\n"; got != want {
		t.Fatalf("input after Alt+Enter with path completion = %q, want %q", got, want)
	}
	if result.isBusy() {
		t.Fatal("Alt+Enter submitted the composer while path completion was visible")
	}
}

func TestProgramAltEnterInsertsNewlineWhilePathCompletionLoads(t *testing.T) {
	model, _ := newPathReferenceTestModel("inspect @ab")
	model.pathReference.loading = true
	if !model.pathReferencePicker().visible {
		t.Fatal("expected the path-completion loading state to be visible")
	}

	result := runTerminalInputProgramAfterKeys(t, model, "\x1b\r\x03", 1)
	if got, want := testMainInput(result), "inspect @ab\n"; got != want {
		t.Fatalf("input after Alt+Enter while path completion loads = %q, want %q", got, want)
	}
	if result.isBusy() {
		t.Fatal("Alt+Enter submitted the composer while path completion was loading")
	}
}

func TestProgramAltEnterInsertsNewlineInFreeformQuestion(t *testing.T) {
	model := sizedTestUIModel(newProjectedStaticUIModel(), 64, 20)
	next, projectionCmd := model.Update(askEventMsg{event: testQuestionAskEvent("ask-alt-enter", "Explain?")})
	model = next.(*uiModel)
	if projectionCmd == nil {
		t.Fatal("question did not schedule its projection")
	}
	model = updateUIModel(t, model, projectionCmd())
	if !model.ask.freeform {
		t.Fatal("question did not open freeform input")
	}
	testSetAskInput(model, "answer")

	result := runTerminalInputProgramAfterKeys(t, model, "\x1b\r\x03", 1)
	if got, want := testAskInput(result), "answer\n"; got != want {
		t.Fatalf("freeform answer after Alt+Enter = %q, want %q", got, want)
	}
	if result.ask.current == nil || result.ask.activeDelivery != nil {
		t.Fatal("Alt+Enter submitted or closed the freeform Question")
	}
}

func TestProgramAltEnterInsertsNewlineInApprovalCommentary(t *testing.T) {
	model := sizedTestUIModel(newProjectedStaticUIModel(), 64, 20)
	next, projectionCmd := model.Update(askEventMsg{event: testApprovalAskEvent(
		"approval-alt-enter",
		"Allow?",
		clientui.ApprovalDecisionAllowOnce,
		clientui.ApprovalDecisionDeny,
	)})
	model = next.(*uiModel)
	if projectionCmd == nil {
		t.Fatal("Approval did not schedule its projection")
	}
	model = updateUIModel(t, model, projectionCmd())
	next, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = next.(*uiModel)
	if !model.ask.freeform || model.ask.freeformMode != askFreeformModeApprovalCommentary {
		t.Fatal("Approval did not open commentary input")
	}
	testSetAskInput(model, "reason")

	result := runTerminalInputProgramAfterKeys(t, model, "\x1b[13;3u\x03", 1)
	if got, want := testAskInput(result), "reason\n"; got != want {
		t.Fatalf("Approval commentary after Alt+Enter = %q, want %q", got, want)
	}
	if result.ask.current == nil || result.ask.activeDelivery != nil {
		t.Fatal("Alt+Enter submitted or closed the Approval")
	}
}

func TestProgramDecodedAltEnterInsertsNewline(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgram(t, model, "\x1b\r\x03")
	if got, want := testMainInput(result), "draft\n"; got != want {
		t.Fatalf("input after decoded Alt+Enter = %q, want %q", got, want)
	}
}

func TestProgramEnhancedCtrlJInsertsNewline(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgram(t, model, "\x1b[106;5u\x03")
	if got, want := testMainInput(result), "draft\n"; got != want {
		t.Fatalf("input after enhanced Ctrl+J = %q, want %q", got, want)
	}
}

func TestProgramKittyAssociatedTextInsertsPlainRune(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgram(t, model, "\x1b[97;1;97u\x03")
	if got, want := testMainInput(result), "drafta"; got != want {
		t.Fatalf("input after Kitty text event = %q, want %q", got, want)
	}
}

func TestProgramKittyAssociatedTextPreservesShiftedRune(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgram(t, model, "\x1b[97;2;65u\x03")
	if got, want := testMainInput(result), "draftA"; got != want {
		t.Fatalf("input after Kitty shifted text event = %q, want %q", got, want)
	}
}

func TestProgramKittyAssociatedTextPreservesMultipleCodepoints(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgram(t, model, "\x1b[0;;97:769u\x03")
	if got, want := testMainInput(result), "drafta\u0301"; got != want {
		t.Fatalf("input after Kitty multi-codepoint text event = %q, want %q", got, want)
	}
}

func TestProgramKittyKeyWithoutAssociatedTextDoesNotInventText(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgram(t, model, "\x1b[97;2u\x03")
	if got, want := testMainInput(result), "draft"; got != want {
		t.Fatalf("input after Kitty key event without associated text = %q, want %q", got, want)
	}
}

func TestProgramKittyAssociatedTextPreservesSpace(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgram(t, model, "\x1b[32;1;32u\x03")
	if got, want := testMainInput(result), "draft "; got != want {
		t.Fatalf("input after Kitty Space text event = %q, want %q", got, want)
	}
}

func TestProgramEnhancedCtrlJWithLockModifiersInsertsNewline(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "caps lock", input: "\x1b[106;69u\x03"},
		{name: "num lock", input: "\x1b[106;133u\x03"},
		{name: "caps and num lock", input: "\x1b[106;197u\x03"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := newProjectedStaticUIModel()
			testSetMainInput(model, "draft")
			result := runTerminalInputProgram(t, model, test.input)
			if got, want := testMainInput(result), "draft\n"; got != want {
				t.Fatalf("input after enhanced Ctrl+J with lock modifiers = %q, want %q", got, want)
			}
		})
	}
}

func TestProgramEnhancedCtrlWDeletesPreviousWord(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "alpha beta gamma")
	result := runTerminalInputProgram(t, model, "\x1b[119;5u\x03")
	if got, want := testMainInput(result), "alpha beta "; got != want {
		t.Fatalf("input after enhanced Ctrl+W = %q, want %q", got, want)
	}
}

func TestProgramKeypadEnterSubmitsLikeEnter(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgramAfterKeys(t, model, "\x1b[57414u\x03", 1)
	if !result.hasPendingInputAdmission() {
		t.Fatal("keypad Enter did not start submission")
	}
}

func TestProgramKittyPlainEnterSubmitsLikeEnter(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgramAfterKeys(t, model, "\x1b[13;1u\x03", 1)
	if !result.hasPendingInputAdmission() {
		t.Fatal("Kitty plain Enter did not start submission")
	}
}

func TestProgramKittyPlainTabQueuesLikeTab(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgramAfterKeys(t, model, "\x1b[9;1u\x03", 1)
	if !result.hasPendingInputAdmission() {
		t.Fatal("Kitty plain Tab did not start submission")
	}
}

func TestProgramKittyPlainBackspaceDeletesBackward(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "draft")
	result := runTerminalInputProgram(t, model, "\x1b[127;1u\x03")
	if got, want := testMainInput(result), "draf"; got != want {
		t.Fatalf("input after Kitty plain Backspace = %q, want %q", got, want)
	}
}

func TestProgramEnhancedCtrlWWithLockModifiersDeletesPreviousWord(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "caps lock", input: "\x1b[119;69u\x03"},
		{name: "num lock", input: "\x1b[119;133u\x03"},
		{name: "caps and num lock", input: "\x1b[119;197u\x03"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := newProjectedStaticUIModel()
			testSetMainInput(model, "alpha beta gamma")
			result := runTerminalInputProgram(t, model, test.input)
			if got, want := testMainInput(result), "alpha beta "; got != want {
				t.Fatalf("input after enhanced Ctrl+W with lock modifiers = %q, want %q", got, want)
			}
		})
	}
}

func TestProgramAltArrowMovesByWord(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		cursor     int
		wantCursor int
	}{
		{name: "left", input: "\x1b[1;3D\x03", cursor: len([]rune("alpha beta gamma")), wantCursor: len([]rune("alpha beta "))},
		{name: "right", input: "\x1b[1;3C\x03", cursor: len([]rune("alpha beta ")), wantCursor: len([]rune("alpha beta gamma"))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := newProjectedStaticUIModel()
			testSetMainInputAtRuneCursor(model, "alpha beta gamma", test.cursor)
			result := runTerminalInputProgram(t, model, test.input)
			if got, want := testMainInput(result), "alpha beta gamma"; got != want {
				t.Fatalf("input after Alt+arrow = %q, want unchanged text %q", got, want)
			}
			if got := testMainInputRuneCursor(result); got != test.wantCursor {
				t.Fatalf("cursor after Alt+arrow = %d, want %d", got, test.wantCursor)
			}
		})
	}
}

func TestProgramAltArrowWithCapsLockMovesByWord(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		cursor     int
		wantCursor int
	}{
		{name: "left", input: "\x1b[1;67D", cursor: len([]rune("alpha beta gamma")), wantCursor: len([]rune("alpha beta "))},
		{name: "right", input: "\x1b[1;67C", cursor: len([]rune("alpha beta ")), wantCursor: len([]rune("alpha beta gamma"))},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			model := newProjectedStaticUIModel()
			testSetMainInputAtRuneCursor(model, "alpha beta gamma", test.cursor)
			result := runTerminalInputProgramAfterKeys(t, model, test.input+"\x03", 1)
			if got, want := testMainInput(result), "alpha beta gamma"; got != want {
				t.Fatalf("input after Caps-Lock Alt+arrow = %q, want unchanged text %q", got, want)
			}
			if got := testMainInputRuneCursor(result); got != test.wantCursor {
				t.Fatalf("cursor after Caps-Lock Alt+arrow = %d, want %d", got, test.wantCursor)
			}
		})
	}
}

func TestProgramCapsLockLeftArrowMovesCursor(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInputAtRuneCursor(model, "alpha beta", len([]rune("alpha beta")))
	result := runTerminalInputProgramAfterKeys(t, model, "\x1b[1;65D\x03", 1)
	if got, want := testMainInputRuneCursor(result), len([]rune("alpha beta"))-1; got != want {
		t.Fatalf("cursor after Caps-Lock Left = %d, want %d", got, want)
	}
}

func TestProgramCapsLockRightArrowMovesCursor(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInputAtRuneCursor(model, "alpha beta", 0)
	result := runTerminalInputProgramAfterKeys(t, model, "\x1b[1;65C\x03", 1)
	if got, want := testMainInputRuneCursor(result), 1; got != want {
		t.Fatalf("cursor after Caps-Lock Right = %d, want %d", got, want)
	}
}

func TestProgramCapsLockCtrlLeftMovesByWord(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInputAtRuneCursor(model, "alpha beta gamma", len([]rune("alpha beta gamma")))
	result := runTerminalInputProgramAfterKeys(t, model, "\x1b[1;69D\x03", 1)
	if got, want := testMainInputRuneCursor(result), len([]rune("alpha beta ")); got != want {
		t.Fatalf("cursor after Caps-Lock Ctrl+Left = %d, want %d", got, want)
	}
}

func TestProgramCapsLockCtrlRightMovesByWord(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInputAtRuneCursor(model, "alpha beta gamma", len([]rune("alpha beta ")))
	result := runTerminalInputProgramAfterKeys(t, model, "\x1b[1;69C\x03", 1)
	if got, want := testMainInputRuneCursor(result), len([]rune("alpha beta gamma")); got != want {
		t.Fatalf("cursor after Caps-Lock Ctrl+Right = %d, want %d", got, want)
	}
}

func TestProgramAltBackspaceDeletesPreviousWord(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "alpha beta gamma")
	result := runTerminalInputProgram(t, model, "\x1b[127;3u\x03")
	if got, want := testMainInput(result), "alpha beta "; got != want {
		t.Fatalf("input after Alt+Backspace = %q, want %q", got, want)
	}
	if got, want := testMainInputRuneCursor(result), len([]rune("alpha beta ")); got != want {
		t.Fatalf("cursor after Alt+Backspace = %d, want %d", got, want)
	}
}

func TestProgramXtermAltBackspaceDeletesPreviousWord(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInput(model, "alpha beta gamma")
	result := runTerminalInputProgram(t, model, "\x1b[27;3;127~\x03")
	if got, want := testMainInput(result), "alpha beta "; got != want {
		t.Fatalf("input after xterm Alt+Backspace = %q, want %q", got, want)
	}
	if got, want := testMainInputRuneCursor(result), len([]rune("alpha beta ")); got != want {
		t.Fatalf("cursor after xterm Alt+Backspace = %d, want %d", got, want)
	}
}

func TestProgramXtermCtrlBackspaceDeletesCurrentLine(t *testing.T) {
	model := newProjectedStaticUIModel()
	testSetMainInputAtRuneCursor(model, "one\ntwo\nthree", 5)
	result := runTerminalInputProgram(t, model, "\x1b[27;5;127~\x03")
	if got, want := testMainInput(result), "one\nthree"; got != want {
		t.Fatalf("input after xterm Ctrl+Backspace = %q, want %q", got, want)
	}
	if got, want := testMainInputRuneCursor(result), 4; got != want {
		t.Fatalf("cursor after xterm Ctrl+Backspace = %d, want %d", got, want)
	}
}
