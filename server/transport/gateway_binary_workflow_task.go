package transport

import (
	"errors"

	"core/shared/apicontract"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"

	"google.golang.org/protobuf/proto"
)

func registerWorkflowTaskGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	read := taskpb.File_kent_api_workflow_task_read_proto.Services().ByName("TaskReadService")
	board := taskpb.File_kent_api_workflow_task_read_proto.Services().ByName("BoardReadService")
	return errors.Join(
		registerWorkflowUnary(bindings, read, "List",
			func() *taskpb.ListRequest { return &taskpb.ListRequest{} },
			apicontract.WorkflowService.ListWorkflowTasks, binaryTaskListFailure),
		registerWorkflowUnary(bindings, read, "GetProjectGroupCounts",
			func() *taskpb.ProjectTaskGroupCountsRequest { return &taskpb.ProjectTaskGroupCountsRequest{} },
			apicontract.WorkflowService.GetWorkflowProjectTaskGroupCounts, binaryWorkflowProjectFailure[*taskpb.ProjectTaskGroupCountsRequest]),
		registerWorkflowUnary(bindings, board, "Get",
			func() *taskpb.BoardGetRequest { return &taskpb.BoardGetRequest{} },
			apicontract.WorkflowService.GetWorkflowBoard, binaryTaskReadFailure[*taskpb.BoardGetRequest]),
		registerWorkflowUnary(bindings, board, "ListNodeCards",
			func() *taskpb.BoardNodeCardsListRequest { return &taskpb.BoardNodeCardsListRequest{} },
			apicontract.WorkflowService.ListWorkflowBoardNodeCards, binaryTaskReadFailure[*taskpb.BoardNodeCardsListRequest]),
	)
}

func binaryTaskListFailure(request *taskpb.ListRequest, err error) proto.Message {
	var scope *serverapi.WorkflowTaskListScopeError
	if errors.As(err, &scope) {
		var reason taskpb.ListScopeErrorReason
		switch scope.Reason {
		case serverapi.WorkflowTaskListScopeReasonNoLinkedWorkflows:
			reason = taskpb.ListScopeErrorReason_LIST_SCOPE_ERROR_REASON_NO_LINKED_WORKFLOWS
		case serverapi.WorkflowTaskListScopeReasonWorkflowNotLinked:
			reason = taskpb.ListScopeErrorReason_LIST_SCOPE_ERROR_REASON_WORKFLOW_NOT_LINKED
		case serverapi.WorkflowTaskListScopeReasonWorkflowRequiredColumns:
			reason = taskpb.ListScopeErrorReason_LIST_SCOPE_ERROR_REASON_WORKFLOW_REQUIRED_FOR_COLUMNS
		default:
			return binaryInternalFailure(err)
		}
		detail := &taskpb.ListScopeErrorDetails{Reason: reason, ProjectId: request.GetProjectId()}
		if scope.WorkflowID != nil {
			value := scope.WorkflowID.String()
			detail.WorkflowId = &value
		}
		return detail
	}
	return binaryTaskReadFailure(request, err)
}

func binaryTaskReadFailure[Request interface{ GetProjectId() string }](request Request, err error) proto.Message {
	var label *serverapi.WorkflowLabelError
	var validation serverapi.WorkflowRequestValidationError
	switch {
	case errors.As(err, &label):
		switch label.Reason {
		case serverapi.WorkflowLabelErrorReasonLabelNotFound:
			return &taskpb.LabelNotFoundDetails{ProjectId: *label.ProjectID, LabelId: *label.LabelID}
		case serverapi.WorkflowLabelErrorReasonWrongProject:
			return &taskpb.WrongProjectDetails{ProjectId: *label.ProjectID, LabelId: *label.LabelID}
		case serverapi.WorkflowLabelErrorReasonInvalidFilter:
			return &taskpb.InvalidFilterDetails{Field: *label.Field}
		default:
			return binaryInternalFailure(err)
		}
	case errors.As(err, &validation):
		return &taskpb.InvalidFilterDetails{Field: validation.Field}
	default:
		return binaryWorkflowProjectFailure(request, err)
	}
}
