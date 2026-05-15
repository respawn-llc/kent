package serverapi

import (
	"fmt"
	"strings"

	"builder/shared/workflowkey"
)

const (
	WorkflowRequestErrorRequired    = "workflow.request.required"
	WorkflowRequestErrorInvalidKey  = "workflow.request.invalid_key"
	WorkflowRequestErrorInvalidMode = "workflow.request.invalid_mode"
	WorkflowRequestErrorTooLong     = "workflow.request.too_long"
)

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

type WorkflowValidationMode string

const (
	WorkflowValidationModeDraft        WorkflowValidationMode = "draft"
	WorkflowValidationModeTaskCreation WorkflowValidationMode = "task_creation"
	WorkflowValidationModeExecution    WorkflowValidationMode = "execution"
)

type WorkflowRecord struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Description   string `json:"description"`
	GraphRevision int64  `json:"graph_revision"`
}

type WorkflowNode struct {
	ID             string                `json:"id"`
	WorkflowID     string                `json:"workflow_id"`
	Key            string                `json:"key"`
	Kind           string                `json:"kind"`
	DisplayName    string                `json:"display_name"`
	SubagentRole   string                `json:"subagent_role,omitempty"`
	PromptTemplate string                `json:"prompt_template,omitempty"`
	OutputFields   []WorkflowOutputField `json:"output_fields,omitempty"`
}

type WorkflowTransitionGroup struct {
	ID           string `json:"id"`
	WorkflowID   string `json:"workflow_id"`
	SourceNodeID string `json:"source_node_id"`
	TransitionID string `json:"transition_id"`
	DisplayName  string `json:"display_name"`
}

type WorkflowEdge struct {
	ID                 string                      `json:"id"`
	WorkflowID         string                      `json:"workflow_id"`
	TransitionGroupID  string                      `json:"transition_group_id"`
	Key                string                      `json:"key"`
	TargetNodeID       string                      `json:"target_node_id"`
	RequiresApproval   bool                        `json:"requires_approval"`
	ContextMode        string                      `json:"context_mode"`
	InputBindings      []WorkflowInputBinding      `json:"input_bindings,omitempty"`
	OutputRequirements []WorkflowOutputRequirement `json:"output_requirements,omitempty"`
}

type WorkflowOutputField struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type WorkflowOutputRequirement struct {
	FieldName string `json:"field_name"`
}

type WorkflowInputBinding struct {
	Name   string `json:"name"`
	Source string `json:"source"`
	Field  string `json:"field"`
}

type WorkflowDefinition struct {
	Workflow         WorkflowRecord            `json:"workflow"`
	Nodes            []WorkflowNode            `json:"nodes"`
	TransitionGroups []WorkflowTransitionGroup `json:"transition_groups"`
	Edges            []WorkflowEdge            `json:"edges"`
}

type WorkflowCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type WorkflowCreateResponse struct {
	Workflow WorkflowRecord `json:"workflow"`
}

