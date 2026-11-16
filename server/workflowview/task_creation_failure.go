package workflowview

import (
	"core/server/workflowstore"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"fmt"
)

func TaskCreateSelectionFailure(selection workflowstore.TaskWorkflowSelectionError) (*taskpb.CreateSelectionDetails, error) {
	var reason taskpb.CreateSelectionReason
	switch selection.Reason {
	case workflowstore.TaskWorkflowSelectionNoLinkedWorkflows:
		reason = taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_NO_LINKED_WORKFLOWS
	case workflowstore.TaskWorkflowSelectionWorkflowNotLinked:
		reason = taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_WORKFLOW_NOT_LINKED
	case workflowstore.TaskWorkflowSelectionAmbiguousWithoutDefault:
		reason = taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_AMBIGUOUS_WITHOUT_DEFAULT
	default:
		return nil, fmt.Errorf("unsupported Task Workflow selection reason %q", selection.Reason)
	}
	detail := &taskpb.CreateSelectionDetails{Reason: reason, ProjectId: selection.ProjectID}
	if selection.WorkflowID != nil {
		value := selection.WorkflowID.String()
		detail.WorkflowId = &value
	}
	return detail, nil
}
