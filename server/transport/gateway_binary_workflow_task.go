package transport

import (
	"database/sql"
	"errors"

	"core/server/workflow"
	"core/server/workflowstore"
	"core/shared/apicontract"
	"core/shared/protoapi"
	taskpb "core/shared/protoapi/gen/kent/api/workflow_task"
	"core/shared/serverapi"
	"core/shared/worktreecontract"

	"google.golang.org/protobuf/proto"
)

func registerWorkflowTaskGatewayBinaryBindings(bindings map[string]gatewayBinaryBinding) error {
	read := taskpb.File_kent_api_workflow_task_read_proto.Services().ByName("TaskReadService")
	board := taskpb.File_kent_api_workflow_task_read_proto.Services().ByName("BoardReadService")
	lifecycle := taskpb.File_kent_api_workflow_task_lifecycle_proto.Services().ByName("TaskLifecycleService")
	return errors.Join(
		registerWorkflowUnary(bindings, read, "List",
			func() *taskpb.ListRequest { return &taskpb.ListRequest{} },
			apicontract.WorkflowService.ListWorkflowTasks, binaryTaskListFailure),
		registerWorkflowUnary(bindings, read, "GetProjectGroupCounts",
			func() *taskpb.ProjectTaskGroupCountsRequest { return &taskpb.ProjectTaskGroupCountsRequest{} },
			apicontract.WorkflowService.GetWorkflowProjectTaskGroupCounts, binaryWorkflowProjectFailure[*taskpb.ProjectTaskGroupCountsRequest]),
		registerWorkflowUnary(bindings, read, "Get",
			func() *taskpb.GetRequest { return &taskpb.GetRequest{} },
			apicontract.WorkflowService.GetWorkflowTask, binaryTaskGetFailure),
		registerWorkflowUnary(bindings, read, "Search",
			func() *taskpb.SearchRequest { return &taskpb.SearchRequest{} },
			apicontract.WorkflowService.SearchWorkflowTasks, binaryTaskSearchFailure),
		registerWorkflowUnary(bindings, board, "Get",
			func() *taskpb.BoardGetRequest { return &taskpb.BoardGetRequest{} },
			apicontract.WorkflowService.GetWorkflowBoard, binaryTaskReadFailure[*taskpb.BoardGetRequest]),
		registerWorkflowUnary(bindings, board, "ListNodeCards",
			func() *taskpb.BoardNodeCardsListRequest { return &taskpb.BoardNodeCardsListRequest{} },
			apicontract.WorkflowService.ListWorkflowBoardNodeCards, binaryTaskReadFailure[*taskpb.BoardNodeCardsListRequest]),
		registerWorkflowUnary(bindings, lifecycle, "Create",
			func() *taskpb.CreateRequest { return &taskpb.CreateRequest{} },
			apicontract.WorkflowService.CreateWorkflowTask, binaryTaskCreateFailure),
		registerWorkflowUnary(bindings, lifecycle, "Update",
			func() *taskpb.UpdateRequest { return &taskpb.UpdateRequest{} },
			apicontract.WorkflowService.UpdateWorkflowTask, binaryTaskEntityFailure[*taskpb.UpdateRequest]),
		registerWorkflowUnary(bindings, lifecycle, "Delete",
			func() *taskpb.DeleteRequest { return &taskpb.DeleteRequest{} },
			apicontract.WorkflowService.DeleteWorkflowTask, binaryTaskDeleteFailure),
		registerWorkflowUnary(bindings, lifecycle, "Start",
			func() *taskpb.StartRequest { return &taskpb.StartRequest{} },
			apicontract.WorkflowService.StartWorkflowTask, binaryTaskExecutionFailure[*taskpb.StartRequest]),
		registerWorkflowUnary(bindings, lifecycle, "Resume",
			func() *taskpb.ResumeRequest { return &taskpb.ResumeRequest{} },
			apicontract.WorkflowService.ResumeWorkflowTask, binaryTaskExecutionFailure[*taskpb.ResumeRequest]),
	)
}

func binaryTaskExecutionFailure[Request interface{ GetTaskId() string }](request Request, err error) proto.Message {
	var self *serverapi.WorkflowTaskMutationSelfTargetError
	var conflict *serverapi.WorkflowTaskStartConflictError
	var resolution *serverapi.WorkflowExecutionTargetResolutionError
	var locked *serverapi.WorkflowLockedExecutionTargetError
	var branch *serverapi.WorkflowTaskInitialBranchError
	var contextSelection *serverapi.WorkflowTaskContextSelectionRequiredError
	var retained *worktreecontract.SetupRetainedError
	switch {
	case errors.As(err, &self):
		return &taskpb.SelfTargetDetails{TaskId: self.TaskID}
	case errors.As(err, &conflict) && conflict.Reason == serverapi.WorkflowTaskStartConflictAlreadyStarted:
		return &taskpb.StartConflictDetails{TaskId: conflict.TaskID, Reason: taskpb.StartConflictReason_START_CONFLICT_REASON_ALREADY_STARTED}
	case errors.As(err, &resolution):
		code, conversionErr := protoapi.TaskExecutionResolutionCode.Encode(string(resolution.Code))
		if conversionErr != nil {
			return binaryInternalFailure(conversionErr)
		}
		return &taskpb.ExecutionTargetResolutionDetails{Code: code, RequestedRef: resolution.RequestedRef}
	case errors.As(err, &locked):
		return serverapi.WorkflowLockedTargetDetails(locked.Cause)
	case errors.As(err, &branch):
		reason, conversionErr := protoapi.TaskInitialBranchReason.Encode(string(branch.Reason))
		if conversionErr != nil {
			return binaryInternalFailure(conversionErr)
		}
		return &taskpb.InitialBranchDetails{
			Reason: reason, BranchName: branch.BranchName, Ref: branch.Ref, Remote: branch.Remote,
			ExistingBranchName: branch.ExistingBranchName,
		}
	case errors.As(err, &contextSelection):
		return &taskpb.ContextSelectionRequiredDetails{TaskId: contextSelection.TaskID}
	case errors.As(err, &retained):
		return retained.Details
	default:
		return binaryTaskEntityFailure(request, err)
	}
}

