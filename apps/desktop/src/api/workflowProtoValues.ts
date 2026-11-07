import {
  AssigneeSelection,
  CompletionMode,
  ContextMode,
  ContextSourceKind,
  ExecutionTargetMode,
  GraphEntityType,
  NodeKind,
  ParameterPurpose,
  SelectorApplicabilityReason,
  ThinkingSelection,
  ValidationMode,
  ProjectEventResource,
  ProjectEventAction,
} from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import { ContractError } from "./errors";
import {
  CreateSelectionReason,
  InitialBranchErrorReason,
  ExecutionTargetResolutionCode,
  MovePreviewBlocker,
  DependencyMutationOutcome,
  DependencyErrorReason,
  CommentAuthorKind,
} from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import {
  ExecutionTargetUnavailableCause,
  LockedExecutionTargetCause,
} from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import {
  TaskStatusKind,
  TaskNativeState,
  TaskAttentionKind,
  ListSortField,
  ListSortDirection,
  MatchingWorkflowCardinality,
  ProjectTaskGroup,
  DependencyDirection,
  DependencySatisfaction,
  ExecutionTargetProvenance,
  SearchMode,
  SearchSourceKind,
} from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";

function workflowEnum<Code extends number, Name extends string>(entries: readonly (readonly [Code, Name])[]) {
  const names = new Map<number, Name>(entries);
  const codes = new Map<string, Code>(entries.map(([code, name]) => [name, code]));
  return {
    encode: (name: string): Code => {
      const code = codes.get(name);
      if (code === undefined) throw new ContractError(`Unsupported Workflow value ${name}.`);
      return code;
    },
    decode: (code: number): Name => {
      const name = names.get(code);
      if (name === undefined) throw new ContractError(`Unsupported Workflow value ${code.toString()}.`);
      return name;
    },
  };
}

export const taskStatusKind = workflowEnum([
  [TaskStatusKind.DONE, "done"],
  [TaskStatusKind.WAITING_QUESTION, "waiting_question"],
  [TaskStatusKind.WAITING_APPROVAL, "waiting_approval"],
  [TaskStatusKind.INTERRUPTED, "interrupted"],
  [TaskStatusKind.RUNNING, "running"],
  [TaskStatusKind.QUEUED, "queued"],
  [TaskStatusKind.BACKLOG, "backlog"],
  [TaskStatusKind.ACTIVE, "active"],
]);

export const workflowEventResource = workflowEnum([
  [ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_WORKFLOW, "workflow"],
  [ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_WORKFLOW_LINK, "workflow_link"],
  [ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_TASK, "task"],
  [ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_LABEL, "label"],
  [ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_SESSION_CATALOG, "session_catalog"],
]);

export const workflowEventAction = workflowEnum([
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_CREATED, "created"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_UPDATED, "updated"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_RENAMED, "renamed"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_REORDERED, "reordered"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_DELETED, "deleted"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_GRAPH_SAVED, "graph_saved"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_LINKED, "linked"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_DEFAULT_CHANGED, "default_changed"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_UNLINKED, "unlinked"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_STARTED, "started"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_INTERRUPTED, "interrupted"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_RESUMED, "resumed"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_APPROVED, "approved"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_MOVED, "moved"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_COMPLETED, "completed"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_COMMENT_ADDED, "comment_added"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_COMMENT_UPDATED, "comment_updated"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_COMMENT_DELETED, "comment_deleted"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_QUESTION_WAITING, "question_waiting"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_QUESTION_CLEARED, "question_cleared"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_LABELS_CHANGED, "labels_changed"],
  [ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_DEPENDENCIES_CHANGED, "dependencies_changed"],
]);

export const taskNativeState = workflowEnum([
  [TaskNativeState.TERMINAL, "terminal"],
  [TaskNativeState.WAITING_ASK, "waiting_ask"],
  [TaskNativeState.WAITING_APPROVAL, "waiting_approval"],
  [TaskNativeState.INTERRUPTED, "interrupted"],
  [TaskNativeState.RUNNING, "running"],
  [TaskNativeState.QUEUED, "queued"],
  [TaskNativeState.ACTIVE, "active"],
]);

