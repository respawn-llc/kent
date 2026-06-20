package input

import (
	"testing"

	"github.com/rivo/uniseg"
)

func TestFieldRenderReturnsWidthSafeLinesAndCursor(t *testing.T) {
	field := NewField()
	field.Prefix = "› "
	field.Editor.Replace("alpha beta gamma")

	result := field.Render(8)
	if result.Width != 8 {
		t.Fatalf("width = %d, want 8", result.Width)
	}
	if len(result.Lines) != 3 {
		t.Fatalf("lines = %+v, want 3 lines", result.Lines)
	}
	for index, line := range result.Lines {
		if got := uniseg.StringWidth(line); got != 8 {
			t.Fatalf("line %d width = %d, want 8: %q", index, got, line)
		}
	}
	if !result.Cursor.Visible {
		t.Fatal("expected cursor visible")
	}
	if result.Cursor.Row != 2 || result.Cursor.Col != 2 {
		t.Fatalf("cursor = %+v, want row 2 col 2", result.Cursor)
	}
}

func TestFieldRenderFrameOffsetsCursor(t *testing.T) {
	field := NewField()
	field.Framed = true
	field.Prefix = "› "
	field.Editor.Replace("hello")

	result := field.Render(10)
	if len(result.Lines) != 3 {
		t.Fatalf("framed lines = %+v, want 3", result.Lines)
	}
	if result.Cursor.Row != 1 || result.Cursor.Col != uniseg.StringWidth("› hello") {
		t.Fatalf("framed cursor = %+v", result.Cursor)
	}
}
