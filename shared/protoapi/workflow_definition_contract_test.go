package protoapi

import (
	"testing"

	workflowdefinitionpb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/runtimeids"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestWorkflowVersionSafeIntegerBoundary(t *testing.T) {
	request := &workflowdefinitionpb.GraphSavePreviewRequest{
		WorkflowId: runtimeids.NewWorkflowID().String(), ExpectedVersion: 9007199254740991,
		Graph: &workflowdefinitionpb.GraphDraft{},
	}
	if err := Validate(request); err != nil {
		t.Fatalf("largest supported version: %v", err)
	}
	request.ExpectedVersion++
	if err := Validate(request); err == nil {
		t.Fatal("version outside the declared safe integer range was accepted")
	}
}

func TestWorkflowCustomRefDraftCanBeSavedWithoutRef(t *testing.T) {
	policy := &workflowdefinitionpb.ExecutionTargetConfiguration{
		Mode: workflowdefinitionpb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_CUSTOM_REF,
	}
	if err := Validate(policy); err != nil {
		t.Fatalf("saveable custom-ref Draft: %v", err)
	}
}

func TestWorkflowValidationCodeRoundTripsThroughGeneratedContract(t *testing.T) {
	code := workflowdefinitionpb.ValidationErrorCode_VALIDATION_ERROR_CODE_SESSION_TRANSITION_MISSING
	const placeholder = ".Params.review.session_id"
	message := &workflowdefinitionpb.WorkflowValidationError{
		Code:    code,
		Message: "required diagnostic",
		Details: &workflowdefinitionpb.WorkflowValidationErrorDetails{
			Placeholder: placeholder,
		},
	}

	codeField := message.ProtoReflect().Descriptor().Fields().ByName("code")
	if codeField == nil || codeField.Kind() != protoreflect.EnumKind {
		t.Fatalf("generated code field descriptor = %v, want enum", codeField)
	}

	encoded, err := proto.Marshal(message)
	if err != nil {
		t.Fatalf("marshal workflow validation error: %v", err)
	}
	decoded := &workflowdefinitionpb.WorkflowValidationError{}
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		t.Fatalf("unmarshal workflow validation error: %v", err)
	}
	if decoded.GetCode() != code || decoded.GetMessage() != "required diagnostic" ||
		decoded.GetDetails() == nil || decoded.GetDetails().GetPlaceholder() != placeholder {
		t.Fatalf("decoded workflow validation error = %v, want code, message, and placeholder", decoded)
	}
}
