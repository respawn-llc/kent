package app

import (
	"fmt"
	"strings"
	"testing"

	"core/cli/tui/transcriptrender"
	tuitest "core/internal/testharness/pty"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestAskDocumentTallQuestionRetainsCompleteDocument(t *testing.T) {
	const (
		width  = 48
		height = 12
	)
	model := sizedTestUIModel(newProjectedStaticUIModel(), width, height)
	event := testQuestionAskEvent("ask-1", "Question source", "First", "Second")
	testSetActiveAsk(model, &event)
	rows := make([]string, 60)
	for index := range rows {
		rows[index] = fmt.Sprintf("question row %02d", index+1)
	}
	model.ask.activeProjection.rows = rows

	model = updateUIModel(t, model, tea.WindowSizeMsg{Width: width, Height: height})
	if model.surface() != uiSurfaceAsk {
		t.Fatal("tall question was not promoted")
	}
	document := model.layout().askDocument(width)
	optionRows := 0
	hintRows := 0
	questionRows := 0
	for _, line := range document.lines {
		switch line.prompt.Kind {
		case askPromptLineKindQuestion:
			questionRows++
		case askPromptLineKindOption:
			optionRows++
		case askPromptLineKindHint:
			hintRows++
		}
	}
	if optionRows != 3 || hintRows != 1 {
		t.Fatalf("priority rows = options %d hints %d, want all picker controls", optionRows, hintRows)
	}
	if questionRows != len(rows) {
		t.Fatalf("document retained %d of %d question rows", questionRows, len(rows))
	}
}

func TestAskDocumentMarkdownHyperlinksAreWidthSafe(t *testing.T) {
	const (
		target = "https://example.com/long-destination"
		width  = 12
		height = 9
	)
	for _, presentation := range []transcriptrender.MarkdownLinkPresentation{
		transcriptrender.MarkdownLinkLabelOnly,
		transcriptrender.MarkdownLinkLabelAndDestination,
	} {
		t.Run(fmt.Sprintf("presentation-%d", presentation), func(t *testing.T) {
			model := sizedTestUIModel(newProjectedStaticUIModel(
				WithUIMarkdownLinkPresentation(presentation),
			), width, height)
			question := strings.Repeat("[linked text]("+target+")\n", 8)
			event := testQuestionAskEvent("ask-1", question)
			testSetActiveAsk(model, &event)

			visible := model.layout().askDocument(width).lines
			questionRows := make([]uiInputPaneContentLine, 0)
			for _, line := range visible {
				if line.prompt.Kind == askPromptLineKindQuestion {
					questionRows = append(questionRows, line)
				}
			}
			if len(questionRows) == 0 {
				t.Fatal("document omitted question rows")
			}
			for _, line := range questionRows {
				if got := lipgloss.Width(line.text); got > width {
					t.Fatalf("question row width = %d, want <= %d", got, width)
				}
				tuitest.TraceTerminalHyperlinks(t, line.text+" plain")
			}
		})
	}
}

func TestAskDocumentTallMixedMarkdownRetainsAllRowsAcrossLinkPresentations(t *testing.T) {
	const (
		width  = 48
		height = 12
	)
	source := strings.Repeat(
		"Paragraph with wrapped words and a [link](https://example.com/destination).\n\n"+
			"- first list item\n- second list item\n\n"+
			"```go\nfmt.Println(\"bounded\")\n```\n\n",
		8,
	)
	for _, presentation := range []transcriptrender.MarkdownLinkPresentation{
		transcriptrender.MarkdownLinkLabelOnly,
		transcriptrender.MarkdownLinkLabelAndDestination,
	} {
		t.Run(fmt.Sprintf("presentation-%d", presentation), func(t *testing.T) {
			model := sizedTestUIModel(newProjectedStaticUIModel(
				WithUIMarkdownLinkPresentation(presentation),
			), width, height)
			next, command := model.Update(askEventMsg{event: testQuestionAskEvent(
				"ask-1",
				source,
				"First",
				"Second",
			)})
			pending := next.(*uiModel)
			next, _ = pending.Update(command())
			ready := next.(*uiModel)

			visible := ready.layout().askDocument(width).lines
			if ready.surface() != uiSurfaceAsk {
				t.Fatal("mixed Markdown prompt was not promoted")
			}
			questionRows := 0
			optionRows := 0
			for _, line := range visible {
				if got := lipgloss.Width(line.text); got > width {
					t.Fatalf("visible row width = %d, want <= %d", got, width)
				}
				switch line.prompt.Kind {
				case askPromptLineKindQuestion:
					questionRows++
					tuitest.TraceTerminalHyperlinks(t, line.text+" plain")
				case askPromptLineKindOption:
					optionRows++
				}
			}
			if questionRows != len(ready.ask.activeProjection.rows) || optionRows != 3 {
				t.Fatalf("bounded mixed Markdown rows = question %d options %d", questionRows, optionRows)
			}
		})
	}
}

func TestAskViewportPickerFreeformRoundTripPreservesBoundedDraftAndCursor(t *testing.T) {
	const (
		width  = 24
		height = 9
	)
	model := sizedTestUIModel(newProjectedStaticUIModel(), width, height)
	event := testQuestionAskEvent("ask-1", "Question source", "First", "Second")
	testSetActiveAsk(model, &event)
	model.ask.activeProjection.rows = []string{
		"question row one",
		"question row two",
		"question row three",
		"question row four",
	}

	next, _ := model.Update(tea.KeyMsg{Type: tea.KeyTab})
	freeform := next.(*uiModel)
	next, _ = freeform.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("draft text")})
	freeform = next.(*uiModel)
	next, _ = freeform.Update(tea.KeyMsg{Type: tea.KeyLeft})
	freeform = next.(*uiModel)
	wantEditorCursor := freeform.ask.editor.Cursor()
	freeformViewport := freeform.layout().askInputViewport(width, inputContentLineLimit(height))
	if !freeformViewport.cursor.Visible {
		t.Fatal("bounded freeform viewport hid the active editor cursor")
	}
	if len(freeformViewport.lines) > inputContentLineLimit(height) {
		t.Fatalf("freeform viewport rows = %d, content budget = %d", len(freeformViewport.lines), inputContentLineLimit(height))
	}

	next, _ = freeform.Update(tea.KeyMsg{Type: tea.KeyTab})
	picker := next.(*uiModel)
	pickerViewport := picker.layout().askInputViewport(width, inputContentLineLimit(height))
	if picker.ask.freeform {
		t.Fatal("round trip did not return to picker mode")
	}
	if picker.ask.editor.Text() != "draft text" || picker.ask.editor.Cursor() != wantEditorCursor {
		t.Fatalf("picker mode changed draft/editor cursor: %q/%d", picker.ask.editor.Text(), picker.ask.editor.Cursor())
	}
	if pickerViewport.cursor.Visible {
		t.Fatal("picker viewport exposed the disabled draft cursor")
	}
	if len(pickerViewport.lines) > inputContentLineLimit(height) {
		t.Fatalf("picker viewport rows = %d, content budget = %d", len(pickerViewport.lines), inputContentLineLimit(height))
	}

	next, _ = picker.Update(tea.KeyMsg{Type: tea.KeyTab})
	restored := next.(*uiModel)
	restoredViewport := restored.layout().askInputViewport(width, inputContentLineLimit(height))
	if !restored.ask.freeform {
		t.Fatal("round trip did not restore freeform mode")
	}
	if restored.ask.editor.Text() != "draft text" || restored.ask.editor.Cursor() != wantEditorCursor {
		t.Fatalf("restored freeform changed draft/editor cursor: %q/%d", restored.ask.editor.Text(), restored.ask.editor.Cursor())
	}
	if !restoredViewport.cursor.Visible {
		t.Fatal("restored bounded freeform viewport hid the active editor cursor")
	}
	if len(restoredViewport.lines) > inputContentLineLimit(height) {
		t.Fatalf("restored viewport rows = %d, content budget = %d", len(restoredViewport.lines), inputContentLineLimit(height))
	}
}
