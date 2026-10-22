package runtime

import (
	"testing"

	"core/server/llm"
	"core/shared/runtimeids"
)

func TestNewAgentSteerBuildsTypedDeveloperMessage(t *testing.T) {
	sourceID := runtimeids.NewSessionID()
	steer := NewAgentSteer(runtimeids.SessionIdentity{ID: sourceID}, "inspect the migration\nthen report back")
	message := steer.Message()
	if message.Role != llm.RoleDeveloper {
		t.Fatalf("message role = %q, want developer", message.Role)
	}
	if message.MessageType == nil || *message.MessageType != llm.MessageTypeAgentSteer {
		t.Fatalf("message type = %v, want agent_steer", message.MessageType)
	}
	if message.Content == nil || *message.Content == "" {
		t.Fatalf("message content = %v", message.Content)
	}
}
