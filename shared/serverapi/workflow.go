package serverapi

import (
	"context"
	"fmt"
	"strings"

	"builder/shared/workflowkey"
)

const (
	WorkflowRequestErrorRequired     = "workflow.request.required"
	WorkflowRequestErrorInvalidKey   = "workflow.request.invalid_key"
	WorkflowRequestErrorInvalidValue = "workflow.request.invalid_value"
	WorkflowRequestErrorInvalidMode  = "workflow.request.invalid_mode"
	WorkflowRequestErrorTooLong      = "workflow.request.too_long"
)

const WorkflowListMaxPageSize = 100

const (
	WorkflowGraphDraftMaxNodeGroups       = 200
	WorkflowGraphDraftMaxNodes            = 500
	WorkflowGraphDraftMaxTransitionGroups = 1000
	WorkflowGraphDraftMaxEdges            = 2000
	WorkflowGraphDraftMaxFieldsPerEntity  = 200
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

type WorkflowProjectLinkDefaultMode string

const (
	WorkflowProjectLinkDefaultNever            WorkflowProjectLinkDefaultMode = "never"
	WorkflowProjectLinkDefaultAlways           WorkflowProjectLinkDefaultMode = "always"
	WorkflowProjectLinkDefaultIfProjectHasNone WorkflowProjectLinkDefaultMode = "if_project_has_none"
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
	GroupID        string                `json:"group_id,omitempty"`
	GroupKey       string                `json:"group_key,omitempty"`
	SubagentRole   string                `json:"subagent_role,omitempty"`
	PromptTemplate string                `json:"prompt_template,omitempty"`
	OutputFields   []WorkflowOutputField `json:"output_fields,omitempty"`
}

type WorkflowNodeGroup struct {
	GroupID     string `json:"group_id"`
	WorkflowID  string `json:"workflow_id"`
	GroupKey    string `json:"group_key"`
	DisplayName string `json:"display_name"`
	SortOrder   int    `json:"sort_order"`
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
	ContextSource      WorkflowContextSource       `json:"context_source"`
	InputBindings      []WorkflowInputBinding      `json:"input_bindings,omitempty"`
	OutputRequirements []WorkflowOutputRequirement `json:"output_requirements,omitempty"`
}

type WorkflowContextSource struct {
	Kind    string `json:"kind"`
	NodeKey string `json:"node_key,omitempty"`
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
	NodeGroups       []WorkflowNodeGroup       `json:"node_groups,omitempty"`
	Workflow         WorkflowRecord            `json:"workflow"`
	Nodes            []WorkflowNode            `json:"nodes"`
	TransitionGroups []WorkflowTransitionGroup `json:"transition_groups"`
	Edges            []WorkflowEdge            `json:"edges"`
}

type WorkflowGraphDraft struct {
	NodeGroups       []WorkflowGraphDraftNodeGroup       `json:"node_groups,omitempty"`
	Nodes            []WorkflowGraphDraftNode            `json:"nodes"`
	TransitionGroups []WorkflowGraphDraftTransitionGroup `json:"transition_groups"`
	Edges            []WorkflowGraphDraftEdge            `json:"edges"`
}

type WorkflowGraphDraftNodeGroup struct {
	ID          string `json:"id"`
	Key         string `json:"key"`
	DisplayName string `json:"display_name"`
}

type WorkflowGraphDraftNode struct {
	ID             string                `json:"id"`
	Key            string                `json:"key"`
	Kind           string                `json:"kind"`
	DisplayName    string                `json:"display_name"`
	GroupID        string                `json:"group_id,omitempty"`
	GroupKey       string                `json:"group_key,omitempty"`
	SubagentRole   string                `json:"subagent_role,omitempty"`
	PromptTemplate string                `json:"prompt_template,omitempty"`
	OutputFields   []WorkflowOutputField `json:"output_fields,omitempty"`
}

type WorkflowGraphDraftTransitionGroup struct {
	ID           string `json:"id"`
	SourceNodeID string `json:"source_node_id"`
	TransitionID string `json:"transition_id"`
	DisplayName  string `json:"display_name"`
}

type WorkflowGraphDraftEdge struct {
	ID                 string                      `json:"id"`
	TransitionGroupID  string                      `json:"transition_group_id"`
	Key                string                      `json:"key"`
	TargetNodeID       string                      `json:"target_node_id"`
	RequiresApproval   bool                        `json:"requires_approval"`
	ContextMode        string                      `json:"context_mode"`
	ContextSource      WorkflowContextSource       `json:"context_source"`
	InputBindings      []WorkflowInputBinding      `json:"input_bindings,omitempty"`
	OutputRequirements []WorkflowOutputRequirement `json:"output_requirements,omitempty"`
}

type WorkflowGraphValidateDraftRequest struct {
	WorkflowID string                   `json:"workflow_id"`
	Graph      WorkflowGraphDraft       `json:"graph"`
	Modes      []WorkflowValidationMode `json:"modes"`
}

type WorkflowGraphValidateDraftResponse struct {
	Results map[WorkflowValidationMode]WorkflowValidateResponse `json:"results"`
}

type WorkflowGraphSavePreviewRequest struct {
	WorkflowID            string             `json:"workflow_id"`
	ExpectedGraphRevision int64              `json:"expected_graph_revision"`
	Graph                 WorkflowGraphDraft `json:"graph"`
}

type WorkflowGraphSaveConfirmation struct {
	ExpectedRemovedNodeCount            int64 `json:"expected_removed_node_count"`
	ExpectedRemovedTransitionGroupCount int64 `json:"expected_removed_transition_group_count"`
	ExpectedRemovedEdgeCount            int64 `json:"expected_removed_edge_count"`
	ExpectedNodeTaskReferenceCount      int64 `json:"expected_node_task_reference_count"`
	ExpectedEdgeTaskReferenceCount      int64 `json:"expected_edge_task_reference_count"`
}

type WorkflowGraphSaveRequest struct {
	WorkflowID            string                         `json:"workflow_id"`
	ExpectedGraphRevision int64                          `json:"expected_graph_revision"`
	Graph                 WorkflowGraphDraft             `json:"graph"`
	Confirmation          *WorkflowGraphSaveConfirmation `json:"confirmation,omitempty"`
}

type WorkflowGraphSavePreviewResponse struct {
	CurrentGraphRevision int64                                               `json:"current_graph_revision"`
	ValidationResults    map[WorkflowValidationMode]WorkflowValidateResponse `json:"validation_results"`
	Impact               WorkflowGraphSaveImpact                             `json:"impact"`
	Blockers             []WorkflowGraphSaveBlocker                          `json:"blockers,omitempty"`
	CanSave              bool                                                `json:"can_save"`
	ConfirmationRequired bool                                                `json:"confirmation_required"`
}

type WorkflowGraphSaveResponse struct {
	Saved                bool                                                `json:"saved"`
	Definition           *WorkflowDefinition                                 `json:"definition,omitempty"`
	CurrentGraphRevision int64                                               `json:"current_graph_revision"`
	ValidationResults    map[WorkflowValidationMode]WorkflowValidateResponse `json:"validation_results"`
	Impact               WorkflowGraphSaveImpact                             `json:"impact"`
	Blockers             []WorkflowGraphSaveBlocker                          `json:"blockers,omitempty"`
	CanSave              bool                                                `json:"can_save"`
	ConfirmationRequired bool                                                `json:"confirmation_required"`
}

type WorkflowGraphSaveImpact struct {
	RemovedNodeCount                  int64 `json:"removed_node_count"`
	RemovedTransitionGroupCount       int64 `json:"removed_transition_group_count"`
	RemovedEdgeCount                  int64 `json:"removed_edge_count"`
	NodeTaskReferenceCount            int64 `json:"node_task_reference_count"`
	EdgeTaskReferenceCount            int64 `json:"edge_task_reference_count"`
	ActiveNodePlacementCount          int64 `json:"active_node_placement_count"`
	PendingApprovalCount              int64 `json:"pending_approval_count"`
	ActiveRunCount                    int64 `json:"active_run_count"`
	RunnableRunCount                  int64 `json:"runnable_run_count"`
	StartNodeChangeCount              int64 `json:"start_node_change_count"`
	LastTerminalChangeCount           int64 `json:"last_terminal_change_count"`
	TaskReferencedNodeKindChangeCount int64 `json:"task_referenced_node_kind_change_count"`
}

type WorkflowGraphSaveBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int64  `json:"count"`
}

