package serverapi

import (
	"strings"
	"testing"

	"builder/shared/workflowkey"
)

func TestWorkflowCreateUpdateRequestValidation(t *testing.T) {
	if err := (WorkflowCreateRequest{Name: "Pipeline"}).Validate(); err != nil {
		t.Fatalf("valid create request rejected: %v", err)
	}
	if err := (WorkflowCreateRequest{Name: " "}).Validate(); err == nil || !strings.Contains(err.Error(), "name is required") {
		t.Fatalf("empty name error = %v", err)
	}
	if err := (WorkflowUpdateRequest{WorkflowID: "workflow-1", Name: strings.Repeat("x", 121)}).Validate(); err == nil || !strings.Contains(err.Error(), "<= 120") {
		t.Fatalf("long name error = %v", err)
	}
}

func TestWorkflowNodeAndEdgeRequestValidation(t *testing.T) {
	validNode := WorkflowNodeAddRequest{WorkflowID: "workflow-1", Key: "implement", Kind: "agent", DisplayName: "Implement", OutputFields: []WorkflowOutputField{{Name: "summary", Description: "Summary"}}}
	if err := validNode.Validate(); err != nil {
		t.Fatalf("valid node request rejected: %v", err)
	}
	invalidNode := validNode
	invalidNode.Key = "Bad-Key"
	if err := invalidNode.Validate(); err == nil || !strings.Contains(err.Error(), workflowkey.Description) {
		t.Fatalf("invalid node key error = %v", err)
	} else if validationErr, ok := err.(WorkflowRequestValidationError); !ok || validationErr.Code != WorkflowRequestErrorInvalidKey {
		t.Fatalf("invalid node key error type = %#v", err)
	}
	invalidNode = validNode
	invalidNode.DisplayName = ""
	if err := invalidNode.Validate(); err == nil || !strings.Contains(err.Error(), "display_name") {
		t.Fatalf("invalid display name error = %v", err)
	}

	validEdge := WorkflowEdgeAddRequest{WorkflowID: "workflow-1", TransitionGroupID: "group-1", Key: "done", TargetNodeID: "node-2", ContextMode: "new_session", InputBindings: []WorkflowInputBinding{{Name: "task", Source: "task", Field: "body"}}, OutputRequirements: []WorkflowOutputRequirement{{FieldName: "summary"}}}
	if err := validEdge.Validate(); err != nil {
		t.Fatalf("valid edge request rejected: %v", err)
	}
	invalidEdge := validEdge
	invalidEdge.OutputRequirements = []WorkflowOutputRequirement{{FieldName: "Summary"}}
	if err := invalidEdge.Validate(); err == nil || !strings.Contains(err.Error(), workflowkey.Description) {
		t.Fatalf("invalid output requirement error = %v", err)
	}
}

func TestWorkflowTaskAndCommentRequestValidation(t *testing.T) {
	if err := (WorkflowTaskCreateRequest{ProjectID: "project-1", Title: "Task"}).Validate(); err != nil {
		t.Fatalf("valid task create rejected: %v", err)
	}
	if err := (WorkflowTaskCreateRequest{ProjectID: "project-1", Title: "", Body: "Body"}).Validate(); err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("empty title error = %v", err)
	}
	if err := (WorkflowTaskUpdateRequest{TaskID: "task-1", Title: "Task"}).Validate(); err != nil {
		t.Fatalf("valid task update rejected: %v", err)
	}
	if err := (WorkflowTaskUpdateRequest{TaskID: "task-1", Title: " "}).Validate(); err == nil || !strings.Contains(err.Error(), "title") {
		t.Fatalf("empty update title error = %v", err)
	}
	if err := (WorkflowTaskStartRequest{TaskID: "task-1"}).Validate(); err != nil {
		t.Fatalf("valid task start rejected: %v", err)
	}
	if err := (WorkflowTaskResumeRequest{TaskID: "task-1"}).Validate(); err != nil {
		t.Fatalf("valid task resume rejected: %v", err)
	}
	if err := (WorkflowTaskInterruptRequest{TaskID: "task-1"}).Validate(); err != nil {
		t.Fatalf("valid task interrupt rejected: %v", err)
	}
	if err := (WorkflowTaskApproveRequest{TaskTransitionID: "transition-1"}).Validate(); err != nil {
		t.Fatalf("valid task approval rejected: %v", err)
	}
	if err := (WorkflowTaskApproveRequest{}).Validate(); err == nil || !strings.Contains(err.Error(), "transition_id") {
		t.Fatalf("empty legacy task approval error = %v", err)
	}
	if err := (WorkflowTaskQuestionAnswerRequest{ClientRequestID: "req-1", TaskID: "task-1", AskID: "ask-1", FreeformAnswer: "answer"}).Validate(); err != nil {
		t.Fatalf("valid task question answer rejected: %v", err)
	}
	if err := (WorkflowTaskQuestionAnswerRequest{ClientRequestID: "req-1", TaskID: "task-1", AskID: "ask-1", SelectedOptionNumber: 1, FreeformAnswer: "because"}).Validate(); err != nil {
		t.Fatalf("valid selected option plus freeform rejected: %v", err)
	}
	if err := (WorkflowTaskQuestionAnswerRequest{ClientRequestID: "req-1", TaskID: "task-1", AskID: "ask-1", SelectedOptionNumber: -1}).Validate(); err == nil || !strings.Contains(err.Error(), "selected_option_number") {
		t.Fatalf("negative selected option error = %v", err)
	}
	if err := (WorkflowTaskQuestionAnswerRequest{ClientRequestID: "req-1", TaskID: "task-1", AskID: "ask-1", ErrorMessage: "err", FreeformAnswer: "answer"}).Validate(); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("conflicting task question answer error = %v", err)
	}
	if err := (WorkflowTaskQuestionAnswerRequest{ClientRequestID: "req-1", TaskID: "task-1", AskID: "ask-1", Answer: "one", FreeformAnswer: "two"}).Validate(); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("multi-mode task question answer error = %v", err)
	}
	if err := (WorkflowTaskCommentAddRequest{TaskID: "task-1", Body: "comment", Author: "user"}).Validate(); err != nil {
		t.Fatalf("valid comment add rejected: %v", err)
	}
	if err := (WorkflowTaskCommentAddRequest{TaskID: "task-1", Body: "", Author: "user"}).Validate(); err == nil || !strings.Contains(err.Error(), "body") {
		t.Fatalf("empty comment body error = %v", err)
	}
	if err := (WorkflowTaskActivityListRequest{TaskID: "task-1", PageSize: 10}).Validate(); err != nil {
		t.Fatalf("valid activity list rejected: %v", err)
	}
	if err := (WorkflowTaskActivityListRequest{TaskID: "task-1", PageSize: -1}).Validate(); err == nil || !strings.Contains(err.Error(), "page_size") {
		t.Fatalf("invalid activity page size error = %v", err)
	}
	if err := (WorkflowTaskTeleportTargetRequest{TaskID: "task-1"}).Validate(); err != nil {
		t.Fatalf("valid teleport target rejected: %v", err)
	}
	if err := (WorkflowTaskTeleportTargetRequest{}).Validate(); err == nil || !strings.Contains(err.Error(), "task_id") {
		t.Fatalf("empty teleport task id error = %v", err)
	}
}