export const taskAttentionKind = workflowEnum([
  [TaskAttentionKind.QUESTION, "question"],
  [TaskAttentionKind.APPROVAL, "approval"],
  [TaskAttentionKind.INTERRUPTED, "interrupted"],
]);

export const taskSortField = workflowEnum([
  [ListSortField.CREATED, "created"],
  [ListSortField.UPDATED, "updated"],
  [ListSortField.STATUS, "status"],
  [ListSortField.COLUMN, "column"],
  [ListSortField.TITLE, "title"],
  [ListSortField.LABELS, "labels"],
  [ListSortField.SHORT_ID, "short_id"],
]);

export const taskSortDirection = workflowEnum([
  [ListSortDirection.ASC, "asc"],
  [ListSortDirection.DESC, "desc"],
]);

export const taskCardinality = workflowEnum([
  [MatchingWorkflowCardinality.NONE, "none"],
  [MatchingWorkflowCardinality.ONE, "one"],
  [MatchingWorkflowCardinality.MULTIPLE, "multiple"],
]);

export const projectTaskGroup = workflowEnum([
  [ProjectTaskGroup.ACTIVE, "active"],
  [ProjectTaskGroup.BACKLOG, "backlog"],
  [ProjectTaskGroup.DONE, "done"],
]);

export const taskDependencyDirection = workflowEnum([
  [DependencyDirection.BLOCKED_BY, "blocked-by"],
  [DependencyDirection.BLOCKS, "blocks"],
]);

export const taskDependencySatisfaction = workflowEnum([
  [DependencySatisfaction.SATISFIED, "satisfied"],
  [DependencySatisfaction.UNSATISFIED, "unsatisfied"],
]);

export const taskDependencyMutationOutcome = workflowEnum([
  [DependencyMutationOutcome.ADDED, "added"],
  [DependencyMutationOutcome.ALREADY_PRESENT, "already_present"],
  [DependencyMutationOutcome.REMOVED, "removed"],
  [DependencyMutationOutcome.ALREADY_ABSENT, "already_absent"],
]);

export const taskDependencyErrorReason = workflowEnum([
  [DependencyErrorReason.MISSING_TASK, "missing_task"],
  [DependencyErrorReason.SELF, "self_dependency"],
  [DependencyErrorReason.PROJECT_MISMATCH, "project_mismatch"],
  [DependencyErrorReason.RECIPROCAL, "reciprocal_dependency"],
  [DependencyErrorReason.BLOCKER_LIMIT, "blocker_limit"],
  [DependencyErrorReason.BLOCKED_LIMIT, "blocked_limit"],
]);

export const taskCommentAuthor = workflowEnum([
  [CommentAuthorKind.USER, "user"],
  [CommentAuthorKind.AGENT, "agent"],
]);

export const taskExecutionProvenance = workflowEnum([
  [ExecutionTargetProvenance.RESOLVED, "resolved"],
  [ExecutionTargetProvenance.LEGACY_OBSERVED, "legacy_observed"],
]);

export const taskSearchMode = workflowEnum([
  [SearchMode.LITERAL, "literal"],
  [SearchMode.FTS5, "fts5"],
]);

export const taskSearchSourceKind = workflowEnum([
  [SearchSourceKind.SHORT_ID, "short_id"],
  [SearchSourceKind.TITLE, "title"],
  [SearchSourceKind.BODY, "body"],
  [SearchSourceKind.COMMENT, "comment"],
]);

export const taskCreateSelectionReason = workflowEnum([
  [CreateSelectionReason.NO_LINKED_WORKFLOWS, "no_linked_workflows"],
  [CreateSelectionReason.WORKFLOW_NOT_LINKED, "workflow_not_linked"],
  [CreateSelectionReason.AMBIGUOUS_WITHOUT_DEFAULT, "ambiguous_without_default"],
]);

export const taskOriginalTargetCause = workflowEnum([
  [LockedExecutionTargetCause.DETACHED_HEAD, "detached_head"],
  [LockedExecutionTargetCause.INVALID_ROOT, "invalid_root"],
  [LockedExecutionTargetCause.ROOT_INACCESSIBLE, "root_inaccessible"],
  [LockedExecutionTargetCause.MISSING_BRANCH, "missing_branch"],
  [LockedExecutionTargetCause.CONFLICT, "conflict"],
  [LockedExecutionTargetCause.GIT_FAILURE, "git_failure"],
]);

