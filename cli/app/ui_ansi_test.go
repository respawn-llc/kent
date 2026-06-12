package app

import (
	"fmt"
	"strings"
	"testing"

	"github.com/lucasb-eyer/go-colorful"

	"builder/shared/theme"
)

// foregroundTrueColorEscape builds the truecolor foreground escape exactly the
// way the terminal renderer emits it. termenv resolves a hex color through
// go-colorful and truncates each channel with uint8(component*255), so the
// float round-trip can land one below the literal hex value (e.g. #3185FC's
// red 0x31 yields 48, not 49). Mirroring that conversion here keeps expected
// escapes aligned with the rendered output for every palette color.
func foregroundTrueColorEscape(hex string) string {
	c, err := colorful.Hex(strings.TrimSpace(hex))
	if err != nil {
		return ""
	}
	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", uint8(c.R*255), uint8(c.G*255), uint8(c.B*255))
}

func assertContainsColoredShellSymbol(t *testing.T, text, themeName, paletteHex string) {
	t.Helper()
	want := foregroundTrueColorEscape(paletteHex) + "$"
	if !strings.Contains(text, want) {
		t.Fatalf("expected %s shell symbol escape %q in %q", themeName, want, text)
	}
}

func assertNoColoredShellSymbol(t *testing.T, text, themeName, paletteHex string) {
	t.Helper()
	forbidden := foregroundTrueColorEscape(paletteHex) + "$"
	if strings.Contains(text, forbidden) {
		t.Fatalf("did not expect %s shell symbol escape %q in %q", themeName, forbidden, text)
	}
}

func transcriptToolSuccessColorHex(themeName string) string {
	return theme.ResolvePalette(themeName).Transcript.ToolSuccess.TrueColor
}

func transcriptToolPendingColorHex(themeName string) string {
	return theme.ResolvePalette(themeName).Transcript.Tool.TrueColor
}
