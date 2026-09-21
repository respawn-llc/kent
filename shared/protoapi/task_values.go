package protoapi

import (
	"fmt"

	pb "core/shared/protoapi/gen/kent/api/workflow_task"
)

var TaskStatusKind = workflowValueNames(map[pb.TaskStatusKind]string{
	pb.TaskStatusKind_TASK_STATUS_KIND_DONE:             "done",
	pb.TaskStatusKind_TASK_STATUS_KIND_WAITING_QUESTION: "waiting_question",
	pb.TaskStatusKind_TASK_STATUS_KIND_WAITING_APPROVAL: "waiting_approval",
	pb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED:      "interrupted",
	pb.TaskStatusKind_TASK_STATUS_KIND_RUNNING:          "running",
	pb.TaskStatusKind_TASK_STATUS_KIND_QUEUED:           "queued",
	pb.TaskStatusKind_TASK_STATUS_KIND_BACKLOG:          "backlog",
	pb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE:           "active",
})

var TaskAttentionKind = workflowValueNames(map[pb.TaskAttentionKind]string{
	pb.TaskAttentionKind_TASK_ATTENTION_KIND_QUESTION:    "question",
	pb.TaskAttentionKind_TASK_ATTENTION_KIND_APPROVAL:    "approval",
	pb.TaskAttentionKind_TASK_ATTENTION_KIND_INTERRUPTED: "interrupted",
})

var TaskListSortField = workflowValueNames(map[pb.ListSortField]string{
	pb.ListSortField_LIST_SORT_FIELD_CREATED:  "created",
	pb.ListSortField_LIST_SORT_FIELD_UPDATED:  "updated",
	pb.ListSortField_LIST_SORT_FIELD_STATUS:   "status",
	pb.ListSortField_LIST_SORT_FIELD_COLUMN:   "column",
	pb.ListSortField_LIST_SORT_FIELD_TITLE:    "title",
	pb.ListSortField_LIST_SORT_FIELD_LABELS:   "labels",
	pb.ListSortField_LIST_SORT_FIELD_SHORT_ID: "short_id",
})

var TaskListSortDirection = workflowValueNames(map[pb.ListSortDirection]string{
	pb.ListSortDirection_LIST_SORT_DIRECTION_ASC:  "asc",
	pb.ListSortDirection_LIST_SORT_DIRECTION_DESC: "desc",
})

var TaskGroup = workflowValueNames(map[pb.ProjectTaskGroup]string{
	pb.ProjectTaskGroup_PROJECT_TASK_GROUP_ACTIVE:  "active",
	pb.ProjectTaskGroup_PROJECT_TASK_GROUP_BACKLOG: "backlog",
	pb.ProjectTaskGroup_PROJECT_TASK_GROUP_DONE:    "done",
})

var TaskLabelFilterMode = workflowValueNames(map[pb.NamedLabelFilterMode]string{
	pb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ANY: "any",
	pb.NamedLabelFilterMode_NAMED_LABEL_FILTER_MODE_ALL: "all",
})

var TaskCardinality = workflowValueNames(map[pb.MatchingWorkflowCardinality]string{
	pb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_NONE:     "none",
	pb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_ONE:      "one",
	pb.MatchingWorkflowCardinality_MATCHING_WORKFLOW_CARDINALITY_MULTIPLE: "multiple",
})

var TaskNativeStateName = workflowValueNames(map[pb.TaskNativeState]string{
	pb.TaskNativeState_TASK_NATIVE_STATE_TERMINAL:         "terminal",
	pb.TaskNativeState_TASK_NATIVE_STATE_WAITING_ASK:      "waiting_ask",
	pb.TaskNativeState_TASK_NATIVE_STATE_WAITING_APPROVAL: "waiting_approval",
	pb.TaskNativeState_TASK_NATIVE_STATE_INTERRUPTED:      "interrupted",
	pb.TaskNativeState_TASK_NATIVE_STATE_RUNNING:          "running",
	pb.TaskNativeState_TASK_NATIVE_STATE_QUEUED:           "queued",
	pb.TaskNativeState_TASK_NATIVE_STATE_ACTIVE:           "active",
})

func TaskNativeState(kind pb.TaskStatusKind) (pb.TaskNativeState, error) {
	switch kind {
	case pb.TaskStatusKind_TASK_STATUS_KIND_DONE:
		return pb.TaskNativeState_TASK_NATIVE_STATE_TERMINAL, nil
	case pb.TaskStatusKind_TASK_STATUS_KIND_WAITING_QUESTION:
		return pb.TaskNativeState_TASK_NATIVE_STATE_WAITING_ASK, nil
	case pb.TaskStatusKind_TASK_STATUS_KIND_WAITING_APPROVAL:
		return pb.TaskNativeState_TASK_NATIVE_STATE_WAITING_APPROVAL, nil
	case pb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED:
		return pb.TaskNativeState_TASK_NATIVE_STATE_INTERRUPTED, nil
	case pb.TaskStatusKind_TASK_STATUS_KIND_RUNNING:
		return pb.TaskNativeState_TASK_NATIVE_STATE_RUNNING, nil
	case pb.TaskStatusKind_TASK_STATUS_KIND_QUEUED:
		return pb.TaskNativeState_TASK_NATIVE_STATE_QUEUED, nil
	case pb.TaskStatusKind_TASK_STATUS_KIND_BACKLOG, pb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE:
		return pb.TaskNativeState_TASK_NATIVE_STATE_ACTIVE, nil
	default:
		return pb.TaskNativeState_TASK_NATIVE_STATE_UNSPECIFIED, fmt.Errorf("invalid Task status %v", kind)
	}
}

func TaskGroupDefinitions() []*pb.ProjectTaskGroupDefinition {
	return []*pb.ProjectTaskGroupDefinition{
		{Group: pb.ProjectTaskGroup_PROJECT_TASK_GROUP_ACTIVE, StatusKinds: []pb.TaskStatusKind{
			pb.TaskStatusKind_TASK_STATUS_KIND_WAITING_QUESTION,
			pb.TaskStatusKind_TASK_STATUS_KIND_WAITING_APPROVAL,
			pb.TaskStatusKind_TASK_STATUS_KIND_INTERRUPTED,
			pb.TaskStatusKind_TASK_STATUS_KIND_RUNNING,
			pb.TaskStatusKind_TASK_STATUS_KIND_QUEUED,
			pb.TaskStatusKind_TASK_STATUS_KIND_ACTIVE,
		}},
		{Group: pb.ProjectTaskGroup_PROJECT_TASK_GROUP_BACKLOG, StatusKinds: []pb.TaskStatusKind{pb.TaskStatusKind_TASK_STATUS_KIND_BACKLOG}},
		{Group: pb.ProjectTaskGroup_PROJECT_TASK_GROUP_DONE, StatusKinds: []pb.TaskStatusKind{pb.TaskStatusKind_TASK_STATUS_KIND_DONE}},
	}
}