export const taskUnavailableTargetCause = workflowEnum([
  [ExecutionTargetUnavailableCause.INVALID_REVISION, "invalid_revision"],
  [ExecutionTargetUnavailableCause.NON_COMMIT, "non_commit"],
  [ExecutionTargetUnavailableCause.DEFAULT_BRANCH_MISSING, "default_branch_missing"],
  [ExecutionTargetUnavailableCause.DEFAULT_BRANCH_AMBIGUOUS, "default_branch_ambiguous"],
  [ExecutionTargetUnavailableCause.GIT_FAILURE, "git_failure"],
]);

export const taskInitialBranchReason = workflowEnum([
  [InitialBranchErrorReason.INVALID_NAME, "invalid_name"],
  [InitialBranchErrorReason.LOCAL_COLLISION, "local_collision"],
  [InitialBranchErrorReason.REMOTE_TRACKING_COLLISION, "remote_tracking_collision"],
  [InitialBranchErrorReason.NO_MANAGED_TARGET, "no_managed_target"],
  [InitialBranchErrorReason.OPERATION_CANNOT_CREATE_WORKTREE, "operation_cannot_create_worktree"],
  [InitialBranchErrorReason.POST_CREATION_MISMATCH, "post_creation_mismatch"],
]);

export const taskExecutionResolutionCode = workflowEnum([
  [ExecutionTargetResolutionCode.INVALID_REVISION, "invalid_revision"],
  [ExecutionTargetResolutionCode.NON_COMMIT, "non_commit"],
  [ExecutionTargetResolutionCode.GIT_FAILURE, "git_failure"],
  [ExecutionTargetResolutionCode.DEFAULT_BRANCH_MISSING, "default_branch_missing"],
  [ExecutionTargetResolutionCode.DEFAULT_BRANCH_AMBIGUOUS, "default_branch_ambiguous"],
]);

export const taskMovePreviewBlocker = workflowEnum([
  [MovePreviewBlocker.INVALID_WORKFLOW, "invalid_workflow"],
  [MovePreviewBlocker.NO_SOURCE_POSITION, "no_source_position"],
  [MovePreviewBlocker.UNSUPPORTED_DESTINATION, "unsupported_destination"],
  [MovePreviewBlocker.LIFECYCLE_CONFLICT, "lifecycle_conflict"],
  [MovePreviewBlocker.CONTEXT_SESSION_UNAVAILABLE, "context_session_unavailable"],
  [MovePreviewBlocker.NO_USABLE_TRANSITION, "no_usable_transition"],
  [MovePreviewBlocker.PARALLEL_BRANCH_REQUIRES_FAN_OUT, "parallel_branch_requires_fan_out"],
]);

