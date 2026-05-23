package runtime

import (
	"testing"

	"builder/prompts"
)

func TestHandoffFutureAgentMessageWrapsContentForModelAndTranscript(t *testing.T) {
	msg := handoffFutureAgentMessage(" resume with tests ")

	if got, want := msg.Content, prompts.FormatHandoffFutureAgentMessage("resume with tests"); got != want {
		t.Fatalf("future-agent message content = %q, want %q", got, want)
	}
}
