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

var TaskExecutionTargetProvenance = workflowValueNames(map[pb.ExecutionTargetProvenance]string{
	pb.ExecutionTargetProvenance_EXECUTION_TARGET_PROVENANCE_RESOLVED:        "resolved",
	pb.ExecutionTargetProvenance_EXECUTION_TARGET_PROVENANCE_LEGACY_OBSERVED: "legacy_observed",
})

var TaskSearchMode = workflowValueNames(map[pb.SearchMode]string{
	pb.SearchMode_SEARCH_MODE_LITERAL: "literal",
	pb.SearchMode_SEARCH_MODE_FTS5:    "fts5",
})

var TaskSearchSourceKind = workflowValueNames(map[pb.SearchSourceKind]string{
	pb.SearchSourceKind_SEARCH_SOURCE_KIND_SHORT_ID: "short_id",
	pb.SearchSourceKind_SEARCH_SOURCE_KIND_TITLE:    "title",
	pb.SearchSourceKind_SEARCH_SOURCE_KIND_BODY:     "body",
	pb.SearchSourceKind_SEARCH_SOURCE_KIND_COMMENT:  "comment",
})

var TaskDependencyDirection = workflowValueNames(map[pb.DependencyDirection]string{
	pb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKED_BY: "blocked-by",
	pb.DependencyDirection_DEPENDENCY_DIRECTION_BLOCKS:     "blocks",
})

var TaskDependencySatisfaction = workflowValueNames(map[pb.DependencySatisfaction]string{
	pb.DependencySatisfaction_DEPENDENCY_SATISFACTION_SATISFIED:   "satisfied",
	pb.DependencySatisfaction_DEPENDENCY_SATISFACTION_UNSATISFIED: "unsatisfied",
})

var TaskDependencyMutationOutcome = workflowValueNames(map[pb.DependencyMutationOutcome]string{
	pb.DependencyMutationOutcome_DEPENDENCY_MUTATION_OUTCOME_ADDED:           "added",
	pb.DependencyMutationOutcome_DEPENDENCY_MUTATION_OUTCOME_ALREADY_PRESENT: "already_present",
	pb.DependencyMutationOutcome_DEPENDENCY_MUTATION_OUTCOME_REMOVED:         "removed",
	pb.DependencyMutationOutcome_DEPENDENCY_MUTATION_OUTCOME_ALREADY_ABSENT:  "already_absent",
})

var TaskDependencyErrorReason = workflowValueNames(map[pb.DependencyErrorReason]string{
	pb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_MISSING_TASK:     "missing_task",
	pb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_SELF:             "self_dependency",
	pb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_PROJECT_MISMATCH: "project_mismatch",
	pb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_RECIPROCAL:       "reciprocal_dependency",
	pb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_BLOCKER_LIMIT:    "blocker_limit",
	pb.DependencyErrorReason_DEPENDENCY_ERROR_REASON_BLOCKED_LIMIT:    "blocked_limit",
})

var TaskExecutionResolutionCode = workflowValueNames(map[pb.ExecutionTargetResolutionCode]string{
	pb.ExecutionTargetResolutionCode_EXECUTION_TARGET_RESOLUTION_CODE_INVALID_REVISION: "invalid_revision",
	pb.ExecutionTargetResolutionCode_EXECUTION_TARGET_RESOLUTION_CODE_NON_COMMIT:       "non_commit",
	pb.ExecutionTargetResolutionCode_EXECUTION_TARGET_RESOLUTION_CODE_GIT_FAILURE:      "git_failure",
})

var TaskInitialBranchReason = workflowValueNames(map[pb.InitialBranchErrorReason]string{
	pb.InitialBranchErrorReason_INITIAL_BRANCH_ERROR_REASON_INVALID_NAME:                     "invalid_name",
	pb.InitialBranchErrorReason_INITIAL_BRANCH_ERROR_REASON_LOCAL_COLLISION:                  "local_collision",
	pb.InitialBranchErrorReason_INITIAL_BRANCH_ERROR_REASON_REMOTE_TRACKING_COLLISION:        "remote_tracking_collision",
	pb.InitialBranchErrorReason_INITIAL_BRANCH_ERROR_REASON_NO_MANAGED_TARGET:                "no_managed_target",
	pb.InitialBranchErrorReason_INITIAL_BRANCH_ERROR_REASON_OPERATION_CANNOT_CREATE_WORKTREE: "operation_cannot_create_worktree",
	pb.InitialBranchErrorReason_INITIAL_BRANCH_ERROR_REASON_POST_CREATION_MISMATCH:           "post_creation_mismatch",
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