type WorkflowUpdateRequest struct {
	WorkflowID  string `json:"workflow_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type WorkflowListRequest struct{}

type WorkflowListResponse struct {
	Workflows []WorkflowRecord `json:"workflows"`
}

type WorkflowGetRequest struct {
	WorkflowID string `json:"workflow_id"`
}

type WorkflowGetResponse struct {
	Definition WorkflowDefinition `json:"definition"`
}

type WorkflowNodeAddRequest struct {
	WorkflowID     string                `json:"workflow_id"`
	NodeID         string                `json:"node_id,omitempty"`
	Key            string                `json:"key"`
	Kind           string                `json:"kind"`
	DisplayName    string                `json:"display_name"`
	SubagentRole   string                `json:"subagent_role,omitempty"`
	PromptTemplate string                `json:"prompt_template,omitempty"`
	OutputFields   []WorkflowOutputField `json:"output_fields,omitempty"`
}

type WorkflowNodeAddResponse struct {
	GraphRevision int64 `json:"graph_revision"`
}

type WorkflowTransitionGroupAddRequest struct {
	WorkflowID   string `json:"workflow_id"`
	GroupID      string `json:"group_id,omitempty"`
	SourceNodeID string `json:"source_node_id"`
	TransitionID string `json:"transition_id"`
	DisplayName  string `json:"display_name,omitempty"`
}

type WorkflowTransitionGroupAddResponse struct {
	GraphRevision int64 `json:"graph_revision"`
}

type WorkflowEdgeAddRequest struct {
	WorkflowID         string                      `json:"workflow_id"`
	EdgeID             string                      `json:"edge_id,omitempty"`
	TransitionGroupID  string                      `json:"transition_group_id"`
	Key                string                      `json:"key"`
	TargetNodeID       string                      `json:"target_node_id"`
	ContextMode        string                      `json:"context_mode"`
	RequiresApproval   bool                        `json:"requires_approval"`
	InputBindings      []WorkflowInputBinding      `json:"input_bindings,omitempty"`
	OutputRequirements []WorkflowOutputRequirement `json:"output_requirements,omitempty"`
}

type WorkflowEdgeAddResponse struct {
	GraphRevision int64 `json:"graph_revision"`
}

type WorkflowLinkProjectRequest struct {
	ProjectID  string `json:"project_id"`
	WorkflowID string `json:"workflow_id"`
	Default    bool   `json:"default"`
}

type WorkflowLinkProjectResponse struct {
	Link ProjectWorkflowLink `json:"link"`
}

type WorkflowListProjectLinksRequest struct {
	ProjectID string `json:"project_id"`
}

type WorkflowListProjectLinksResponse struct {
	Links []ProjectWorkflowLink `json:"links"`
}

type WorkflowSetDefaultProjectLinkRequest struct {
	ProjectID  string `json:"project_id"`
	WorkflowID string `json:"workflow_id"`
}

type WorkflowSetDefaultProjectLinkResponse struct {
	Link ProjectWorkflowLink `json:"link"`
}

type ProjectWorkflowLink struct {
	ID               string `json:"id"`
	ProjectID        string `json:"project_id"`
	WorkflowID       string `json:"workflow_id"`
	Default          bool   `json:"default"`
	UnlinkedAtUnixMs int64  `json:"unlinked_at_unix_ms"`
}

type WorkflowUnlinkProjectRequest struct {
	LinkID                   string `json:"link_id"`
	ReplacementDefaultLinkID string `json:"replacement_default_link_id,omitempty"`
}

type WorkflowValidateRequest struct {
	WorkflowID string                 `json:"workflow_id"`
	Mode       WorkflowValidationMode `json:"mode"`
}

type WorkflowValidateResponse struct {
	Valid  bool                      `json:"valid"`
	Errors []WorkflowValidationError `json:"errors"`
}

type WorkflowValidationError struct {
	Code              string   `json:"code"`
	Message           string   `json:"message"`
	WorkflowID        string   `json:"workflow_id,omitempty"`
	NodeID            string   `json:"node_id,omitempty"`
	TransitionGroupID string   `json:"transition_group_id,omitempty"`
	EdgeID            string   `json:"edge_id,omitempty"`
	RelatedIDs        []string `json:"related_ids,omitempty"`
	BlocksContext     bool     `json:"blocks_context"`
}

type WorkflowTaskCreateRequest struct {
	ProjectID  string `json:"project_id"`
	WorkflowID string `json:"workflow_id,omitempty"`
	Title      string `json:"title"`
	Body       string `json:"body"`
	SourceURL  string `json:"source_url,omitempty"`
}

type WorkflowTaskCreateResponse struct {
	Task WorkflowTaskSummary `json:"task"`
}

type WorkflowTaskStartRequest struct {
	TaskID string `json:"task_id"`
}

type WorkflowTaskStartResponse struct {
	TransitionID string `json:"transition_id"`
	PlacementID  string `json:"placement_id"`
	RunID        string `json:"run_id"`
}

type WorkflowTaskApproveRequest struct {
	TransitionID string `json:"transition_id"`
}

type WorkflowTaskApproveResponse struct {
	TransitionID string   `json:"transition_id"`
	State        string   `json:"state"`
	PlacementIDs []string `json:"placement_ids,omitempty"`
	RunIDs       []string `json:"run_ids,omitempty"`
}

type WorkflowTaskMoveRequest struct {
	TaskID       string            `json:"task_id"`
	TargetNodeID string            `json:"target_node_id"`
	OutputValues map[string]string `json:"output_values,omitempty"`
	Commentary   string            `json:"commentary,omitempty"`
}

type WorkflowTaskMoveResponse struct {
	TransitionID string   `json:"transition_id"`
	State        string   `json:"state"`
	PlacementIDs []string `json:"placement_ids,omitempty"`
	RunIDs       []string `json:"run_ids,omitempty"`
}

type WorkflowTaskCancelRequest struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason,omitempty"`
}

