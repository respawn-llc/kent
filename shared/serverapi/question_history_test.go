package serverapi_test

import (
	"core/shared/protoapi"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	"google.golang.org/protobuf/proto"
	"testing"
)

func TestQuestionHistorySubscribeRequestValidation(t *testing.T) {
	valid := &sessionpb.QuestionHistorySubscribeRequest{
		SessionId:   "12345678-1234-4234-8234-123456789012",
		MaxHandoffs: 25,
	}
	if err := protoapi.Validate(valid); err != nil {
		t.Fatalf("valid request: %v", err)
	}
	invalid := proto.Clone(valid).(*sessionpb.QuestionHistorySubscribeRequest)
	invalid.MaxHandoffs = 0
	if err := protoapi.Validate(invalid); err == nil {
		t.Fatal("nonpositive max_handoffs accepted")
	}
}

func TestQuestionHistoryEventValidation(t *testing.T) {
	events := []*sessionpb.QuestionHistoryEvent{
		{Event: &sessionpb.QuestionHistoryEvent_Started{Started: &sessionpb.QuestionHistoryStarted{}}},
		{Event: &sessionpb.QuestionHistoryEvent_Question{Question: &sessionpb.QuestionHistoryQuestion{Question: "question", Answer: "answer"}}},
		{Event: &sessionpb.QuestionHistoryEvent_Completed{Completed: &sessionpb.QuestionHistoryCompleted{HistoryOmitted: true}}},
	}
	for _, event := range events {
		if err := protoapi.Validate(event); err != nil {
			t.Fatalf("valid event %#v: %v", event, err)
		}
	}
	if err := protoapi.Validate(&sessionpb.QuestionHistoryEvent{}); err == nil {
		t.Fatal("Question event without record accepted")
	}
}
