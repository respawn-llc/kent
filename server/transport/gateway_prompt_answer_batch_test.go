package transport

import (
	"testing"

	"core/shared/protoapi"
	connectionpb "core/shared/protoapi/gen/kent/api/connection"
	promptpb "core/shared/protoapi/gen/kent/api/prompt"
	"core/shared/runtimeids"
	"core/shared/serverapi"
)

func TestGatewayPromptAnswerBatchMapsParentRejection(t *testing.T) {
	bindings := make(map[string]gatewayBinaryBinding)
	if err := registerPromptGatewayBinaryBindings(bindings); err != nil {
		t.Fatalf("register prompt gateway bindings: %v", err)
	}
	method := promptpb.File_kent_api_prompt_prompt_proto.Services().
		ByName("AnswerService").Methods().ByName("AnswerBatch")
	operation, err := protoapi.OperationFromDescriptor(method)
	if err != nil {
		t.Fatalf("resolve AnswerBatch operation: %v", err)
	}
	binding, ok := bindings[operation.Name]
	if !ok {
		t.Fatal("AnswerBatch binding is missing")
	}
	answeringSessionID := runtimeids.NewSessionID()
	questionSessionID := runtimeids.NewSessionID()
	request := &promptpb.AnswerBatchRequest{
		SessionId: questionSessionID.String(),
		StepId:    "22222222-2222-4222-8222-222222222222",
	}

	message := binding.failure(nil, nil, request, &serverapi.ParentQuestionAnswerRejectedError{
		AnsweringSessionID: answeringSessionID,
		QuestionSessionID:  questionSessionID,
	})
	encoded, err := protoapi.Marshal(message)
	if err != nil {
		t.Fatalf("marshal failure result: %v", err)
	}
	var result promptpb.AnswerBatchResult
	if err := protoapi.Unmarshal(encoded, &result); err != nil {
		t.Fatalf("unmarshal failure result: %v", err)
	}
	if err := protoapi.Validate(&result); err != nil {
		t.Fatalf("validate AnswerBatchResult: %v", err)
	}
	if result.GetError().Code != "parent_question_answer_rejected" {
		t.Fatalf("failure code = %q", result.GetError().Code)
	}
	details := result.GetError().GetParentQuestionAnswerRejected()
	if details == nil ||
		details.AnsweringSessionId != answeringSessionID.String() ||
		details.QuestionSessionId != questionSessionID.String() {
		t.Fatalf("parent rejection details = %+v", details)
	}
}

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
