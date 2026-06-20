package tui

import (
	"testing"

	"core/shared/transcript"
)

func TestResolveToolRenderHintPreservesShellDialectOnShellPreviewFallback(t *testing.T) {
	meta := &transcript.ToolCallMeta{
		RenderBehavior: transcript.ToolCallRenderBehaviorShell,
		RenderHint: &transcript.ToolRenderHint{
			Kind:         transcript.ToolRenderKindSource,
			Path:         "script.ps1",
			ResultOnly:   true,
			ShellDialect: transcript.ToolShellDialectPowerShell,
		},
	}

	hint, ok := resolveToolRenderHint("tool_shell_success", "Get-Content script.ps1", meta)
	if !ok {
		t.Fatal("expected shell preview fallback hint")
	}
	if hint.Kind != transcript.ToolRenderKindShell {
		t.Fatalf("expected shell fallback hint, got %+v", hint)
	}
	if hint.ShellDialect != transcript.ToolShellDialectPowerShell {
		t.Fatalf("expected powershell dialect preserved, got %+v", hint)
	}
}