export const workflowNodeKind = workflowEnum([
  [NodeKind.WORKFLOW_NODE_KIND_START, "start"],
  [NodeKind.WORKFLOW_NODE_KIND_AGENT, "agent"],
  [NodeKind.WORKFLOW_NODE_KIND_SCRIPT, "script"],
  [NodeKind.WORKFLOW_NODE_KIND_JOIN, "join"],
  [NodeKind.WORKFLOW_NODE_KIND_TERMINAL, "terminal"],
]);
export const workflowValidationMode = workflowEnum([
  [ValidationMode.WORKFLOW_VALIDATION_MODE_DRAFT, "draft"],
  [ValidationMode.WORKFLOW_VALIDATION_MODE_TASK_CREATION, "task_creation"],
  [ValidationMode.WORKFLOW_VALIDATION_MODE_EXECUTION, "execution"],
]);
export const workflowExecutionTargetMode = workflowEnum([
  [ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_NONE, "none"],
  [ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_HEAD, "head"],
  [ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_DEFAULT_BRANCH, "default_branch"],
  [ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_CUSTOM_REF, "custom_ref"],
  [ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_ASK_ON_FIRST_EXECUTION, "ask_on_first_execution"],
]);
export const workflowCompletionMode = workflowEnum([
  [CompletionMode.WORKFLOW_COMPLETION_MODE_AUTO, "auto"],
  [CompletionMode.WORKFLOW_COMPLETION_MODE_STRUCTURED_OUTPUT, "structured_output"],
  [CompletionMode.WORKFLOW_COMPLETION_MODE_TOOL, "tool"],
  [CompletionMode.WORKFLOW_COMPLETION_MODE_SHELL_COMMAND, "shell_command"],
  [CompletionMode.WORKFLOW_COMPLETION_MODE_UNSTRUCTURED_OUTPUT, "unstructured_output"],
]);
export const workflowAssigneeSelection = workflowEnum([
  [AssigneeSelection.WORKFLOW_ASSIGNEE_SELECTION_CONFIGURED, "configured"],
  [AssigneeSelection.WORKFLOW_ASSIGNEE_SELECTION_PREVIOUS_NODE, "previous_node"],
]);
export const workflowThinkingSelection = workflowEnum([
  [ThinkingSelection.WORKFLOW_THINKING_SELECTION_CONFIGURED, "configured"],
  [ThinkingSelection.WORKFLOW_THINKING_SELECTION_PREVIOUS_NODE, "previous_node"],
]);
export const workflowContextMode = workflowEnum([
  [ContextMode.WORKFLOW_CONTEXT_MODE_NEW_SESSION, "new_session"],
  [ContextMode.WORKFLOW_CONTEXT_MODE_CONTINUE_SESSION, "continue_session"],
  [ContextMode.WORKFLOW_CONTEXT_MODE_COMPACT_AND_CONTINUE_SESSION, "compact_and_continue_session"],
]);
export const workflowContextSourceKind = workflowEnum([
  [ContextSourceKind.WORKFLOW_CONTEXT_SOURCE_KIND_IMMEDIATE_SOURCE, "immediate_source"],
  [ContextSourceKind.WORKFLOW_CONTEXT_SOURCE_KIND_SELECTED_NODE, "selected_node"],
  [ContextSourceKind.WORKFLOW_CONTEXT_SOURCE_KIND_PREVIOUS_TARGET, "previous_target"],
  [ContextSourceKind.WORKFLOW_CONTEXT_SOURCE_KIND_PREVIOUS_TARGET_OR_NEW, "previous_target_or_new"],
]);
export const workflowParameterPurpose = workflowEnum([
  [ParameterPurpose.WORKFLOW_PARAMETER_PURPOSE_ORDINARY, "ordinary"],
  [ParameterPurpose.WORKFLOW_PARAMETER_PURPOSE_TARGET_ASSIGNEE, "target_assignee"],
  [ParameterPurpose.WORKFLOW_PARAMETER_PURPOSE_TARGET_THINKING, "target_thinking"],
]);
export const workflowSelectorReason = workflowEnum([
  [SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_ELIGIBLE, "eligible"],
  [SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_TOPOLOGY, "topology"],
  [SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_CONTEXT_SOURCE, "context_source"],
  [SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_NO_CALLABLE_ROLES, "no_callable_roles"],
  [
    SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_NO_THINKING_SUPPORT,
    "no_thinking_support",
  ],
  [
    SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_UNAVAILABLE_CONFIGURATION,
    "unavailable_configuration",
  ],
  [
    SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_SOLE_CALLABLE_ROLE,
    "sole_callable_role",
  ],
  [
    SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_NO_THINKING_LEVELS,
    "no_thinking_levels",
  ],
  [
    SelectorApplicabilityReason.WORKFLOW_SELECTOR_APPLICABILITY_REASON_SOLE_THINKING_LEVEL,
    "sole_thinking_level",
  ],
]);
export const workflowGraphEntityType = workflowEnum([
  [GraphEntityType.WORKFLOW_GRAPH_ENTITY_TYPE_EDGE, "edge"],
  [GraphEntityType.WORKFLOW_GRAPH_ENTITY_TYPE_NODE, "node"],
  [GraphEntityType.WORKFLOW_GRAPH_ENTITY_TYPE_NODE_GROUP, "node_group"],
  [GraphEntityType.WORKFLOW_GRAPH_ENTITY_TYPE_TRANSITION_GROUP, "transition_group"],
]);
