package protoapi

import (
	"errors"

	"buf.build/go/protovalidate"
	pb "core/shared/protoapi/gen/kent/api/workflow_definition"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"google.golang.org/protobuf/proto"
)

// WorkflowProjectLabelValidationDetail keeps label form errors typed while
// using generated violations as the sole message-local validation authority.
func WorkflowProjectLabelValidationDetail(projectID string, err error) proto.Message {
	field := workflowLabelValidationField(err)
	if field == nil {
		return nil
	}
	if *field == "name" && projectID != "" {
		return &pb.LabelInvalidNameDetails{ProjectId: projectID, Field: *field}
	}
	var project *string
	if projectID != "" {
		project = &projectID
	}
	return &pb.LabelInvalidMutationDetails{ProjectId: project, Field: *field}
}

func WorkflowTaskLabelValidationDetail(taskID string, err error) *taskpb.LabelErrorDetails {
	field := workflowLabelValidationField(err)
	if field == nil {
		return nil
	}
	var task *string
	if taskID != "" {
		task = &taskID
	}
	return &taskpb.LabelErrorDetails{
		Reason: taskpb.LabelErrorReason_LABEL_ERROR_REASON_INVALID_MUTATION,
		TaskId: task, Field: field,
	}
}

func workflowLabelValidationField(err error) *string {
	var validation *protovalidate.ValidationError
	if !errors.As(err, &validation) {
		return nil
	}
	var field *string
	for _, violation := range validation.Violations {
		elements := violation.Proto.GetField().GetElements()
		if len(elements) == 0 {
			if violation.Proto.GetRuleId() == "kent.workflow_task.labels_update_disjoint" {
				name := "remove_label_ids"
				field = &name
			}
			continue
		}
		name := elements[0].GetFieldName()
		field = &name
		if elements[0].GetFieldNumber() == 1 {
			break
		}
	}
	return field
}