func binaryTaskDeleteFailure(request *taskpb.DeleteRequest, err error) proto.Message {
	if errors.Is(err, worktreecontract.ErrWorktreeBlocked) {
		return worktreeDeletionFailure(request, err)
	}
	return binaryTaskEntityFailure(request, err)
}

func binaryTaskEntityFailure[Request interface{ GetTaskId() string }](request Request, err error) proto.Message {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, serverapi.ErrWorkflowTaskNotFound) {
		id := request.GetTaskId()
		return &taskpb.TaskNotFoundDetails{TaskId: &id}
	}
	return binaryWorkflowTaskLabelFailureDetail(nil, err)
}

func binaryTaskCreateFailure(request *taskpb.CreateRequest, err error) proto.Message {
	var selection workflowstore.TaskWorkflowSelectionError
	var conflict workflowstore.TaskCreateConflictError
	var dependency workflow.TaskDependencyPolicyError
	switch {
	case errors.As(err, &selection):
		var reason taskpb.CreateSelectionReason
		switch selection.Reason {
		case workflowstore.TaskWorkflowSelectionNoLinkedWorkflows:
			reason = taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_NO_LINKED_WORKFLOWS
		case workflowstore.TaskWorkflowSelectionWorkflowNotLinked:
			reason = taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_WORKFLOW_NOT_LINKED
		case workflowstore.TaskWorkflowSelectionAmbiguousWithoutDefault:
			reason = taskpb.CreateSelectionReason_CREATE_SELECTION_REASON_AMBIGUOUS_WITHOUT_DEFAULT
		default:
			return binaryInternalFailure(err)
		}
		detail := &taskpb.CreateSelectionDetails{Reason: reason, ProjectId: selection.ProjectID}
		if selection.WorkflowID != nil {
			value := selection.WorkflowID.String()
			detail.WorkflowId = &value
		}
		return detail
	case errors.As(err, &conflict) && conflict.Reason == workflowstore.TaskCreateConflictSerialization:
		return &taskpb.CreateConflictDetails{Reason: taskpb.CreateConflictReason_CREATE_CONFLICT_REASON_SERIALIZATION}
	case errors.As(err, &dependency):
		return binaryTaskDependencyFailure(dependency)
	case errors.Is(err, serverapi.ErrProjectNotFound):
		return &taskpb.LabelErrorDetails{
			Reason: taskpb.LabelErrorReason_LABEL_ERROR_REASON_PROJECT_NOT_FOUND, ProjectId: &request.ProjectId,
		}
	default:
		return binaryWorkflowTaskLabelFailureDetail(nil, err)
	}
}

func binaryTaskDependencyFailure(failure workflow.TaskDependencyPolicyError) proto.Message {
	var reason taskpb.DependencyErrorReason
	switch failure.Reason {
	case workflow.TaskDependencyMissingTask:
		reason = taskpb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_MISSING_TASK
	case workflow.TaskDependencySelf:
		reason = taskpb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_SELF
	case workflow.TaskDependencyProjectMismatch:
		reason = taskpb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_PROJECT_MISMATCH
	case workflow.TaskDependencyReciprocal:
		reason = taskpb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_RECIPROCAL
	case workflow.TaskDependencyBlockerLimit:
		reason = taskpb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_BLOCKER_LIMIT
	case workflow.TaskDependencyBlockedLimit:
		reason = taskpb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_BLOCKED_LIMIT
	default:
		return binaryInternalFailure(failure)
	}
	detail := &taskpb.DependencyErrorDetails{
		Reason: reason, BlockerTaskId: string(failure.BlockerTaskID), BlockedTaskId: string(failure.BlockedTaskID),
	}
	if failure.MissingTaskID != nil {
		value := string(*failure.MissingTaskID)
		detail.MissingTaskId = &value
	}
	if failure.CurrentCount != nil {
		value, err := protoapi.Int32(int(*failure.CurrentCount), "current_count")
		if err != nil {
			return binaryInternalFailure(err)
		}
		detail.CurrentCount = &value
	}
	if failure.Limit != nil {
		value, err := protoapi.Int32(int(*failure.Limit), "limit")
		if err != nil {
			return binaryInternalFailure(err)
		}
		detail.Limit = &value
	}
	return detail
}

func binaryTaskSearchFailure(request *taskpb.SearchRequest, err error) proto.Message {
	var search *serverapi.TaskSearchError
	if errors.As(err, &search) && search.Reason == serverapi.TaskSearchErrorReasonNormalizedTooShort {
		return &taskpb.NormalizedTooShortDetails{}
	}
	return binaryWorkflowCreateFailure(request, err)
}

func binaryTaskGetFailure(request *taskpb.GetRequest, err error) proto.Message {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, serverapi.ErrWorkflowTaskNotFound) {
		return &taskpb.TaskNotFoundDetails{TaskId: request.TaskId, ProjectId: request.ProjectId, ShortId: request.ShortId}
	}
	return binaryWorkflowCreateFailure(request, err)
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
