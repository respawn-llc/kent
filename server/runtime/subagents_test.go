package runtime

import (
	"testing"

	"core/server/llm"
)

func TestSplitMetaContextMessagesTreatsSubagentsAsMeta(t *testing.T) {
	subagents := llm.Message{Role: llm.RoleDeveloper, MessageType: llm.MessageTypeSubagents, Content: "Available subagent roles:"}
	messages := []llm.Message{
		subagents,
		{Role: llm.RoleUser, Content: "request"},
	}
	meta, transcript := splitMetaContextMessages(messages)
	if len(meta) != 1 || meta[0].MessageType != llm.MessageTypeSubagents {
		t.Fatalf("expected subagents meta message, got %+v", meta)
	}
	if len(transcript) != 1 || transcript[0].Role != llm.RoleUser {
		t.Fatalf("expected user transcript, got %+v", transcript)
	}
}

