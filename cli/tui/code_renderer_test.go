package tui

import (
	"core/shared/transcript"
	"testing"
)

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

func TestCodeRendererRejectsInvalidHint(t *testing.T) {
	r := newCodeRenderer("dark")
	hint := &transcript.ToolRenderHint{Kind: transcript.ToolRenderKindSource}
	if out, ok := r.render(hint, "package main"); ok || out != "" {
		t.Fatalf("expected invalid hint to skip rendering, got ok=%v out=%q", ok, out)
	}
}
