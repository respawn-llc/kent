package serverapi_test

import (
	"testing"

	"core/shared/protoapi"
	sessionpb "core/shared/protoapi/gen/kent/api/session"
	transcriptpb "core/shared/protoapi/gen/kent/api/transcript"
)

func TestSessionTranscriptPageRequestRejectsZeroCursor(t *testing.T) {
	req := &transcriptpb.PageRequest{
		SessionId: "session-1",
		Direction: &transcriptpb.PageRequest_Cursor{Cursor: 0},
	}
	if err := protoapi.Validate(req); err == nil {
		t.Fatal("accepted zero page cursor")
	}
}

func TestSessionReadRequestsRequireSessionIdentity(t *testing.T) {
	if err := protoapi.Validate(&sessionpb.MainViewRequest{}); err == nil {
		t.Fatal("accepted main view request without Session")
	}
	if err := protoapi.Validate(&sessionpb.ExecutionEnvironmentRequest{}); err == nil {
		t.Fatal("accepted environment request without Session")
	}
	if err := protoapi.Validate(&transcriptpb.LatestFinalAnswerRequest{}); err == nil {
		t.Fatal("accepted latest answer request without Session")
	}
	if err := protoapi.Validate(&transcriptpb.LatestFinalAnswerRequest{SessionId: "session-1"}); err != nil {
		t.Fatal(err)
	}
}
