package protoapi

import (
	"testing"

	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func TestQuestionSessionIdentityContract(t *testing.T) {
	for _, id := range []string{
		runtimeids.NewSessionID().String(),
		"session-1",
		"550e8400-e29b-41d4-a716-446655440000",
		"550E8400-E29B-41D4-A716-446655440000",
		"550e8400e29b41d4a716446655440000",
		"urn:uuid:550e8400-e29b-41d4-a716-446655440000",
		"{550e8400-e29b-41d4-a716-446655440000}",
		"550e8400-e29b-11d4-a716-446655440000",
	} {
		t.Run(id, func(t *testing.T) {
			question := &promptpb.Question{
				SessionId: id, ToolCallId: "call-1",
				StepId: runtimeids.NewSessionID().String(), Question: "Continue?",
				CreatedAt: timestamppb.Now(),
			}
			_, sourceErr := runtimeids.ParseSessionID(id)
			if err := Validate(question); (err == nil) != (sourceErr == nil) {
				t.Fatalf("wire identity acceptance differs from canonical source: %v", err)
			}
			if sourceErr == nil {
				pending, err := PendingAskFromQuestion(question)
				if err != nil {
					t.Fatal(err)
				}
				if pending.SessionID.String() != id {
					t.Fatal("Session identity changed during conversion")
				}
			}
		})
	}
}
