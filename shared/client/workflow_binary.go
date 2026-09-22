package client

import (
	"fmt"

	"core/shared/protoapi"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	"core/shared/serverapi"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func workflowMethod(service, method protoreflect.Name) protoreflect.MethodDescriptor {
	return bootstrapMethod(pb.File_kent_api_workflow_definition_workflow_definition_proto, service, method)
}

func workflowEntityGeneratedError(code string, missing *pb.WorkflowNotFoundDetails) error {
	if code == "workflow_not_found" {
		if err := protoapi.Validate(missing); err != nil {
			return err
		}
		return fmt.Errorf("%w: %q", serverapi.ErrWorkflowNotFound, missing.WorkflowId)
	}
	return generatedOperationFailure(code)
}
