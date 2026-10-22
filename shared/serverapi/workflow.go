package serverapi

import (
	"errors"
	"fmt"
	"strings"

	"core/shared/runtimeids"
)

const (
	WorkflowRequestErrorRequired     = "workflow.request.required"
	WorkflowRequestErrorInvalidKey   = "workflow.request.invalid_key"
	WorkflowRequestErrorInvalidValue = "workflow.request.invalid_value"
	WorkflowRequestErrorInvalidMode  = "workflow.request.invalid_mode"
	WorkflowRequestErrorTooLong      = "workflow.request.too_long"
)

const WorkflowPaginationMaxLimit = 100
const WorkflowTaskListMaxSortSelectors = 7
const WorkflowBoardNodeCardsMaxPageSize = 25

type WorkflowRequestValidationError struct {
	Code    string
	Field   string
	Message string
}

func (e WorkflowRequestValidationError) Error() string {
	if strings.TrimSpace(e.Field) == "" {
		return e.Message
	}
	return e.Field + ": " + e.Message
}

type WorkflowOutputField struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type WorkflowValidationError struct {
	Code              string                          `json:"code"`
	Message           string                          `json:"message"`
	WorkflowID        *runtimeids.WorkflowID          `json:"workflow_id,omitempty"`
	NodeID            *string                         `json:"node_id"`
	TransitionGroupID *string                         `json:"transition_group_id"`
	EdgeID            *string                         `json:"edge_id"`
	Details           *WorkflowValidationErrorDetails `json:"details,omitempty"`
	RelatedIDs        []string                        `json:"related_ids,omitempty"`
	BlocksContext     bool                            `json:"blocks_context"`
}

type WorkflowValidationErrorDetails struct {
	FieldName      string  `json:"field_name,omitempty"`
	InputName      string  `json:"input_name,omitempty"`
	Placeholder    string  `json:"placeholder,omitempty"`
	ProviderEdgeID *string `json:"provider_edge_id"`
	Role           *string `json:"role,omitempty"`
	RequiredTool   *string `json:"required_tool,omitempty"`
}

type WorkflowTaskMutationSelfTargetError struct {
	TaskID string
}

func (e *WorkflowTaskMutationSelfTargetError) Error() string {
	if e == nil {
		return "workflow task mutation self-target denied"
	}
	return fmt.Sprintf("workflow task mutation self-target denied for task %q", e.TaskID)
}

type WorkflowTaskStartConflictReason string

const WorkflowTaskStartConflictAlreadyStarted WorkflowTaskStartConflictReason = "already_started"

type WorkflowTaskStartConflictError struct {
	TaskID string
	Reason WorkflowTaskStartConflictReason
}

func (e *WorkflowTaskStartConflictError) Error() string { return "workflow task start conflict" }

var ErrWorkflowTaskCompleteTargetNotFound = errors.New("workflow task completion target not found")
var ErrWorkflowTaskCompleteSelectorAmbiguous = errors.New("workflow task completion selector is ambiguous")

type WorkflowTaskCompleteSelectorAmbiguousError struct{ Message string }

func (e WorkflowTaskCompleteSelectorAmbiguousError) Error() string {
	message := strings.TrimSpace(e.Message)
	if message == "" {
		return ErrWorkflowTaskCompleteSelectorAmbiguous.Error()
	}
	return message
}

func (e WorkflowTaskCompleteSelectorAmbiguousError) Is(target error) bool {
	return target == ErrWorkflowTaskCompleteSelectorAmbiguous
}

type WorkflowTaskListScopeErrorReason string

const (
	WorkflowTaskListScopeReasonNoLinkedWorkflows       WorkflowTaskListScopeErrorReason = "no_linked_workflows"
	WorkflowTaskListScopeReasonWorkflowNotLinked       WorkflowTaskListScopeErrorReason = "workflow_not_linked"
	WorkflowTaskListScopeReasonWorkflowRequiredColumns WorkflowTaskListScopeErrorReason = "workflow_required_for_columns"
)

type WorkflowTaskListScopeError struct {
	Reason     WorkflowTaskListScopeErrorReason
	ProjectID  *string
	WorkflowID *runtimeids.WorkflowID
}

func (e *WorkflowTaskListScopeError) Error() string {
	if e == nil {
		return "workflow task list scope error"
	}
	return "workflow task list scope error: " + string(e.Reason)
}

func workflowRequestError(code string, field string, message string) error {
	return WorkflowRequestValidationError{Code: code, Field: field, Message: message}
}