type WorkflowTaskCommentAddRequest struct {
	TaskID   string `json:"task_id"`
	Body     string `json:"body"`
	Author   string `json:"author"`
	AuthorID string `json:"author_id,omitempty"`
}

type WorkflowTaskCommentAddResponse struct {
	Comment WorkflowTaskComment `json:"comment"`
}

type WorkflowTaskCommentListRequest struct {
	TaskID         string `json:"task_id"`
	IncludeDeleted bool   `json:"include_deleted"`
}

type WorkflowTaskCommentListResponse struct {
	Comments []WorkflowTaskComment `json:"comments"`
}

type WorkflowTaskCommentReplaceRequest struct {
	CommentID string `json:"comment_id"`
	Body      string `json:"body"`
}

type WorkflowTaskCommentDeleteRequest struct {
	CommentID string `json:"comment_id"`
}

type WorkflowBoardRequest struct {
	ProjectID string `json:"project_id"`
}

type WorkflowBoardResponse struct {
	Board WorkflowBoard `json:"board"`
}

type WorkflowBoard struct {
	ProjectID string                  `json:"project_id"`
	Workflows []WorkflowBoardWorkflow `json:"workflows"`
}

type WorkflowBoardWorkflow struct {
	Workflow WorkflowRecord        `json:"workflow"`
	Nodes    []WorkflowBoardNode   `json:"nodes"`
	Tasks    []WorkflowTaskSummary `json:"tasks"`
}

type WorkflowBoardNode struct {
	Node             WorkflowNode          `json:"node"`
	ActivePlacements []WorkflowPlacement   `json:"active_placements"`
	Tasks            []WorkflowTaskSummary `json:"tasks"`
}

type WorkflowTaskGetRequest struct {
	TaskID string `json:"task_id"`
}

type WorkflowTaskGetResponse struct {
	Task WorkflowTaskDetail `json:"task"`
}

type WorkflowTaskSummary struct {
	ID            string   `json:"id"`
	ProjectID     string   `json:"project_id"`
	WorkflowID    string   `json:"workflow_id"`
	ShortID       string   `json:"short_id"`
	Title         string   `json:"title"`
	CanceledAt    int64    `json:"canceled_at_unix_ms"`
	CancelReason  string   `json:"cancel_reason,omitempty"`
	Done          bool     `json:"done"`
	ActiveNodeIDs []string `json:"active_node_ids,omitempty"`
}

type WorkflowTaskDetail struct {
	Summary     WorkflowTaskSummary      `json:"summary"`
	Placements  []WorkflowPlacement      `json:"placements"`
	Runs        []WorkflowRun            `json:"runs"`
	Transitions []WorkflowTaskTransition `json:"transitions"`
	Comments    []WorkflowTaskComment    `json:"comments"`
}

type WorkflowPlacement struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id"`
	NodeID string `json:"node_id"`
	State  string `json:"state"`
}

