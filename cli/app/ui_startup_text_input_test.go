package app

import (
	"strings"
	"testing"
)

func TestProjectNamePromptUsesRealAltScreenCursorWhenAvailable(t *testing.T) {
	state := newUITerminalCursorState()
	model := newProjectNamePromptModel("alpha beta gamma", "dark")
	model.terminalCursor = state
	model.width = 24
	model.height = 10

	view := model.View()
	placement, ok := state.Snapshot()
	if !ok {
		t.Fatalf("expected real cursor placement for project prompt input, view=%q", view)
	}
	if !placement.AltScreen {
		t.Fatalf("expected alt-screen cursor placement, got %+v", placement)
	}
	if placement.CursorCol >= model.width {
		t.Fatalf("cursor col %d outside width %d", placement.CursorCol, model.width)
	}
	if strings.Contains(view, "\x1b[7") {
		t.Fatal("did not expect soft cursor when real terminal cursor is available")
	}
}
