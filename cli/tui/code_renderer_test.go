package tui

import (
	"builder/shared/transcript"
	patchformat "builder/shared/transcript/patchformat"
	"strings"
	"testing"

	"github.com/alecthomas/chroma/v2"
	"github.com/charmbracelet/x/ansi"
)

func renderedPatch(t *testing.T, cwd string, patchLines ...string) *patchformat.RenderedPatch {
	t.Helper()
	rendered := patchformat.Render(strings.Join(patchLines, "\n")+"\n", cwd)
	return &rendered
}

func renderedDiff(lines ...patchformat.RenderedLine) *patchformat.RenderedPatch {
	return &patchformat.RenderedPatch{DetailLines: lines}
}

func TestCodeRendererUsesDialectSpecificShellLexers(t *testing.T) {
	r := newCodeRenderer("dark")
	tests := []struct {
		name    string
		dialect transcript.ToolShellDialect
		want    string
	}{
		{name: "powershell", dialect: transcript.ToolShellDialectPowerShell, want: "PowerShell"},
		{name: "windows command", dialect: transcript.ToolShellDialectWindowsCommand, want: "Batchfile"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			lexer := r.resolveLexer(&transcript.ToolRenderHint{Kind: transcript.ToolRenderKindShell, ShellDialect: tc.dialect}, `copy /y C:\Users\nek\src.txt C:\Temp\dst.txt`)
			if lexer == nil || lexer.Config() == nil {
				t.Fatalf("expected lexer for %s shell dialect", tc.name)
			}
			if got := lexer.Config().Name; got != tc.want {
				t.Fatalf("lexer name = %q, want %q", got, tc.want)
			}
		})
	}
}

func testCodeRendererOverridesBaseTextColorWithAppForeground(t *testing.T, theme string) {
	t.Helper()
	r := newCodeRenderer(theme)
	baseText := r.baseStyle().Get(chroma.Text).Colour
	appForeground := chroma.MustParseColour(r.baseForeground.hexString())
	style := r.style()
	if got := style.Get(chroma.Text).Colour; got != appForeground {
		t.Fatalf("expected code renderer text color to use app foreground for %s theme, got %s want %s", theme, got, appForeground)
	}
	for _, token := range []chroma.TokenType{chroma.Text, chroma.Keyword, chroma.LiteralString, chroma.NameFunction, chroma.Punctuation} {
		if bg := style.Get(token).Background; bg.IsSet() {
			t.Fatalf("expected code renderer token %s background to stay transparent for %s theme, got %s", token, theme, bg)
		}
	}
	hint := &transcript.ToolRenderHint{Kind: transcript.ToolRenderKindShell}
	out, ok := r.render(hint, "./gradlew -p apps/respawn detektFormat")
	if !ok {
		t.Fatal("expected shell highlight to render")
	}
	oldBaseSeq := foregroundEscape(rgbColor{r: int(baseText.Red()), g: int(baseText.Green()), b: int(baseText.Blue())})
	if baseText != appForeground && strings.Contains(out, oldBaseSeq) {
		t.Fatalf("expected shell highlight to avoid original base text color %q, got %q", oldBaseSeq, out)
	}
	if containsBackgroundSGR(out) {
		t.Fatalf("expected shell highlight to avoid background color escapes for %s theme, got %q", theme, out)
	}
}

func testCodeRendererShellHighlightOwnsDefaultForeground(t *testing.T, theme string) {
	t.Helper()
	r := newCodeRenderer(theme)
	hint := &transcript.ToolRenderHint{Kind: transcript.ToolRenderKindShell}
	command := `git add cli/app/ui_mode_flow_test.go cli/app/ui_native_scrollback_integration_test.go cli/app/ui_runtime_adapter_test.go cli/app/ui_runtime_client.go cli/app/ui_runtime_control_test.go cli/app/ui_runtime_sync.go cli/tui/model_reducer.go cli/tui/model_rendering.go cli/tui/model_rendering_style.go cli/tui/model_rendering_tools.go cli/tui/model_test.go cli/tui/roles.go testdata/transcript-visibility.md server/primaryrun/runtime_client.go server/primaryrun/runtime_client_test.go server/runtime/chat_store.go server/runtime/chat_store_test.go server/runtime/compaction.go server/runtime/engine_test.go server/runtime/step_executor.go server/runtime/transcript_event_entries.go server/runtime/transcript_projector_test.go server/runtime/transcript_scan.go server/runtime/transcript_message_visibility.go shared/clientui/runtime.go shared/transcript/roles.go && git commit -m "fix: align transcript visibility and refresh behavior"`

	out, ok := r.render(hint, command)
	if !ok {
		t.Fatal("expected shell highlight to render")
	}
	assertHasForegroundOwnership(t, out, r.baseForeground)
	assertRestoresForegroundAfterReset(t, out, r.baseForeground)
	for _, unwanted := range oldFormatterBaseForegroundEscapes(theme) {
		if strings.Contains(out, unwanted) {
			t.Fatalf("expected shell highlight to avoid formatter-owned base foreground %q for %s theme, got %q", unwanted, theme, out)
		}
	}
	if got := ansi.Strip(out); got != command {
		t.Fatalf("expected shell highlight text preserved, got %q", got)
	}
}

func TestCodeRendererRejectsInvalidHint(t *testing.T) {
	r := newCodeRenderer("dark")
	hint := &transcript.ToolRenderHint{Kind: transcript.ToolRenderKindSource}
	if out, ok := r.render(hint, "package main"); ok || out != "" {
		t.Fatalf("expected invalid hint to skip rendering, got ok=%v out=%q", ok, out)
	}
}
