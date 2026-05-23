package prompts

import "testing"

func TestFormatHandoffFutureAgentMessage(t *testing.T) {
	raw := " left \"quoted\"\nnext step "
	got := FormatHandoffFutureAgentMessage(raw)
	want := "The previous agent also left an additional message: \"left \"quoted\"\nnext step\""
	if got != want {
		t.Fatalf("FormatHandoffFutureAgentMessage() = %q, want %q", got, want)
	}
}