type WorkflowCreateRequest struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type WorkflowCreateResponse struct {
	Workflow WorkflowRecord `json:"workflow"`
}

type WorkflowCreateAndLinkProjectRequest struct {
	Name          string                         `json:"name"`
	Description   string                         `json:"description,omitempty"`
	ProjectID     string                         `json:"project_id"`
	DefaultPolicy WorkflowProjectLinkDefaultMode `json:"default_policy,omitempty"`
}

type WorkflowCreateAndLinkProjectResponse struct {
	Workflow WorkflowRecord      `json:"workflow"`
	Link     ProjectWorkflowLink `json:"link"`
}

type WorkflowUpdateRequest struct {
	WorkflowID  string `json:"workflow_id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

type WorkflowListRequest struct {
	PageSize  int    `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
	Query     string `json:"query,omitempty"`
}

type WorkflowListResponse struct {
	Workflows     []WorkflowRecord `json:"workflows"`
	NextPageToken string           `json:"next_page_token,omitempty"`
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
	GroupKey       string                `json:"group_key,omitempty"`
	SubagentRole   string                `json:"subagent_role,omitempty"`
	PromptTemplate string                `json:"prompt_template,omitempty"`
	OutputFields   []WorkflowOutputField `json:"output_fields,omitempty"`
}

type WorkflowNodeAddResponse struct {
	GraphRevision int64 `json:"graph_revision"`
}

type WorkflowNodeUpdateRequest struct {
	WorkflowID     string                `json:"workflow_id"`
	NodeID         string                `json:"node_id"`
	Key            string                `json:"key"`
	Kind           string                `json:"kind"`
	DisplayName    string                `json:"display_name"`
	GroupKey       string                `json:"group_key,omitempty"`
	SubagentRole   string                `json:"subagent_role,omitempty"`
	PromptTemplate string                `json:"prompt_template,omitempty"`
	OutputFields   []WorkflowOutputField `json:"output_fields,omitempty"`
}

type WorkflowNodeUpdateResponse struct {
	GraphRevision int64 `json:"graph_revision"`
}

type WorkflowNodeGroupAddRequest struct {
	WorkflowID  string `json:"workflow_id"`
	GroupID     string `json:"group_id,omitempty"`
	GroupKey    string `json:"group_key"`
	DisplayName string `json:"display_name"`
	SortOrder   int    `json:"sort_order"`
}

type WorkflowNodeGroupUpdateRequest struct {
	WorkflowID  string `json:"workflow_id"`
	GroupID     string `json:"group_id"`
	GroupKey    string `json:"group_key"`
	DisplayName string `json:"display_name"`
	SortOrder   int    `json:"sort_order"`
}

type WorkflowNodeGroupDeleteRequest struct {
	WorkflowID string `json:"workflow_id"`
	GroupID    string `json:"group_id"`
}