type WorkflowRun struct {
	ID                  string `json:"id"`
	TaskID              string `json:"task_id"`
	PlacementID         string `json:"placement_id"`
	NodeID              string `json:"node_id"`
	SessionID           string `json:"session_id,omitempty"`
	Generation          int64  `json:"generation"`
	StartedAtUnixMs     int64  `json:"started_at_unix_ms"`
	CompletedAtUnixMs   int64  `json:"completed_at_unix_ms"`
	InterruptedAtUnixMs int64  `json:"interrupted_at_unix_ms"`
	InterruptionReason  string `json:"interruption_reason,omitempty"`
	WaitingAskID        string `json:"waiting_ask_id,omitempty"`
}

type WorkflowTaskTransition struct {
	ID           string                   `json:"id"`
	TaskID       string                   `json:"task_id"`
	TransitionID string                   `json:"transition_id"`
	State        string                   `json:"state"`
	Commentary   string                   `json:"commentary,omitempty"`
	OutputValues map[string]string        `json:"output_values,omitempty"`
	CreatedAt    int64                    `json:"created_at_unix_ms"`
	Edges        []WorkflowTransitionEdge `json:"edges,omitempty"`
}

type WorkflowTransitionEdge struct {
	ID                   string `json:"id"`
	TaskTransitionID     string `json:"task_transition_id"`
	WorkflowEdgeID       string `json:"workflow_edge_id,omitempty"`
	EdgeKey              string `json:"edge_key"`
	TargetNodeID         string `json:"target_node_id,omitempty"`
	TargetPlacementID    string `json:"target_placement_id,omitempty"`
	State                string `json:"state"`
	WorkflowRevisionSeen int64  `json:"workflow_revision_seen"`
}

type WorkflowTaskComment struct {
	ID        string `json:"id"`
	TaskID    string `json:"task_id"`
	Body      string `json:"body"`
	Author    string `json:"author"`
	AuthorID  string `json:"author_id,omitempty"`
	DeletedAt int64  `json:"deleted_at_unix_ms"`
	UpdatedAt int64  `json:"updated_at_unix_ms"`
}

func (r WorkflowCreateRequest) Validate() error {
	return validateWorkflowName(r.Name)
}

func (r WorkflowUpdateRequest) Validate() error {
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	return validateWorkflowName(r.Name)
}

func (r WorkflowGetRequest) Validate() error {
	return validateRequired("workflow_id", r.WorkflowID)
}

func (r WorkflowNodeAddRequest) Validate() error {
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	if err := validateModelKey("key", r.Key); err != nil {
		return err
	}
	if err := validateRequired("kind", r.Kind); err != nil {
		return err
	}
	if err := validateDisplayName(r.DisplayName); err != nil {
		return err
	}
	for _, field := range r.OutputFields {
		if err := validateModelKey("output_field.name", field.Name); err != nil {
			return err
		}
		if strings.TrimSpace(field.Description) == "" {
			return workflowRequestError(WorkflowRequestErrorRequired, "output_field.description", "output_field.description is required")
		}
	}
	return nil
}

func (r WorkflowTransitionGroupAddRequest) Validate() error {
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	if err := validateRequired("source_node_id", r.SourceNodeID); err != nil {
		return err
	}
	return validateModelKey("transition_id", r.TransitionID)
}

func (r WorkflowEdgeAddRequest) Validate() error {
	for _, field := range []struct{ name, value string }{{"workflow_id", r.WorkflowID}, {"transition_group_id", r.TransitionGroupID}, {"target_node_id", r.TargetNodeID}, {"context_mode", r.ContextMode}} {
		if err := validateRequired(field.name, field.value); err != nil {
			return err
		}
	}
	if err := validateModelKey("key", r.Key); err != nil {
		return err
	}
	for _, binding := range r.InputBindings {
		if err := validateModelKey("input_binding.name", binding.Name); err != nil {
			return err
		}
		if err := validateRequired("input_binding.source", binding.Source); err != nil {
			return err
		}
	}
	for _, requirement := range r.OutputRequirements {
		if err := validateModelKey("output_requirement.field_name", requirement.FieldName); err != nil {
			return err
		}
	}
	return nil
}

