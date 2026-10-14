package protoapi_test

import (
	"testing"

	"core/shared/protoapi"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

func TestTranscriptSessionNameMustBeNonblankWhenPresent(t *testing.T) {
	if err := protoapi.Validate(&transcriptpb.SessionIdentity{
		SessionId:             "11111111-1111-4111-8111-111111111111",
		ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH,
	}); err != nil {
		t.Fatalf("absent name: %v", err)
	}
	for _, name := range []string{" \t", "session"} {
		message := &transcriptpb.SessionIdentity{
			SessionId:             "11111111-1111-4111-8111-111111111111",
			SessionName:           &name,
			ConversationFreshness: runtimepb.ConversationFreshness_CONVERSATION_FRESHNESS_FRESH,
		}
		if err := protoapi.Validate(message); (err == nil) != (name == "session") {
			t.Errorf("name %q acceptance = %v", name, err)
		}
	}
}

func TestTranscriptWorkflowTaskMustBeNonblankWhenPresent(t *testing.T) {
	if err := protoapi.Validate(&transcriptpb.SessionStatus{
		ReviewerFrequency: "off", ThinkingLevel: "medium", CompactionMode: "none",
	}); err != nil {
		t.Fatalf("absent workflow: %v", err)
	}
	for _, task := range []string{" \t", "task-1"} {
		message := &transcriptpb.SessionStatus{
			ReviewerFrequency: "off",
			ThinkingLevel:     "medium",
			CompactionMode:    "none",
			Workflow:          &runtimepb.WorkflowSessionStatus{TaskId: task, WorkflowId: "workflow-1"},
		}
		if err := protoapi.Validate(message); (err == nil) != (task == "task-1") {
			t.Errorf("task %q acceptance = %v", task, err)
		}
	}
}
