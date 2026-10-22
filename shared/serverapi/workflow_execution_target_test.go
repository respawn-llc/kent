package serverapi

import (
	"testing"

	"buf.build/go/protovalidate"
	definitionpb "core/shared/protoapi/gen/kent/api/workflow_definition"

	"core/shared/runtimeids"
)

func TestWorkflowGraphMetadataExecutionTargetPolicyValidation(t *testing.T) {
	customRef := "refs/tags/v1"
	if err := protovalidate.Validate(&definitionpb.GraphSavePreviewRequest{
		WorkflowId:      runtimeids.NewWorkflowID().String(),
		ExpectedVersion: 1,
		Graph:           &definitionpb.GraphDraft{},
		Metadata: &definitionpb.GraphMetadata{
			Name:                  "Workflow",
			ExecutionTargetPolicy: &definitionpb.ExecutionTargetConfiguration{Mode: definitionpb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_CUSTOM_REF, CustomRef: &customRef},
		},
	}); err != nil {
		t.Fatalf("custom target policy metadata rejected: %v", err)
	}
	if err := protovalidate.Validate(&definitionpb.GraphSavePreviewRequest{
		WorkflowId:      runtimeids.NewWorkflowID().String(),
		ExpectedVersion: 1,
		Graph:           &definitionpb.GraphDraft{},
		Metadata: &definitionpb.GraphMetadata{
			Name:                  "Workflow",
			ExecutionTargetPolicy: &definitionpb.ExecutionTargetConfiguration{Mode: definitionpb.ExecutionTargetMode_WORKFLOW_EXECUTION_TARGET_MODE_HEAD, CustomRef: &customRef},
		},
	}); err == nil {
		t.Fatal("non-custom policy metadata accepted a custom ref")
	}
}