func (r WorkflowLinkProjectRequest) Validate() error {
	if err := validateRequired("project_id", r.ProjectID); err != nil {
		return err
	}
	return validateRequired("workflow_id", r.WorkflowID)
}

func (r WorkflowListProjectLinksRequest) Validate() error {
	return validateRequired("project_id", r.ProjectID)
}

func (r WorkflowSetDefaultProjectLinkRequest) Validate() error {
	if err := validateRequired("project_id", r.ProjectID); err != nil {
		return err
	}
	return validateRequired("workflow_id", r.WorkflowID)
}

func (r WorkflowUnlinkProjectRequest) Validate() error {
	return validateRequired("link_id", r.LinkID)
}

func (r WorkflowValidateRequest) Validate() error {
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	switch r.Mode {
	case "", WorkflowValidationModeDraft, WorkflowValidationModeTaskCreation, WorkflowValidationModeExecution:
		return nil
	default:
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "mode", "mode must be draft, task_creation, or execution")
	}
}

func (r WorkflowTaskCreateRequest) Validate() error {
	for _, field := range []struct{ name, value string }{{"project_id", r.ProjectID}, {"title", r.Title}, {"body", r.Body}} {
		if err := validateRequired(field.name, field.value); err != nil {
			return err
		}
	}
	return nil
}

func (r WorkflowTaskStartRequest) Validate() error {
	return validateRequired("task_id", r.TaskID)
}

func (r WorkflowTaskApproveRequest) Validate() error {
	return validateRequired("transition_id", r.TransitionID)
}

func (r WorkflowTaskMoveRequest) Validate() error {
	if err := validateRequired("task_id", r.TaskID); err != nil {
		return err
	}
	return validateRequired("target_node_id", r.TargetNodeID)
}

func (r WorkflowTaskCancelRequest) Validate() error {
	return validateRequired("task_id", r.TaskID)
}

func (r WorkflowTaskCommentAddRequest) Validate() error {
	if err := validateRequired("task_id", r.TaskID); err != nil {
		return err
	}
	if err := validateRequired("body", r.Body); err != nil {
		return err
	}
	return validateRequired("author", r.Author)
}

func (r WorkflowTaskCommentListRequest) Validate() error {
	return validateRequired("task_id", r.TaskID)
}

func (r WorkflowTaskCommentReplaceRequest) Validate() error {
	if err := validateRequired("comment_id", r.CommentID); err != nil {
		return err
	}
	return validateRequired("body", r.Body)
}

func (r WorkflowTaskCommentDeleteRequest) Validate() error {
	return validateRequired("comment_id", r.CommentID)
}

func (r WorkflowBoardRequest) Validate() error {
	return validateRequired("project_id", r.ProjectID)
}

func (r WorkflowTaskGetRequest) Validate() error {
	return validateRequired("task_id", r.TaskID)
}

func validateRequired(name string, value string) error {
	if strings.TrimSpace(value) == "" {
		return workflowRequestError(WorkflowRequestErrorRequired, name, name+" is required")
	}
	return nil
}

func validateWorkflowName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return workflowRequestError(WorkflowRequestErrorRequired, "name", "name is required")
	}
	if len([]rune(trimmed)) > 120 {
		return workflowRequestError(WorkflowRequestErrorTooLong, "name", "name must be <= 120 characters")
	}
	return nil
}

func validateDisplayName(name string) error {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return workflowRequestError(WorkflowRequestErrorRequired, "display_name", "display_name is required")
	}
	if len([]rune(trimmed)) > 120 {
		return workflowRequestError(WorkflowRequestErrorTooLong, "display_name", "display_name must be <= 120 characters")
	}
	return nil
}

func validateModelKey(name string, value string) error {
	if !workflowkey.Valid(value) {
		return workflowRequestError(WorkflowRequestErrorInvalidKey, name, fmt.Sprintf("%s must %s", name, workflowkey.Description))
	}
	return nil
}

func workflowRequestError(code string, field string, message string) error {
	return WorkflowRequestValidationError{Code: code, Field: field, Message: message}
}
