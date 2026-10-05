package workflowcontract

import "core/shared/runtimeids"

type EventResource string

const (
	EventResourceWorkflow     EventResource = "workflow"
	EventResourceWorkflowLink EventResource = "workflow_link"
	EventResourceTask         EventResource = "task"
	EventResourceLabel        EventResource = "label"
)

type EventAction string

const (
	EventActionCreated             EventAction = "created"
	EventActionUpdated             EventAction = "updated"
	EventActionRenamed             EventAction = "renamed"
	EventActionReordered           EventAction = "reordered"
	EventActionDeleted             EventAction = "deleted"
	EventActionGraphSaved          EventAction = "graph_saved"
	EventActionLinked              EventAction = "linked"
	EventActionDefaultChanged      EventAction = "default_changed"
	EventActionUnlinked            EventAction = "unlinked"
	EventActionStarted             EventAction = "started"
	EventActionInterrupted         EventAction = "interrupted"
	EventActionResumed             EventAction = "resumed"
	EventActionApproved            EventAction = "approved"
	EventActionMoved               EventAction = "moved"
	EventActionCompleted           EventAction = "completed"
	EventActionCommentAdded        EventAction = "comment_added"
	EventActionCommentUpdated      EventAction = "comment_updated"
	EventActionCommentDeleted      EventAction = "comment_deleted"
	EventActionQuestionWaiting     EventAction = "question_waiting"
	EventActionQuestionCleared     EventAction = "question_cleared"
	EventActionLabelsChanged       EventAction = "labels_changed"
	EventActionDependenciesChanged EventAction = "dependencies_changed"
)

type Event struct {
	ProjectID        *string
	WorkflowID       *runtimeids.WorkflowID
	Resource         EventResource
	Action           EventAction
	PrimaryEntityID  string
	RelatedIDs       []string
	OccurredAtUnixMs int64
}
