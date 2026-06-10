package tui

import (
	"testing"

	"builder/shared/clientui"
)

func TestTranscriptRoleWireRoundTrip(t *testing.T) {
	tests := []struct {
		name string
		wire string
		want TranscriptRole
	}{
		{name: "empty", wire: "", want: TranscriptRoleUnknown},
		{name: "trimmed assistant", wire: " assistant ", want: TranscriptRoleAssistant},
		{name: "known tool result", wire: "tool_result_ok", want: TranscriptRoleToolResultOK},
		{name: "known cache warning", wire: "cache_warning", want: TranscriptRoleCacheWarning},
		{name: "future role preserved", wire: "future_renderable_role", want: TranscriptRole("future_renderable_role")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TranscriptRoleFromWire(tt.wire)
			if got != tt.want {
				t.Fatalf("role = %q, want %q", got, tt.want)
			}
			if got == TranscriptRoleUnknown {
				return
			}
			if roundTrip := TranscriptRoleFromWire(string(got)); roundTrip != got {
				t.Fatalf("round trip = %q, want %q", roundTrip, got)
			}
		})
	}
}

func TestTranscriptRoleDisplayIntent(t *testing.T) {
	tests := []struct {
		name  string
		role  TranscriptRole
		phase clientui.MessagePhase
		want  RenderIntent
	}{
		{name: "assistant final", role: TranscriptRoleAssistant, phase: clientui.MessagePhaseFinal, want: RenderIntentAssistant},
		{name: "assistant commentary", role: TranscriptRoleAssistant, phase: clientui.MessagePhaseCommentary, want: RenderIntentAssistantCommentary},
		{name: "tool result ok", role: TranscriptRoleToolResultOK, want: RenderIntentToolSuccess},
		{name: "tool result error", role: TranscriptRoleToolResultError, want: RenderIntentToolError},
		{name: "future role intent preserved", role: TranscriptRole("future_renderable_role"), want: RenderIntent("future_renderable_role")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.role.DisplayIntent(tt.phase); got != tt.want {
				t.Fatalf("intent = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderIntentToolResultMapping(t *testing.T) {
	tests := []struct {
		name   string
		base   RenderIntent
		result TranscriptRole
		want   RenderIntent
	}{
		{name: "shell success", base: RenderIntentToolShell, result: TranscriptRoleToolResultOK, want: RenderIntentToolShellSuccess},
		{name: "shell error", base: RenderIntentToolShell, result: TranscriptRoleToolResultError, want: RenderIntentToolShellError},
		{name: "question success keeps question", base: RenderIntentToolQuestion, result: TranscriptRoleToolResultOK, want: RenderIntentToolQuestion},
		{name: "question error", base: RenderIntentToolQuestion, result: TranscriptRoleToolResultError, want: RenderIntentToolQuestionError},
		{name: "generic future result ignored", base: RenderIntent("future_tool"), result: TranscriptRole("future_result"), want: RenderIntentTool},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.base.BaseToolResultIntent(tt.result); got != tt.want {
				t.Fatalf("mapped intent = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRenderIntentThinkingPredicate(t *testing.T) {
	tests := []struct {
		intent RenderIntent
		want   bool
	}{
		{intent: RenderIntentThinking, want: true},
		{intent: RenderIntentThinkingTrace, want: true},
		{intent: RenderIntentReasoning, want: true},
		{intent: RenderIntentAssistant, want: false},
	}
	for _, tt := range tests {
		t.Run(string(tt.intent), func(t *testing.T) {
			if got := tt.intent.IsThinking(); got != tt.want {
				t.Fatalf("IsThinking() = %t, want %t", got, tt.want)
			}
		})
	}
}
