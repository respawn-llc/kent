package protoapi

import (
	"testing"

	workflowdefinitionpb "core/shared/protoapi/gen/kent/api/workflow_definition"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestWorkflowValidationReasonRoundTripsThroughGeneratedContract(t *testing.T) {
	reason := workflowdefinitionpb.ValidationErrorReason_VALIDATION_ERROR_REASON_SESSION_TRANSITION_MISSING
	const placeholder = ".Params.review.session_id"
	message := &workflowdefinitionpb.WorkflowValidationError{
		Details: &workflowdefinitionpb.WorkflowValidationErrorDetails{
			Placeholder: placeholder,
			Reason:      &reason,
		},
	}

	reasonField := message.ProtoReflect().Descriptor().Fields().ByName("details").Message().Fields().ByName("reason")
	if reasonField == nil || reasonField.Kind() != protoreflect.EnumKind || !reasonField.HasOptionalKeyword() {
		t.Fatalf("generated reason field descriptor = %v, want optional enum", reasonField)
	}

	encoded, err := proto.Marshal(message)
	if err != nil {
		t.Fatalf("marshal workflow validation error: %v", err)
	}
	decoded := &workflowdefinitionpb.WorkflowValidationError{}
	if err := proto.Unmarshal(encoded, decoded); err != nil {
		t.Fatalf("unmarshal workflow validation error: %v", err)
	}
	if decoded.GetDetails() == nil || decoded.GetDetails().GetPlaceholder() != placeholder ||
		decoded.GetDetails().GetReason() != reason {
		t.Fatalf("decoded workflow validation error = %v, want placeholder and reason", decoded)
	}
}
