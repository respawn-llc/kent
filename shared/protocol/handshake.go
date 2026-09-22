package protocol

import (
	"core/shared/clientui"
	"core/shared/runtimeids"
)

type SubscribeResponse struct {
	Stream string `json:"stream"`
}

type AttentionNotificationEventParams struct {
	Event clientui.AttentionNotificationEvent `json:"event"`
}

type WorkflowProjectEventParams struct {
	Event WorkflowProjectEvent `json:"event"`
}

type WorkflowProjectEventResource string

const (
	WorkflowProjectEventResourceWorkflow     WorkflowProjectEventResource = "workflow"
	WorkflowProjectEventResourceWorkflowLink WorkflowProjectEventResource = "workflow_link"
	WorkflowProjectEventResourceTask         WorkflowProjectEventResource = "task"
	WorkflowProjectEventResourceLabel        WorkflowProjectEventResource = "label"
)

type WorkflowProjectEventAction string

const (
	WorkflowProjectEventActionCreated             WorkflowProjectEventAction = "created"
	WorkflowProjectEventActionUpdated             WorkflowProjectEventAction = "updated"
	WorkflowProjectEventActionRenamed             WorkflowProjectEventAction = "renamed"
	WorkflowProjectEventActionReordered           WorkflowProjectEventAction = "reordered"
	WorkflowProjectEventActionDeleted             WorkflowProjectEventAction = "deleted"
	WorkflowProjectEventActionGraphSaved          WorkflowProjectEventAction = "graph_saved"
	WorkflowProjectEventActionLinked              WorkflowProjectEventAction = "linked"
	WorkflowProjectEventActionDefaultChanged      WorkflowProjectEventAction = "default_changed"
	WorkflowProjectEventActionUnlinked            WorkflowProjectEventAction = "unlinked"
	WorkflowProjectEventActionStarted             WorkflowProjectEventAction = "started"
	WorkflowProjectEventActionInterrupted         WorkflowProjectEventAction = "interrupted"
	WorkflowProjectEventActionResumed             WorkflowProjectEventAction = "resumed"
	WorkflowProjectEventActionApproved            WorkflowProjectEventAction = "approved"
	WorkflowProjectEventActionMoved               WorkflowProjectEventAction = "moved"
	WorkflowProjectEventActionCanceled            WorkflowProjectEventAction = "canceled"
	WorkflowProjectEventActionCompleted           WorkflowProjectEventAction = "completed"
	WorkflowProjectEventActionCommentAdded        WorkflowProjectEventAction = "comment_added"
	WorkflowProjectEventActionCommentUpdated      WorkflowProjectEventAction = "comment_updated"
	WorkflowProjectEventActionCommentDeleted      WorkflowProjectEventAction = "comment_deleted"
	WorkflowProjectEventActionQuestionWaiting     WorkflowProjectEventAction = "question_waiting"
	WorkflowProjectEventActionQuestionCleared     WorkflowProjectEventAction = "question_cleared"
	WorkflowProjectEventActionLabelsChanged       WorkflowProjectEventAction = "labels_changed"
	WorkflowProjectEventActionDependenciesChanged WorkflowProjectEventAction = "dependencies_changed"
)

type WorkflowProjectEvent struct {
	ProjectID        *string                      `json:"project_id,omitempty"`
	WorkflowID       *runtimeids.WorkflowID       `json:"workflow_id,omitempty"`
	Resource         WorkflowProjectEventResource `json:"resource"`
	Action           WorkflowProjectEventAction   `json:"action"`
	PrimaryEntityID  string                       `json:"primary_entity_id"`
	RelatedIDs       []string                     `json:"related_ids,omitempty"`
	OccurredAtUnixMs int64                        `json:"occurred_at_unix_ms"`
}

type StreamCompleteParams struct {
	Code                  int    `json:"code,omitempty"`
	Message               string `json:"message,omitempty"`
	TranscriptCloseReason string `json:"transcript_close_reason,omitempty"`
}