func TestWorkflowValidateRequestValidation(t *testing.T) {
	for _, mode := range []WorkflowValidationMode{"", WorkflowValidationModeDraft, WorkflowValidationModeTaskCreation, WorkflowValidationModeExecution} {
		if err := (WorkflowValidateRequest{WorkflowID: "workflow-1", Mode: mode}).Validate(); err != nil {
			t.Fatalf("mode %q rejected: %v", mode, err)
		}
	}
	if err := (WorkflowValidateRequest{WorkflowID: "workflow-1", Mode: "other"}).Validate(); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("invalid mode error = %v", err)
	}
}

func TestWorkflowProjectLinkRequestValidation(t *testing.T) {
	if err := (WorkflowLinkProjectRequest{ProjectID: "project-1", WorkflowID: "workflow-1"}).Validate(); err != nil {
		t.Fatalf("valid link request rejected: %v", err)
	}
	if err := (WorkflowListProjectLinksRequest{ProjectID: "project-1"}).Validate(); err != nil {
		t.Fatalf("valid list links request rejected: %v", err)
	}
	if err := (WorkflowSetDefaultProjectLinkRequest{ProjectID: "project-1", WorkflowID: "workflow-1"}).Validate(); err != nil {
		t.Fatalf("valid set default request rejected: %v", err)
	}
	if err := (WorkflowSetDefaultProjectLinkRequest{ProjectID: "", WorkflowID: "workflow-1"}).Validate(); err == nil || !strings.Contains(err.Error(), "project_id") {
		t.Fatalf("empty project id error = %v", err)
	}
}

func TestWorkflowDeleteRequestValidation(t *testing.T) {
	if err := (WorkflowDeletePreviewRequest{WorkflowID: "workflow-1"}).Validate(); err != nil {
		t.Fatalf("valid delete preview rejected: %v", err)
	}
	if err := (WorkflowDeletePreviewRequest{}).Validate(); err == nil || !strings.Contains(err.Error(), "workflow_id") {
		t.Fatalf("empty delete preview workflow id error = %v", err)
	}
	if err := (WorkflowDeleteRequest{
		WorkflowID:            "workflow-1",
		Confirmed:             true,
		ExpectedGraphRevision: 1,
		ExpectedProjectCount:  1,
		ExpectedLinkCount:     1,
		ExpectedTaskCount:     1,
	}).Validate(); err != nil {
		t.Fatalf("valid delete request rejected: %v", err)
	}
	if err := (WorkflowDeleteRequest{}).Validate(); err == nil || !strings.Contains(err.Error(), "workflow_id") {
		t.Fatalf("empty delete workflow id error = %v", err)
	}
	if err := (WorkflowDeleteRequest{WorkflowID: "workflow-1", ExpectedGraphRevision: -1}).Validate(); err == nil || !strings.Contains(err.Error(), "expected_graph_revision") {
		t.Fatalf("negative graph revision error = %v", err)
	}
	if err := (WorkflowDeleteRequest{WorkflowID: "workflow-1", ExpectedProjectCount: -1}).Validate(); err == nil || !strings.Contains(err.Error(), "expected_project_count") {
		t.Fatalf("negative project count error = %v", err)
	}
	if err := (WorkflowDeleteRequest{WorkflowID: "workflow-1", ExpectedLinkCount: -1}).Validate(); err == nil || !strings.Contains(err.Error(), "expected_link_count") {
		t.Fatalf("negative link count error = %v", err)
	}
	if err := (WorkflowDeleteRequest{WorkflowID: "workflow-1", ExpectedTaskCount: -1}).Validate(); err == nil || !strings.Contains(err.Error(), "expected_task_count") {
		t.Fatalf("negative task count error = %v", err)
	}
}