type WorkflowNodeGroupResponse struct {
	Group         WorkflowNodeGroup `json:"group"`
	GraphRevision int64             `json:"graph_revision"`
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

type WorkflowTransitionGroupUpdateRequest struct {
	WorkflowID   string `json:"workflow_id"`
	GroupID      string `json:"group_id"`
	SourceNodeID string `json:"source_node_id"`
	TransitionID string `json:"transition_id"`
	DisplayName  string `json:"display_name,omitempty"`
}

type WorkflowTransitionGroupUpdateResponse struct {
	GraphRevision int64 `json:"graph_revision"`
}

type WorkflowEdgeAddRequest struct {
	WorkflowID         string                      `json:"workflow_id"`
	EdgeID             string                      `json:"edge_id,omitempty"`
	TransitionGroupID  string                      `json:"transition_group_id"`
	Key                string                      `json:"key"`
	TargetNodeID       string                      `json:"target_node_id"`
	ContextMode        string                      `json:"context_mode"`
	ContextSource      WorkflowContextSource       `json:"context_source"`
	RequiresApproval   bool                        `json:"requires_approval"`
	InputBindings      []WorkflowInputBinding      `json:"input_bindings,omitempty"`
	OutputRequirements []WorkflowOutputRequirement `json:"output_requirements,omitempty"`
}

type WorkflowEdgeAddResponse struct {
	GraphRevision int64 `json:"graph_revision"`
}

type WorkflowEdgeUpdateRequest struct {
	WorkflowID         string                      `json:"workflow_id"`
	EdgeID             string                      `json:"edge_id"`
	TransitionGroupID  string                      `json:"transition_group_id"`
	Key                string                      `json:"key"`
	TargetNodeID       string                      `json:"target_node_id"`
	ContextMode        string                      `json:"context_mode"`
	ContextSource      WorkflowContextSource       `json:"context_source"`
	RequiresApproval   bool                        `json:"requires_approval"`
	InputBindings      []WorkflowInputBinding      `json:"input_bindings,omitempty"`
	OutputRequirements []WorkflowOutputRequirement `json:"output_requirements,omitempty"`
}

type WorkflowEdgeUpdateResponse struct {
	GraphRevision int64 `json:"graph_revision"`
}

type WorkflowLinkProjectRequest struct {
	ProjectID     string                         `json:"project_id"`
	WorkflowID    string                         `json:"workflow_id"`
	Default       bool                           `json:"default"`
	DefaultPolicy WorkflowProjectLinkDefaultMode `json:"default_policy,omitempty"`
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
	ID         string `json:"id"`
	ProjectID  string `json:"project_id"`
	WorkflowID string `json:"workflow_id"`
	Default    bool   `json:"default"`
}

type WorkflowUnlinkProjectRequest struct {
	LinkID                   string `json:"link_id"`
	ReplacementDefaultLinkID string `json:"replacement_default_link_id,omitempty"`
}

type WorkflowUnlinkProjectResponse struct {
	LinkID   string                         `json:"link_id"`
	Unlinked bool                           `json:"unlinked"`
	Blockers []WorkflowUnlinkProjectBlocker `json:"blockers,omitempty"`
}

type WorkflowUnlinkProjectBlocker struct {
	Code    string                        `json:"code"`
	Message string                        `json:"message"`
	Count   int                           `json:"count,omitempty"`
	Tasks   []WorkflowUnlinkTaskReference `json:"tasks,omitempty"`
}

type WorkflowUnlinkTaskReference struct {
	TaskID  string `json:"task_id"`
	ShortID string `json:"short_id"`
	Title   string `json:"title,omitempty"`
}

type WorkflowDeletePreviewRequest struct {
	WorkflowID string `json:"workflow_id"`
}

type WorkflowDeletePreviewResponse struct {
	Impact WorkflowDeleteImpact `json:"impact"`
}

type WorkflowDeleteRequest struct {
	WorkflowID            string `json:"workflow_id"`
	Confirmed             bool   `json:"confirmed"`
	ExpectedGraphRevision int64  `json:"expected_graph_revision"`
	ExpectedProjectCount  int64  `json:"expected_project_count"`
	ExpectedLinkCount     int64  `json:"expected_link_count"`
	ExpectedTaskCount     int64  `json:"expected_task_count"`
	CleanupArtifacts      bool   `json:"cleanup_artifacts,omitempty"`
}

type WorkflowDeleteResponse struct {
	Deleted  bool                    `json:"deleted"`
	Impact   WorkflowDeleteImpact    `json:"impact"`
	Blockers []WorkflowDeleteBlocker `json:"blockers,omitempty"`
}

type WorkflowDeleteImpact struct {
	WorkflowID                     string `json:"workflow_id"`
	GraphRevision                  int64  `json:"graph_revision"`
	ProjectCount                   int64  `json:"project_count"`
	LinkCount                      int64  `json:"link_count"`
	DefaultReplacementProjectCount int64  `json:"default_replacement_project_count"`
	TaskCount                      int64  `json:"task_count"`
	ActiveRunCount                 int64  `json:"active_run_count"`
	RunnableRunCount               int64  `json:"runnable_run_count"`
	BlockedTaskCount               int64  `json:"blocked_task_count"`
}

type WorkflowDeleteBlocker struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Count   int64  `json:"count"`
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
	ProjectID         string `json:"project_id"`
	WorkflowID        string `json:"workflow_id,omitempty"`
	Title             string `json:"title"`
	Body              string `json:"body,omitempty"`
	SourceURL         string `json:"source_url,omitempty"`
	SourceWorkspaceID string `json:"source_workspace_id,omitempty"`
}

type WorkflowTaskCreateResponse struct {
	Task WorkflowTaskSummary `json:"task"`
}

type WorkflowTaskUpdateRequest struct {
	TaskID            string  `json:"task_id"`
	Title             string  `json:"title"`
	Body              *string `json:"body,omitempty"`
	SourceWorkspaceID string  `json:"source_workspace_id,omitempty"`
}

