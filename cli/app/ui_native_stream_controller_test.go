package app

import (
	"strings"
	"testing"

	"builder/cli/tui"

	xansi "github.com/charmbracelet/x/ansi"
)

func TestNativeAssistantStreamControllerPromotesCompletedMarkdownLines(t *testing.T) {
	controller := newNativeAssistantStreamController("dark", 48)

	first := controller.Append("first **line**\nsecond")
	if got := len(first.stable); got != 0 {
		t.Fatalf("expected last complete line to stay live until next line proves it is not setext, got stable %#v", first.stable)
	}
	if got := joinedPlainProjectionLines(first.tail); !strings.Contains(got, "first line") || !strings.Contains(got, "second") {
		t.Fatalf("expected held first line and incomplete second line in tail, got %q", got)
	}

	second := controller.Append(" line\n")
	if got := joinedPlainProjectionLines(second.stable); !strings.Contains(got, "first line") || strings.Contains(got, "second line") {
		t.Fatalf("expected first line to promote and last complete line to stay live, got %q", got)
	}
	if got := joinedPlainProjectionLines(second.tail); !strings.Contains(got, "second line") {
		t.Fatalf("expected last complete line in tail, got %q", got)
	}

	third := controller.Append("\n")
	if got := joinedPlainProjectionLines(third.stable); !strings.Contains(got, "second line") {
		t.Fatalf("expected second line to promote after blank-line boundary, got %q", got)
	}
	if got := len(third.tail); got != 0 {
		t.Fatalf("expected empty live tail after blank-line boundary, got %#v", third.tail)
	}
}

func TestNativeAssistantStreamControllerHoldsPipeTableUntilFinalize(t *testing.T) {
	controller := newNativeAssistantStreamController("dark", 72)

	update := controller.Append("| Name | Value |\n| --- | --- |\n| alpha | beta |\n")
	if got := len(update.stable); got != 0 {
		t.Fatalf("expected active table to stay live, got stable %#v", update.stable)
	}
	if got := joinedPlainProjectionLines(update.tail); !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("expected table in live tail, got %q", got)
	}

	final := controller.Finalize()
	if got := joinedPlainProjectionLines(final.stable); !strings.Contains(got, "alpha") || !strings.Contains(got, "beta") {
		t.Fatalf("expected final table to promote, got %q", got)
	}
	if got := len(final.tail); got != 0 {
		t.Fatalf("expected final tail empty, got %#v", final.tail)
	}
}

func TestNativeAssistantStreamControllerHoldsSetextHeadingCandidate(t *testing.T) {
	controller := newNativeAssistantStreamController("dark", 80)

	pending := controller.Append("Heading\n")
	if got := len(pending.stable); got != 0 {
		t.Fatalf("expected setext candidate to stay live, got stable %#v", pending.stable)
	}
	if got := joinedPlainProjectionLines(pending.tail); !strings.Contains(got, "Heading") {
		t.Fatalf("expected setext candidate in live tail, got %q", got)
	}

	confirmed := controller.Append("---\n")
	if got := joinedPlainProjectionLines(confirmed.stable); !strings.Contains(got, "## Heading") {
		t.Fatalf("expected confirmed setext heading to promote as heading, got %q", got)
	}
	if got := joinedPlainProjectionLines(confirmed.stable); strings.Contains(got, "❮ Heading") {
		t.Fatalf("expected stale paragraph render not promoted, got %q", got)
	}
}

func TestNativeAssistantStreamControllerHoldsReferenceLinksUntilDefinition(t *testing.T) {
	controller := newNativeAssistantStreamController("dark", 100)

	pending := controller.Append("This is [link][id]\n")
	if got := len(pending.stable); got != 0 {
		t.Fatalf("expected unresolved reference link to stay live, got stable %#v", pending.stable)
	}

	defined := controller.Append("\n[id]: https://example.com\n")
	if got := joinedPlainProjectionLines(defined.stable); !strings.Contains(got, "link https://example.com") {
		t.Fatalf("expected resolved reference link to promote with renderer-supported link output, got %q", got)
	}
	if got := joinedPlainProjectionLines(defined.stable); strings.Contains(got, "[link][id]") {
		t.Fatalf("expected stale unresolved reference output not promoted, got %q", got)
	}
}

func TestNativeAssistantStreamControllerHoldsShortcutReferenceLinksUntilDefinition(t *testing.T) {
	controller := newNativeAssistantStreamController("dark", 100)

	pending := controller.Append("This is [id]\n")
	if got := len(pending.stable); got != 0 {
		t.Fatalf("expected unresolved shortcut reference link to stay live, got stable %#v", pending.stable)
	}

	defined := controller.Append("\n[id]: https://example.com\n")
	if got := joinedPlainProjectionLines(defined.stable); !strings.Contains(got, "id https://example.com") {
		t.Fatalf("expected resolved shortcut reference link to promote with renderer-supported link output, got %q", got)
	}
}

func TestNativeAssistantStreamControllerPromotesOpenCodeFenceLinesOptimistically(t *testing.T) {
	controller := newNativeAssistantStreamController("dark", 80)

	update := controller.Append("```go\nfunc main() {}\n")
	if got := joinedPlainProjectionLines(update.stable); !strings.Contains(got, "func main()") {
		t.Fatalf("expected complete open-fence code line to promote, got %q", got)
	}
}

func TestNativeAssistantStreamControllerResizeInvalidatesFurtherPromotion(t *testing.T) {
	controller := newNativeAssistantStreamController("dark", 72)
	controller.Append("stable line\n\n")

	controller.Configure(controller.theme, 24)
	update := controller.Append("new stable line\n")
	if got := len(update.stable); got != 0 {
		t.Fatalf("expected resize-invalidated stream to stop promotion, got %#v", update.stable)
	}
	if got := joinedPlainProjectionLines(update.tail); !strings.Contains(got, "new stable line") {
		t.Fatalf("expected resized content in live tail, got %q", got)
	}
	if !update.needsReplay {
		t.Fatalf("expected resize-invalidated update to request replay")
	}
}

func joinedPlainProjectionLines(lines []tui.TranscriptProjectionLine) string {
	parts := make([]string, 0, len(lines))
	for _, line := range lines {
		parts = append(parts, xansi.Strip(line.Text))
	}
	return strings.Join(parts, "\n")
}
