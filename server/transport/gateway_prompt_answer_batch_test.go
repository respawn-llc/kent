package transport

import (
	"testing"

	"core/shared/protoapi"
	connectionpb "core/shared/protoapi/gen/kent/api/connection"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
)

func TestGatewayPromptAnswerBatchRoundTrip(t *testing.T) {
	appCore, server := newGatewayTestServer(t)
	defer func() { _ = appCore.Close() }()
	defer server.Close()
	store := createGatewayAuthoritativeSession(t, appCore)
	sessionID, err := runtimeids.ParseSessionID(store.Meta().SessionID)
	if err != nil {
		t.Fatalf("ParseSessionID: %v", err)
	}
	stepID, err := runtimeids.ParseStepID("22222222-2222-4222-8222-222222222222")
	if err != nil {
		t.Fatalf("ParseStepID: %v", err)
	}
	request := &promptpb.AnswerBatchRequest{
		SessionId: sessionID.String(),
		StepId:    stepID.String(),
		Entries: []*promptpb.AnswerBatchEntry{{
			ToolCallId: "declined-1",
			Answer:     &promptpb.AnswerBatchEntry_Declined{Declined: &promptpb.Declined{}},
		}},
	}

	conn := dialGateway(t, server)
	defer func() { _ = conn.Close() }()
	handshakeGateway(t, conn)
	if result := attachGatewayProject(t, conn, "attach-project", &connectionpb.AttachProjectRequest{ProjectId: appCore.ProjectID()}); result.GetSuccess() == nil {
		t.Fatalf("attach Project failed: %+v", result.GetError())
	}
	var result promptpb.AnswerBatchResult
	callGatewayDescriptor(t, conn, "prompt-answer-batch",
		promptpb.File_kent_api_prompt_prompt_proto.Services().ByName("AnswerService").Methods().ByName("AnswerBatch"),
		request, &result)
	response := result.GetSuccess()
	if response == nil {
		t.Fatalf("prompt answer batch failed: %+v", result.GetError())
	}
	if err := protoapi.ValidatePromptAnswerBatchResponse(request, response); err != nil {
		t.Fatalf("ValidatePromptAnswerBatchResponse: %v", err)
	}
	if len(response.Results) != 1 || response.Results[0].Outcome != promptpb.AnswerBatchOutcome_ANSWER_BATCH_OUTCOME_SKIPPED {
		t.Fatalf("gateway response = %+v", response)
	}
}
