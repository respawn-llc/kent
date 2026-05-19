package serverapi

import (
	"context"
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
	GroupID        string                `json:"group_id,omitempty"`
	GroupKey       string                `json:"group_key,omitempty"`
	SubagentRole   string                `json:"subagent_role,omitempty"`
	PromptTemplate string                `json:"prompt_template,omitempty"`
	OutputFields   []WorkflowOutputField `json:"output_fields,omitempty"`
}

type WorkflowNodeGroup struct {
	GroupID      string `json:"group_id"`
	WorkflowID   string `json:"workflow_id"`
	GroupKey     string `json:"group_key"`
	DisplayName  string `json:"display_name"`
	SortOrder    int    `json:"sort_order"`
	MetadataJSON string `json:"metadata_json,omitempty"`
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
	NodeGroups       []WorkflowNodeGroup       `json:"node_groups,omitempty"`
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
	GroupKey       string                `json:"group_key,omitempty"`
	SubagentRole   string                `json:"subagent_role,omitempty"`
	PromptTemplate string                `json:"prompt_template,omitempty"`
	OutputFields   []WorkflowOutputField `json:"output_fields,omitempty"`
}

type WorkflowNodeAddResponse struct {
	GraphRevision int64 `json:"graph_revision"`
}

type WorkflowNodeGroupAddRequest struct {
	WorkflowID   string `json:"workflow_id"`
	GroupID      string `json:"group_id,omitempty"`
	GroupKey     string `json:"group_key"`
	DisplayName  string `json:"display_name"`
	SortOrder    int    `json:"sort_order"`
	MetadataJSON string `json:"metadata_json,omitempty"`
}

type WorkflowNodeGroupUpdateRequest struct {
	WorkflowID   string `json:"workflow_id"`
	GroupID      string `json:"group_id"`
	GroupKey     string `json:"group_key"`
	DisplayName  string `json:"display_name"`
	SortOrder    int    `json:"sort_order"`
	MetadataJSON string `json:"metadata_json,omitempty"`
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
	Items               []WorkflowAttentionItem `json:"items"`
	NextPageToken       string                  `json:"next_page_token,omitempty"`
	GeneratedAtUnixMs   int64                   `json:"generated_at_unix_ms"`
	LatestEventSequence int64                   `json:"latest_event_sequence"`
}

type WorkflowTaskAttentionListRequest struct {
	TaskID string `json:"task_id"`
}

type WorkflowTaskAttentionListResponse struct {
	Items             []WorkflowAttentionItem `json:"items"`
	GeneratedAtUnixMs int64                   `json:"generated_at_unix_ms"`
}

type WorkflowAttentionItem struct {
	ID                  string `json:"id"`
	Kind                string `json:"kind"`
	ProjectID           string `json:"project_id,omitempty"`
	WorkflowID          string `json:"workflow_id,omitempty"`
	TaskID              string `json:"task_id,omitempty"`
	TaskShortID         string `json:"task_short_id,omitempty"`
	TaskTitle           string `json:"task_title,omitempty"`
	RunID               string `json:"run_id,omitempty"`
	SessionID           string `json:"session_id,omitempty"`
	AskID               string `json:"ask_id,omitempty"`
	TaskTransitionID    string `json:"task_transition_id,omitempty"`
	Message             string `json:"message"`
	OccurredAtUnixMs    int64  `json:"occurred_at_unix_ms"`
	LatestEventSequence int64  `json:"latest_event_sequence,omitempty"`
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
	ProjectID           string                  `json:"project_id"`
	WorkflowID          string                  `json:"workflow_id"`
	NodeID              string                  `json:"node_id"`
	Cards               []WorkflowBoardTaskCard `json:"cards"`
	NextPageToken       string                  `json:"next_page_token"`
	GeneratedAtUnixMs   int64                   `json:"generated_at_unix_ms"`
	LatestEventSequence int64                   `json:"latest_event_sequence"`
}

type WorkflowBoard struct {
	ProjectID           string                  `json:"project_id"`
	Project             ProjectBoardProject     `json:"project"`
	SelectedWorkflow    WorkflowPickerItem      `json:"selected_workflow"`
	WorkflowPicker      []WorkflowPickerItem    `json:"workflows"`
	Groups              []WorkflowBoardGroup    `json:"groups"`
	Columns             []WorkflowBoardColumn   `json:"columns"`
	Cards               []WorkflowBoardTaskCard `json:"cards"`
	DonePreview         []WorkflowBoardTaskCard `json:"done_preview"`
	HasHiddenDoneCards  bool                    `json:"has_hidden_done_cards"`
	NextPageToken       string                  `json:"next_page_token"`
	GeneratedAtUnixMs   int64                   `json:"generated_at_unix_ms"`
	LatestEventSequence int64                   `json:"latest_event_sequence"`
	Workflows           []WorkflowBoardWorkflow `json:"legacy_workflows,omitempty"`
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
	UnlinkedAtUnixMs     int64                     `json:"unlinked_at_unix_ms"`
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
	NodeID       string `json:"node_id"`
	Key          string `json:"key"`
	Kind         string `json:"kind"`
	DisplayName  string `json:"display_name"`
	AssigneeRole string `json:"assignee_role,omitempty"`
	SortOrder    int    `json:"sort_order"`
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
	ProjectID     string `json:"project_id,omitempty"`
	AfterSequence int64  `json:"after_sequence"`
}

type WorkflowProjectEvent struct {
	Sequence         int64    `json:"sequence"`
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
	DeletedAt       int64  `json:"deleted_at_unix_ms"`
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
	if strings.TrimSpace(r.GroupKey) != "" {
		if err := validateModelKey("group_key", r.GroupKey); err != nil {
			return err
		}
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
	if r.AfterSequence < 0 {
		return workflowRequestError(WorkflowRequestErrorInvalidMode, "after_sequence", "after_sequence must be non-negative")
	}
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
