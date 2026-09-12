package serverapi_test

import (
	"testing"
	"time"

	"core/shared/protoapi"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	runtimepb "core/shared/protoapi/gen/kent/api/runtime"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
)

const (
	validLiveSessionID   = "9b9447ad-04e7-4c70-b4b0-f0eb1a53b47d"
	validLiveQueueItemID = "540a27aa-1e97-4696-8483-6d528ff8bbdd"
)

func TestRuntimeLiveSteerRequestValidateUsesUUIDV4Boundaries(t *testing.T) {
	req := &runtimepb.LiveSteerRequest{SessionId: validLiveSessionID, Text: " steer the run "}
	if err := protoapi.Validate(req); err != nil {
		t.Fatal(err)
	}
	caller := validLiveSessionID
	req.CallerSessionId = &caller
	if err := protoapi.Validate(req); err != nil {
		t.Fatal(err)
	}
	for _, invalid := range []*runtimepb.LiveSteerRequest{
		{SessionId: "session-1", Text: "text"},
		{SessionId: "018f3f8c-5b1a-7b72-8cb9-01af2c01a7e8", Text: "text"},
		{SessionId: validLiveSessionID, Text: " \t "},
	} {
		if err := protoapi.Validate(invalid); err == nil {
			t.Fatalf("accepted invalid request: %+v", invalid)
		}
	}
}

func TestRuntimeLiveSteerRequestRejectsMalformedCallerProvenance(t *testing.T) {
	for _, caller := range []string{"", "/invalid/session", "session-1", "  "} {
		req := &runtimepb.LiveSteerRequest{SessionId: validLiveSessionID, Text: "text", CallerSessionId: &caller}
		if err := protoapi.Validate(req); err == nil {
			t.Fatalf("accepted caller Session %q", caller)
		}
	}
}

func TestRuntimeLiveStopRequestValidateAndStatus(t *testing.T) {
	if err := protoapi.Validate(&runtimepb.LiveStopRequest{SessionId: validLiveSessionID}); err != nil {
		t.Fatal(err)
	}
	for _, status := range []runtimepb.LiveStopStatus{runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_STOPPED, runtimepb.LiveStopStatus_RUNTIME_LIVE_STOP_STATUS_IDLE} {
		if err := protoapi.Validate(&runtimepb.LiveStopSuccess{Status: status}); err != nil {
			t.Fatal(err)
		}
	}
	if err := protoapi.Validate(&runtimepb.LiveStopSuccess{Status: runtimepb.LiveStopStatus(999)}); err == nil {
		t.Fatal("accepted unsupported stop status")
	}
}

func TestRuntimeLiveWaitRequestAndResponseValidation(t *testing.T) {
	if err := protoapi.Validate(&runtimepb.LiveWaitRequest{SessionId: validLiveSessionID}); err != nil {
		t.Fatal(err)
	}
	response := validLiveWaitResponse()
	if err := protoapi.Validate(response); err != nil {
		t.Fatal(err)
	}
	response.Duration = durationpb.New(-time.Millisecond)
	if err := protoapi.Validate(response); err == nil {
		t.Fatal("accepted negative duration")
	}
}

func TestRuntimeLiveWaitResponsePreservesFinalAnswerUnion(t *testing.T) {
	wire, err := protoapi.Encode(validLiveWaitResponse())
	if err != nil {
		t.Fatal(err)
	}
	var decoded runtimepb.LiveWaitSuccess
	if err := protoapi.Decode(wire, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.GetAssistantFinalAnswer() == nil || decoded.GetNoFinalAnswer() != nil {
		t.Fatalf("unexpected final answer selection: %T", decoded.Result)
	}
}

func validLiveWaitResponse() *runtimepb.LiveWaitSuccess {
	return &runtimepb.LiveWaitSuccess{
		SessionId: validLiveSessionID, SessionName: "Session",
		Result:   &runtimepb.LiveWaitSuccess_AssistantFinalAnswer{AssistantFinalAnswer: &runtimepb.LiveWaitAssistantFinalAnswer{Result: "final"}},
		Duration: durationpb.New(42 * time.Millisecond), LiveRunGroupId: validLiveQueueItemID,
		TerminalRunId: validLiveQueueItemID, TerminalStepId: validLiveQueueItemID, TerminalStatus: "completed",
	}
}

func TestRuntimeLiveWatchResponseRejectsQuestionSessionMismatch(t *testing.T) {
	ask := &promptpb.Question{SessionId: "session-b", ToolCallId: "ask-1", StepId: validLiveQueueItemID, Question: "Continue?", CreatedAt: timestamppb.Now()}
	approval := &promptpb.Approval{SessionId: "session-b", ToolCallId: "approval-1", StepId: validLiveQueueItemID,
		Question: &ask.Question, CreatedAt: timestamppb.Now(), Options: []*promptpb.ApprovalOption{{Decision: promptpb.ApprovalDecision_APPROVAL_DECISION_ALLOW_ONCE, Label: "Allow once"}}}
	for _, question := range []*promptpb.ObservationQuestion{
		{Question: &promptpb.ObservationQuestion_Ask{Ask: ask}},
		{Question: &promptpb.ObservationQuestion_Approval{Approval: approval}},
	} {
		response := &promptpb.LiveWatchSuccess{SessionId: "session-a", Outcome: &promptpb.LiveWatchOutcome{
			Outcome: &promptpb.LiveWatchOutcome_Question{Question: question},
		}}
		if err := protoapi.Validate(response); err == nil {
			t.Fatal("question Session mismatch accepted")
		}
	}
}