type WorkflowTaskUpdateResponse struct {
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

type WorkflowTaskResumeRequest struct {
	TaskID string `json:"task_id"`
	RunID  string `json:"run_id,omitempty"`
}

type WorkflowTaskResumeResponse struct {
	RunID       string `json:"run_id"`
	PlacementID string `json:"placement_id"`
	NodeID      string `json:"node_id"`
	Generation  int64  `json:"generation"`
	SessionID   string `json:"session_id,omitempty"`
}

type WorkflowTaskApproveRequest struct {
	TaskTransitionID string `json:"task_transition_id,omitempty"`
	TransitionID     string `json:"transition_id,omitempty"`
}

type WorkflowTaskApproveResponse struct {
	TransitionID string   `json:"transition_id"`
	State        string   `json:"state"`
	PlacementIDs []string `json:"placement_ids,omitempty"`
	RunIDs       []string `json:"run_ids,omitempty"`
}

type WorkflowTaskMoveRequest struct {
	TaskID           string            `json:"task_id"`
	TargetNodeID     string            `json:"target_node_id"`
	OutputValues     map[string]string `json:"output_values,omitempty"`
	Commentary       string            `json:"commentary,omitempty"`
	AllowMissingEdge bool              `json:"allow_missing_edge,omitempty"`
	AutoApprove      bool              `json:"auto_approve,omitempty"`
}

type WorkflowTaskMoveResponse struct {
	TransitionID  string   `json:"transition_id"`
	State         string   `json:"state"`
	PlacementIDs  []string `json:"placement_ids,omitempty"`
	RunIDs        []string `json:"run_ids,omitempty"`
	ApprovalError string   `json:"approval_error,omitempty"`
}

type WorkflowTaskCancelRequest struct {
	TaskID string `json:"task_id"`
	Reason string `json:"reason,omitempty"`
}

type WorkflowTaskInterruptRequest struct {
	TaskID string `json:"task_id"`
	RunID  string `json:"run_id,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type WorkflowTaskInterruptResponse struct {
	RunID string `json:"run_id"`
}

type WorkflowAttentionListRequest struct {
	ProjectID string `json:"project_id,omitempty"`
	PageSize  int    `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

type WorkflowAttentionListResponse struct {
	Items             []WorkflowAttentionItem `json:"items"`
	NextPageToken     string                  `json:"next_page_token,omitempty"`
	GeneratedAtUnixMs int64                   `json:"generated_at_unix_ms"`
}

type WorkflowTaskAttentionListRequest struct {
	TaskID string `json:"task_id"`
}

type WorkflowTaskAttentionListResponse struct {
	Items             []WorkflowAttentionItem `json:"items"`
	GeneratedAtUnixMs int64                   `json:"generated_at_unix_ms"`
}

type WorkflowAttentionItem struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	ProjectID        string `json:"project_id,omitempty"`
	WorkflowID       string `json:"workflow_id,omitempty"`
	TaskID           string `json:"task_id,omitempty"`
	TaskShortID      string `json:"task_short_id,omitempty"`
	TaskTitle        string `json:"task_title,omitempty"`
	RunID            string `json:"run_id,omitempty"`
	SessionID        string `json:"session_id,omitempty"`
	AskID            string `json:"ask_id,omitempty"`
	TaskTransitionID string `json:"task_transition_id,omitempty"`
	Message          string `json:"message"`
	OccurredAtUnixMs int64  `json:"occurred_at_unix_ms"`
}

type WorkflowTaskQuestionAnswerRequest struct {
	ClientRequestID      string `json:"client_request_id"`
	TaskID               string `json:"task_id"`
	RunID                string `json:"run_id,omitempty"`
	AskID                string `json:"ask_id"`
	ErrorMessage         string `json:"error_message,omitempty"`
	Answer               string `json:"answer,omitempty"`
	SelectedOptionNumber int    `json:"selected_option_number,omitempty"`
	FreeformAnswer       string `json:"freeform_answer,omitempty"`
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
	TaskID string `json:"task_id"`
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
	ProjectID        string `json:"project_id"`
	WorkflowID       string `json:"workflow_id,omitempty"`
	DonePreviewLimit int    `json:"done_preview_limit"`
	PageSize         int    `json:"page_size"`
	PageToken        string `json:"page_token"`
}

type WorkflowBoardResponse struct {
	Board WorkflowBoard `json:"board"`
}

type WorkflowBoardNodeCardsListRequest struct {
	ProjectID  string `json:"project_id"`
	WorkflowID string `json:"workflow_id"`
	NodeID     string `json:"node_id"`
	PageSize   int    `json:"page_size"`
	PageToken  string `json:"page_token"`
}

type WorkflowBoardNodeCardsListResponse struct {
	ProjectID         string                  `json:"project_id"`
	WorkflowID        string                  `json:"workflow_id"`
	NodeID            string                  `json:"node_id"`
	Cards             []WorkflowBoardTaskCard `json:"cards"`
	NextPageToken     string                  `json:"next_page_token"`
	GeneratedAtUnixMs int64                   `json:"generated_at_unix_ms"`
}

type WorkflowBoard struct {
	ProjectID          string                  `json:"project_id"`
	Project            ProjectBoardProject     `json:"project"`
	SelectedWorkflow   WorkflowPickerItem      `json:"selected_workflow"`
	WorkflowPicker     []WorkflowPickerItem    `json:"workflows"`
	Groups             []WorkflowBoardGroup    `json:"groups"`
	Columns            []WorkflowBoardColumn   `json:"columns"`
	Cards              []WorkflowBoardTaskCard `json:"cards"`
	DonePreview        []WorkflowBoardTaskCard `json:"done_preview"`
	HasHiddenDoneCards bool                    `json:"has_hidden_done_cards"`
	NextPageToken      string                  `json:"next_page_token"`
	GeneratedAtUnixMs  int64                   `json:"generated_at_unix_ms"`
	Workflows          []WorkflowBoardWorkflow `json:"legacy_workflows,omitempty"`
}

type ProjectBoardProject struct {
	ProjectID   string `json:"project_id"`
	ProjectKey  string `json:"project_key"`
	DisplayName string `json:"display_name"`
}

type WorkflowPickerItem struct {
	WorkflowID           string                    `json:"workflow_id"`
	DisplayName          string                    `json:"display_name"`
	Description          string                    `json:"description"`
	GraphRevision        int64                     `json:"graph_revision"`
	IsProjectDefault     bool                      `json:"is_project_default"`
	ValidForTaskCreation bool                      `json:"valid_for_task_creation"`
	ValidationErrors     []WorkflowValidationError `json:"validation_errors,omitempty"`
}

type WorkflowBoardGroup struct {
	GroupID     string   `json:"group_id"`
	Key         string   `json:"key"`
	DisplayName string   `json:"display_name"`
	SortOrder   int      `json:"sort_order"`
	NodeIDs     []string `json:"node_ids"`
}

type WorkflowBoardColumn struct {
	Node      WorkflowBoardNodeSummary `json:"node"`
	GroupID   string                   `json:"group_id,omitempty"`
	SortOrder int                      `json:"sort_order"`
	IsBacklog bool                     `json:"is_backlog"`
	IsDone    bool                     `json:"is_done"`
	TaskCount int                      `json:"task_count"`
}

type WorkflowBoardNodeSummary struct {
	NodeID                 string                `json:"node_id"`
	Key                    string                `json:"key"`
	Kind                   string                `json:"kind"`
	DisplayName            string                `json:"display_name"`
	AssigneeRole           string                `json:"assignee_role,omitempty"`
	SortOrder              int                   `json:"sort_order"`
	OutputFields           []WorkflowOutputField `json:"output_fields,omitempty"`
	TransitionOutputFields []WorkflowOutputField `json:"transition_output_fields,omitempty"`
}

type WorkflowBoardTaskCard struct {
	TaskID          string                  `json:"task_id"`
	ShortID         string                  `json:"short_id"`
	Title           string                  `json:"title"`
	BodyPreview     string                  `json:"body_preview,omitempty"`
	WorkflowID      string                  `json:"workflow_id"`
	ActiveNodeIDs   []string                `json:"active_node_ids,omitempty"`
	SourceWorkspace ProjectWorkspaceSummary `json:"source_workspace"`
	Status          WorkflowTaskStatus      `json:"status"`
	Actions         WorkflowTaskActions     `json:"actions"`
	UpdatedAtUnixMs int64                   `json:"updated_at_unix_ms"`
}

type WorkflowTaskStatus struct {
	Kind           string   `json:"kind"`
	Label          string   `json:"label"`
	NativeState    string   `json:"native_state"`
	NodeIDs        []string `json:"node_ids,omitempty"`
	RunIDs         []string `json:"run_ids,omitempty"`
	AttentionTypes []string `json:"attention_types,omitempty"`
}

type WorkflowTaskActions struct {
	CanStart                bool     `json:"can_start"`
	CanInterrupt            bool     `json:"can_interrupt"`
	InterruptRunID          string   `json:"interrupt_run_id,omitempty"`
	CanResume               bool     `json:"can_resume"`
	ResumeRunID             string   `json:"resume_run_id,omitempty"`
	CanCancel               bool     `json:"can_cancel"`
	NeedsDetailForInterrupt bool     `json:"needs_detail_for_interrupt"`
	NeedsDetailForResume    bool     `json:"needs_detail_for_resume"`
	ManualMoveTargetNodeIDs []string `json:"manual_move_target_node_ids,omitempty"`
}

type WorkflowProjectSubscribeRequest struct {
	ProjectID string `json:"project_id,omitempty"`
}

type WorkflowProjectEvent struct {
	ProjectID        string   `json:"project_id,omitempty"`
	WorkflowID       string   `json:"workflow_id,omitempty"`
	Resource         string   `json:"resource"`
	Action           string   `json:"action"`
	ChangedIDs       []string `json:"changed_ids,omitempty"`
	OccurredAtUnixMs int64    `json:"occurred_at_unix_ms"`
}

type WorkflowProjectSubscription interface {
	Next(ctx context.Context) (WorkflowProjectEvent, error)
	Close() error
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

type WorkflowTaskActivityListRequest struct {
	TaskID    string `json:"task_id"`
	PageSize  int    `json:"page_size,omitempty"`
	PageToken string `json:"page_token,omitempty"`
}

type WorkflowTaskActivityListResponse struct {
	Items             []WorkflowTaskActivityItem `json:"items"`
	NextPageToken     string                     `json:"next_page_token,omitempty"`
	GeneratedAtUnixMs int64                      `json:"generated_at_unix_ms"`
}

type WorkflowTaskTeleportTargetRequest struct {
	TaskID string `json:"task_id"`
	RunID  string `json:"run_id,omitempty"`
}

type WorkflowTaskTeleportTargetResponse struct {
	Available     bool   `json:"available"`
	TaskID        string `json:"task_id,omitempty"`
	RunID         string `json:"run_id,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	ProjectID     string `json:"project_id,omitempty"`
	WorkspaceID   string `json:"workspace_id,omitempty"`
	WorktreeID    string `json:"worktree_id,omitempty"`
	CwdRelpath    string `json:"cwd_relpath,omitempty"`
	FailureReason string `json:"failure_reason,omitempty"`
}

type WorkflowTaskSummary struct {
	ID                string   `json:"id"`
	ProjectID         string   `json:"project_id"`
	WorkflowID        string   `json:"workflow_id"`
	ShortID           string   `json:"short_id"`
	Title             string   `json:"title"`
	BodyPreview       string   `json:"body_preview,omitempty"`
	SourceWorkspaceID string   `json:"source_workspace_id,omitempty"`
	CanceledAt        int64    `json:"canceled_at_unix_ms"`
	CancelReason      string   `json:"cancel_reason,omitempty"`
	CreatedAtUnixMs   int64    `json:"created_at_unix_ms"`
	UpdatedAtUnixMs   int64    `json:"updated_at_unix_ms"`
	Done              bool     `json:"done"`
	ActiveNodeIDs     []string `json:"active_node_ids,omitempty"`
}

type WorkflowTaskDetail struct {
	Summary         WorkflowTaskSummary      `json:"summary"`
	Project         ProjectBoardProject      `json:"project"`
	Workflow        WorkflowPickerItem       `json:"workflow"`
	Body            string                   `json:"body"`
	SourceURL       string                   `json:"source_url,omitempty"`
	SourceWorkspace ProjectWorkspaceSummary  `json:"source_workspace"`
	ManagedWorktree *WorktreeView            `json:"managed_worktree,omitempty"`
	Status          WorkflowTaskStatus       `json:"status"`
	Actions         WorkflowTaskActions      `json:"actions"`
	Attention       []WorkflowAttentionItem  `json:"attention,omitempty"`
	Placements      []WorkflowPlacement      `json:"placements"`
	Runs            []WorkflowRun            `json:"runs"`
	Transitions     []WorkflowTaskTransition `json:"transitions"`
	Comments        []WorkflowTaskComment    `json:"comments"`
}

type WorkflowPlacement struct {
	ID                        string `json:"id"`
	TaskID                    string `json:"task_id"`
	NodeID                    string `json:"node_id"`
	NodeKey                   string `json:"node_key,omitempty"`
	NodeDisplayName           string `json:"node_display_name,omitempty"`
	NodeKind                  string `json:"node_kind,omitempty"`
	State                     string `json:"state"`
	ParallelBatchTransitionID string `json:"parallel_batch_transition_id,omitempty"`
	ParallelBranchEdgeID      string `json:"parallel_branch_edge_id,omitempty"`
}

type WorkflowRun struct {
	ID                  string `json:"id"`
	TaskID              string `json:"task_id"`
	PlacementID         string `json:"placement_id"`
	NodeID              string `json:"node_id"`
	SessionID           string `json:"session_id,omitempty"`
	SessionName         string `json:"session_name,omitempty"`
	Role                string `json:"role,omitempty"`
	Status              string `json:"status"`
	Generation          int64  `json:"generation"`
	StartedAtUnixMs     int64  `json:"started_at_unix_ms"`
	CompletedAtUnixMs   int64  `json:"completed_at_unix_ms"`
	InterruptedAtUnixMs int64  `json:"interrupted_at_unix_ms"`
	InterruptionReason  string `json:"interruption_reason,omitempty"`
	WaitingAskID        string `json:"waiting_ask_id,omitempty"`
}

type WorkflowTaskTransition struct {
	ID                    string                   `json:"id"`
	TaskID                string                   `json:"task_id"`
	SourceRunID           string                   `json:"source_run_id,omitempty"`
	SourcePlacementID     string                   `json:"source_placement_id,omitempty"`
	SourceNodeID          string                   `json:"source_node_id,omitempty"`
	SourceNodeKey         string                   `json:"source_node_key,omitempty"`
	SourceNodeDisplayName string                   `json:"source_node_display_name,omitempty"`
	TransitionGroupID     string                   `json:"transition_group_id,omitempty"`
	TransitionID          string                   `json:"transition_id"`
	TransitionDisplayName string                   `json:"transition_display_name,omitempty"`
	WorkflowRevisionSeen  int64                    `json:"workflow_revision_seen"`
	Actor                 string                   `json:"actor,omitempty"`
	State                 string                   `json:"state"`
	Commentary            string                   `json:"commentary,omitempty"`
	OutputValues          map[string]string        `json:"output_values,omitempty"`
	CreatedAt             int64                    `json:"created_at_unix_ms"`
	AppliedAtUnixMs       int64                    `json:"applied_at_unix_ms"`
	Edges                 []WorkflowTransitionEdge `json:"edges,omitempty"`
}

type WorkflowTransitionEdge struct {
	ID                    string                      `json:"id"`
	TaskTransitionID      string                      `json:"task_transition_id"`
	WorkflowEdgeID        string                      `json:"workflow_edge_id,omitempty"`
	EdgeKey               string                      `json:"edge_key"`
	TargetNodeID          string                      `json:"target_node_id,omitempty"`
	TargetNodeKey         string                      `json:"target_node_key,omitempty"`
	TargetNodeDisplayName string                      `json:"target_node_display_name,omitempty"`
	TargetNodeKind        string                      `json:"target_node_kind,omitempty"`
	TargetPlacementID     string                      `json:"target_placement_id,omitempty"`
	State                 string                      `json:"state"`
	ContextMode           string                      `json:"context_mode,omitempty"`
	RequiresApproval      bool                        `json:"requires_approval"`
	InputBindings         []WorkflowInputBinding      `json:"input_bindings,omitempty"`
	OutputRequirements    []WorkflowOutputRequirement `json:"output_requirements,omitempty"`
	WorkflowRevisionSeen  int64                       `json:"workflow_revision_seen"`
}

type WorkflowTaskComment struct {
	ID              string `json:"id"`
	TaskID          string `json:"task_id"`
	Body            string `json:"body"`
	Author          string `json:"author"`
	AuthorID        string `json:"author_id,omitempty"`
	CreatedAtUnixMs int64  `json:"created_at_unix_ms"`
	UpdatedAt       int64  `json:"updated_at_unix_ms"`
}

type WorkflowTaskActivityItem struct {
	ActivityID       string                  `json:"activity_id"`
	Type             string                  `json:"type"`
	TaskID           string                  `json:"task_id"`
	OccurredAtUnixMs int64                   `json:"occurred_at_unix_ms"`
	UpdatedAtUnixMs  int64                   `json:"updated_at_unix_ms"`
	Actor            string                  `json:"actor,omitempty"`
	Summary          string                  `json:"summary"`
	Comment          *WorkflowTaskComment    `json:"comment,omitempty"`
	Transition       *WorkflowTaskTransition `json:"transition,omitempty"`
	Run              *WorkflowRun            `json:"run,omitempty"`
	Attention        *WorkflowAttentionItem  `json:"attention,omitempty"`
}

func (r WorkflowCreateRequest) Validate() error {
	return validateWorkflowName(r.Name)
}

func (r WorkflowCreateAndLinkProjectRequest) Validate() error {
	if err := validateWorkflowName(r.Name); err != nil {
		return err
	}
	if err := validateRequired("project_id", r.ProjectID); err != nil {
		return err
	}
	return validateWorkflowProjectLinkDefaultMode(r.DefaultPolicy)
}

func (r WorkflowUpdateRequest) Validate() error {
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	return validateWorkflowName(r.Name)
}

func (r WorkflowListRequest) Validate() error {
	if r.PageSize < 0 {
		return WorkflowRequestValidationError{Code: WorkflowRequestErrorInvalidMode, Field: "page_size", Message: "page_size must be non-negative"}
	}
	if r.PageSize > WorkflowListMaxPageSize {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "page_size", fmt.Sprintf("page_size must be <= %d", WorkflowListMaxPageSize))
	}
	if r.PageToken != strings.TrimSpace(r.PageToken) {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "page_token", "page_token must not have leading or trailing whitespace")
	}
	return nil
}

func (r WorkflowGetRequest) Validate() error {
	return validateRequired("workflow_id", r.WorkflowID)
}

func (r WorkflowNodeAddRequest) Validate() error {
	return validateWorkflowNodeFields(r.WorkflowID, "", r.Key, r.Kind, r.DisplayName, r.GroupKey, r.OutputFields)
}

func (r WorkflowNodeUpdateRequest) Validate() error {
	if err := validateRequired("node_id", r.NodeID); err != nil {
		return err
	}
	return validateWorkflowNodeFields(r.WorkflowID, r.NodeID, r.Key, r.Kind, r.DisplayName, r.GroupKey, r.OutputFields)
}

func validateWorkflowNodeFields(workflowID string, nodeID string, key string, kind string, displayName string, groupKey string, outputFields []WorkflowOutputField) error {
	if err := validateRequired("workflow_id", workflowID); err != nil {
		return err
	}
	if err := validateModelKey("key", key); err != nil {
		return err
	}
	if err := validateRequired("kind", kind); err != nil {
		return err
	}
	if err := validateDisplayName(displayName); err != nil {
		return err
	}
	if strings.TrimSpace(groupKey) != "" {
		if err := validateModelKey("group_key", groupKey); err != nil {
			return err
		}
	}
	for _, field := range outputFields {
		if err := validateModelKey("output_field.name", field.Name); err != nil {
			return err
		}
		if strings.TrimSpace(field.Description) == "" {
			return workflowRequestError(WorkflowRequestErrorRequired, "output_field.description", "output_field.description is required")
		}
	}
	return nil
}

func (r WorkflowNodeGroupAddRequest) Validate() error {
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	if err := validateModelKey("group_key", r.GroupKey); err != nil {
		return err
	}
	if err := validateDisplayName(r.DisplayName); err != nil {
		return err
	}
	if r.SortOrder < 0 {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "sort_order", "sort_order must be non-negative")
	}
	return nil
}

func (r WorkflowNodeGroupUpdateRequest) Validate() error {
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	if err := validateRequired("group_id", r.GroupID); err != nil {
		return err
	}
	if err := validateModelKey("group_key", r.GroupKey); err != nil {
		return err
	}
	if err := validateDisplayName(r.DisplayName); err != nil {
		return err
	}
	if r.SortOrder < 0 {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "sort_order", "sort_order must be non-negative")
	}
	return nil
}

func (r WorkflowNodeGroupDeleteRequest) Validate() error {
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	return validateRequired("group_id", r.GroupID)
}

func (r WorkflowTransitionGroupAddRequest) Validate() error {
	return validateWorkflowTransitionGroupFields(r.WorkflowID, "", r.SourceNodeID, r.TransitionID, r.DisplayName)
}

func (r WorkflowTransitionGroupUpdateRequest) Validate() error {
	if err := validateRequired("group_id", r.GroupID); err != nil {
		return err
	}
	return validateWorkflowTransitionGroupFields(r.WorkflowID, r.GroupID, r.SourceNodeID, r.TransitionID, r.DisplayName)
}

func validateWorkflowTransitionGroupFields(workflowID string, groupID string, sourceNodeID string, transitionID string, displayName string) error {
	_ = groupID
	if err := validateRequired("workflow_id", workflowID); err != nil {
		return err
	}
	if err := validateRequired("source_node_id", sourceNodeID); err != nil {
		return err
	}
	if err := validateModelKey("transition_id", transitionID); err != nil {
		return err
	}
	if strings.TrimSpace(displayName) != "" {
		return validateDisplayName(displayName)
	}
	return nil
}

func (r WorkflowEdgeAddRequest) Validate() error {
	return validateWorkflowEdgeFields(r.WorkflowID, "", r.TransitionGroupID, r.Key, r.TargetNodeID, r.ContextMode, r.ContextSource, r.InputBindings, r.OutputRequirements)
}

func (r WorkflowEdgeUpdateRequest) Validate() error {
	if err := validateRequired("edge_id", r.EdgeID); err != nil {
		return err
	}
	return validateWorkflowEdgeFields(r.WorkflowID, r.EdgeID, r.TransitionGroupID, r.Key, r.TargetNodeID, r.ContextMode, r.ContextSource, r.InputBindings, r.OutputRequirements)
}

func validateWorkflowEdgeFields(workflowID string, edgeID string, transitionGroupID string, key string, targetNodeID string, contextMode string, contextSource WorkflowContextSource, inputBindings []WorkflowInputBinding, outputRequirements []WorkflowOutputRequirement) error {
	_ = edgeID
	for _, field := range []struct{ name, value string }{{"workflow_id", workflowID}, {"transition_group_id", transitionGroupID}, {"target_node_id", targetNodeID}, {"context_mode", contextMode}} {
		if err := validateRequired(field.name, field.value); err != nil {
			return err
		}
	}
	if err := validateModelKey("key", key); err != nil {
		return err
	}
	if err := validateWorkflowContextSource(contextSource); err != nil {
		return err
	}
	for _, binding := range inputBindings {
		if err := validateModelKey("input_binding.name", binding.Name); err != nil {
			return err
		}
		if err := validateRequired("input_binding.source", binding.Source); err != nil {
			return err
		}
	}
	for _, requirement := range outputRequirements {
		if err := validateModelKey("output_requirement.field_name", requirement.FieldName); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkflowContextSource(source WorkflowContextSource) error {
	switch strings.TrimSpace(source.Kind) {
	case "", "immediate_source":
		if strings.TrimSpace(source.NodeKey) != "" {
			return workflowRequestError(WorkflowRequestErrorInvalidValue, "context_source.node_key", "context_source.node_key must be empty for immediate_source")
		}
		return nil
	case "selected_node":
		if err := validateModelKey("context_source.node_key", source.NodeKey); err != nil {
			return err
		}
		return nil
	default:
		return workflowRequestError(WorkflowRequestErrorInvalidValue, "context_source.kind", "context_source.kind is invalid")
	}
}

func (r WorkflowLinkProjectRequest) Validate() error {
	if err := validateRequired("project_id", r.ProjectID); err != nil {
		return err
	}
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	return validateWorkflowProjectLinkDefaultMode(r.DefaultPolicy)
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

func (r WorkflowDeletePreviewRequest) Validate() error {
	return validateRequired("workflow_id", r.WorkflowID)
}

func (r WorkflowDeleteRequest) Validate() error {
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	if r.ExpectedGraphRevision < 0 {
		return WorkflowRequestValidationError{Code: WorkflowRequestErrorInvalidMode, Field: "expected_graph_revision", Message: "expected graph revision must be non-negative"}
	}
	for _, field := range []struct {
		name  string
		value int64
	}{
		{"expected_project_count", r.ExpectedProjectCount},
		{"expected_link_count", r.ExpectedLinkCount},
		{"expected_task_count", r.ExpectedTaskCount},
	} {
		if field.value < 0 {
			return WorkflowRequestValidationError{Code: WorkflowRequestErrorInvalidMode, Field: field.name, Message: "expected count must be non-negative"}
		}
	}
	return nil
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

func (r WorkflowGraphValidateDraftRequest) Validate() error {
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	if err := validateWorkflowGraphValidationModes(r.Modes); err != nil {
		return err
	}
	return validateWorkflowGraphDraftEnvelope(r.Graph)
}

func (r WorkflowGraphSavePreviewRequest) Validate() error {
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	if r.ExpectedGraphRevision < 0 {
		return workflowRequestError(WorkflowRequestErrorInvalidValue, "expected_graph_revision", "expected_graph_revision must be non-negative")
	}
	return validateWorkflowGraphDraftEnvelope(r.Graph)
}

func (r WorkflowGraphSaveRequest) Validate() error {
	if err := (WorkflowGraphSavePreviewRequest{WorkflowID: r.WorkflowID, ExpectedGraphRevision: r.ExpectedGraphRevision, Graph: r.Graph}).Validate(); err != nil {
		return err
	}
	if r.Confirmation == nil {
		return nil
	}
	for _, field := range []struct {
		name  string
		value int64
	}{
		{"expected_removed_node_count", r.Confirmation.ExpectedRemovedNodeCount},
		{"expected_removed_transition_group_count", r.Confirmation.ExpectedRemovedTransitionGroupCount},
		{"expected_removed_edge_count", r.Confirmation.ExpectedRemovedEdgeCount},
		{"expected_node_task_reference_count", r.Confirmation.ExpectedNodeTaskReferenceCount},
		{"expected_edge_task_reference_count", r.Confirmation.ExpectedEdgeTaskReferenceCount},
	} {
		if field.value < 0 {
			return workflowRequestError(WorkflowRequestErrorInvalidValue, field.name, field.name+" must be non-negative")
		}
	}
	return nil
}

func validateWorkflowGraphValidationModes(modes []WorkflowValidationMode) error {
	if len(modes) == 0 {
		return workflowRequestError(WorkflowRequestErrorRequired, "modes", "modes is required")
	}
	for _, mode := range modes {
		switch mode {
		case WorkflowValidationModeDraft, WorkflowValidationModeTaskCreation, WorkflowValidationModeExecution:
		default:
			return workflowRequestError(WorkflowRequestErrorInvalidMode, "modes", "modes must contain only draft, task_creation, or execution")
		}
	}
	return nil
}

func validateWorkflowGraphDraftEnvelope(graph WorkflowGraphDraft) error {
	for _, field := range []struct {
		name  string
		count int
		limit int
	}{
		{"node_groups", len(graph.NodeGroups), WorkflowGraphDraftMaxNodeGroups},
		{"nodes", len(graph.Nodes), WorkflowGraphDraftMaxNodes},
		{"transition_groups", len(graph.TransitionGroups), WorkflowGraphDraftMaxTransitionGroups},
		{"edges", len(graph.Edges), WorkflowGraphDraftMaxEdges},
	} {
		if field.count > field.limit {
			return workflowRequestError(WorkflowRequestErrorTooLong, "graph."+field.name, fmt.Sprintf("%s must be <= %d", field.name, field.limit))
		}
	}
	for _, node := range graph.Nodes {
		if len(node.OutputFields) > WorkflowGraphDraftMaxFieldsPerEntity {
			return workflowRequestError(WorkflowRequestErrorTooLong, "graph.nodes.output_fields", fmt.Sprintf("output_fields must be <= %d", WorkflowGraphDraftMaxFieldsPerEntity))
		}
	}
	for _, edge := range graph.Edges {
		if len(edge.InputBindings) > WorkflowGraphDraftMaxFieldsPerEntity {
			return workflowRequestError(WorkflowRequestErrorTooLong, "graph.edges.input_bindings", fmt.Sprintf("input_bindings must be <= %d", WorkflowGraphDraftMaxFieldsPerEntity))
		}
		if len(edge.OutputRequirements) > WorkflowGraphDraftMaxFieldsPerEntity {
			return workflowRequestError(WorkflowRequestErrorTooLong, "graph.edges.output_requirements", fmt.Sprintf("output_requirements must be <= %d", WorkflowGraphDraftMaxFieldsPerEntity))
		}
	}
	return nil
}

func (r WorkflowTaskCreateRequest) Validate() error {
	for _, field := range []struct{ name, value string }{{"project_id", r.ProjectID}, {"title", r.Title}} {
		if err := validateRequired(field.name, field.value); err != nil {
			return err
		}
	}
	return nil
}

func (r WorkflowTaskUpdateRequest) Validate() error {
	for _, field := range []struct{ name, value string }{{"task_id", r.TaskID}, {"title", r.Title}} {
		if err := validateRequired(field.name, field.value); err != nil {
			return err
		}
	}
	return nil
}

func (r WorkflowTaskStartRequest) Validate() error {
	return validateRequired("task_id", r.TaskID)
}

func (r WorkflowTaskResumeRequest) Validate() error {
	return validateRequired("task_id", r.TaskID)
}

func (r WorkflowTaskApproveRequest) Validate() error {
	if strings.TrimSpace(r.TaskTransitionID) != "" {
		return nil
	}
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

func (r WorkflowTaskInterruptRequest) Validate() error {
	return validateRequired("task_id", r.TaskID)
}

func (r WorkflowAttentionListRequest) Validate() error {
	if r.PageSize < 0 {
		return WorkflowRequestValidationError{Code: WorkflowRequestErrorInvalidMode, Field: "page_size", Message: "page_size must be non-negative"}
	}
	return nil
}

func (r WorkflowTaskAttentionListRequest) Validate() error {
	return validateRequired("task_id", r.TaskID)
}

func (r WorkflowTaskQuestionAnswerRequest) Validate() error {
	for _, field := range []struct{ name, value string }{{"client_request_id", r.ClientRequestID}, {"task_id", r.TaskID}, {"ask_id", r.AskID}} {
		if err := validateRequired(field.name, field.value); err != nil {
			return err
		}
	}
	hasTextAnswer := strings.TrimSpace(r.Answer) != ""
	hasFreeform := strings.TrimSpace(r.FreeformAnswer) != ""
	if r.SelectedOptionNumber < 0 {
		return WorkflowRequestValidationError{Code: WorkflowRequestErrorInvalidMode, Field: "selected_option_number", Message: "selected_option_number must be non-negative"}
	}
	hasSelected := r.SelectedOptionNumber > 0
	hasAnswer := hasTextAnswer || hasFreeform || hasSelected
	hasError := strings.TrimSpace(r.ErrorMessage) != ""
	if hasAnswer && hasError {
		return WorkflowRequestValidationError{Code: WorkflowRequestErrorInvalidMode, Field: "error_message", Message: "error_message cannot be combined with answer fields"}
	}
	if hasTextAnswer && (hasFreeform || hasSelected) {
		return WorkflowRequestValidationError{Code: WorkflowRequestErrorInvalidMode, Field: "answer", Message: "answer cannot be combined with selected_option_number or freeform_answer"}
	}
	if !hasAnswer && !hasError {
		return WorkflowRequestValidationError{Code: WorkflowRequestErrorRequired, Field: "answer", Message: "answer is required"}
	}
	return nil
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
	if err := validateRequired("project_id", r.ProjectID); err != nil {
		return err
	}
	if r.DonePreviewLimit < 0 {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "done_preview_limit", "done_preview_limit must be non-negative")
	}
	if r.PageSize < 0 {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "page_size", "page_size must be non-negative")
	}
	if strings.TrimSpace(r.PageToken) != r.PageToken {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "page_token", "page_token must not have leading or trailing whitespace")
	}
	return nil
}

func (r WorkflowBoardNodeCardsListRequest) Validate() error {
	if err := validateRequired("project_id", r.ProjectID); err != nil {
		return err
	}
	if err := validateRequired("workflow_id", r.WorkflowID); err != nil {
		return err
	}
	if err := validateRequired("node_id", r.NodeID); err != nil {
		return err
	}
	if r.PageSize < 0 {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "page_size", "page_size must be non-negative")
	}
	if strings.TrimSpace(r.PageToken) != r.PageToken {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "page_token", "page_token must not have leading or trailing whitespace")
	}
	return nil
}

func (r WorkflowProjectSubscribeRequest) Validate() error {
	if strings.TrimSpace(r.ProjectID) != r.ProjectID {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "project_id", "project_id must not have leading or trailing whitespace")
	}
	return nil
}

func (r WorkflowTaskGetRequest) Validate() error {
	return validateRequired("task_id", r.TaskID)
}

func (r WorkflowTaskActivityListRequest) Validate() error {
	if err := validateRequired("task_id", r.TaskID); err != nil {
		return err
	}
	if r.PageSize < 0 {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "page_size", "page_size must be non-negative")
	}
	if strings.TrimSpace(r.PageToken) != r.PageToken {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "page_token", "page_token must not have leading or trailing whitespace")
	}
	return nil
}

func (r WorkflowTaskTeleportTargetRequest) Validate() error {
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

func validateWorkflowProjectLinkDefaultMode(mode WorkflowProjectLinkDefaultMode) error {
	switch mode {
	case "", WorkflowProjectLinkDefaultNever, WorkflowProjectLinkDefaultAlways, WorkflowProjectLinkDefaultIfProjectHasNone:
		return nil
	default:
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "default_policy", "default_policy must be never, always, or if_project_has_none")
	}
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
