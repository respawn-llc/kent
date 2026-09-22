import type * as pb from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import type { AttentionCurrentNode } from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import type { SelectionRequired } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { ContractError } from "./errors";
import { timestampMillis } from "./clientTime";
import type {
  TaskStatus,
  TaskDependencyProgress,
  TaskActions,
  WorkspaceSummary,
  WorkflowBoard,
  BoardNodeCardsPage,
  WorkflowPickerItem,
  TaskCurrentNode,
  TaskDetail,
  TaskDependencies,
  TaskDependencyItem,
  TaskDependencyAddAvailability,
} from "./models";
import { projectAvailability } from "./clientProject";
import { workflowValidationError } from "./clientWorkflowProjection";
import type { TaskListPage, ProjectTaskGroupCounts } from "./workflowLabels";
import {
  taskStatusKind,
  taskNativeState,
  taskAttentionKind,
  taskCardinality,
  projectTaskGroup,
  workflowNodeKind,
  workflowExecutionTargetMode,
  taskDependencyDirection,
  taskDependencySatisfaction,
  taskExecutionProvenance,
  taskOriginalTargetCause,
  taskUnavailableTargetCause,
} from "./workflowProtoValues";
import type { WorkflowExecutionTarget, WorkflowExecutionTargetSelectionRequirement } from "./workflowExecutionTarget";

export function taskStatus(value: pb.TaskStatus | undefined): TaskStatus {
  if (value === undefined) throw new ContractError("Task status is required.");
  return {
    kind: taskStatusKind.decode(value.kind),
    nativeState: taskNativeState.decode(value.nativeState),
    nodeIDs: value.nodeIds,
    attentionTypes: value.attentionTypes.map(taskAttentionKind.decode),
  };
}

export function taskDependencyProgress(value: pb.DependencyProgress | undefined): TaskDependencyProgress | null {
  return value === undefined
    ? null
    : { satisfiedCount: value.satisfiedCount, totalCount: value.totalCount };
}

export function taskListPage(value: pb.ListSuccess): TaskListPage {
  if (value.scope === undefined || value.generatedAt === undefined) {
    throw new ContractError("Task list scope and generation time are required.");
  }
  return {
    scope: { projectID: value.scope.projectId, workflowID: value.scope.workflowId ?? null },
    matchingWorkflowCardinality: taskCardinality.decode(value.matchingWorkflowCardinality),
    nextOffset: value.nextOffset ?? null,
    generatedAt: timestampMillis(value.generatedAt),
    tasks: value.tasks.map((task) => {
      if (task.createdAt === undefined || task.updatedAt === undefined) {
        throw new ContractError("Task creation and update times are required.");
      }
      return {
        id: task.taskId,
        shortID: task.shortId,
        workflowID: task.workflowId,
        workflowName: task.workflowName ?? null,
        title: task.title,
        createdAt: timestampMillis(task.createdAt),
        updatedAt: timestampMillis(task.updatedAt),
        columnKeys: task.columnKeys?.values ?? null,
        status: taskStatus(task.status),
        labels: task.labels.map((label) => ({ id: label.id, name: label.name })),
        dependencyProgress: taskDependencyProgress(task.dependencyProgress),
      };
    }),
  };
}

export function projectTaskGroupCounts(value: pb.ProjectTaskGroupCountsSuccess): ProjectTaskGroupCounts {
  if (value.counts === undefined || value.generatedAt === undefined) {
    throw new ContractError("Task group counts and generation time are required.");
  }
  return {
    projectID: value.projectId,
    definitions: value.definitions.map((definition) => ({
      group: projectTaskGroup.decode(definition.group),
      statusKinds: definition.statusKinds.map(taskStatusKind.decode),
    })),
    counts: { active: value.counts.active, backlog: value.counts.backlog, done: value.counts.done },
    generatedAt: timestampMillis(value.generatedAt),
  };
}

export function taskActions(value: pb.TaskActions | undefined): TaskActions {
  if (value === undefined) throw new ContractError("Task actions are required.");
  return {
    canStart: value.canStart,
    canInterrupt: value.canInterrupt,
    canResume: value.canResume,
    canDelete: value.canDelete,
  };
}

export function taskSourceWorkspace(value: pb.TaskSourceWorkspace | undefined): WorkspaceSummary {
  if (value === undefined) throw new ContractError("Task source Workspace is required.");
  return {
    id: value.workspaceId,
    name: value.displayName,
    rootPath: value.rootPath,
    availability: projectAvailability(value.availability),
    isPrimary: value.isPrimary,
    updatedAt: value.updatedAt === undefined ? null : timestampMillis(value.updatedAt),
  };
}

function workflowPickerItem(value: pb.WorkflowPickerItem): WorkflowPickerItem {
  return {
    id: value.workflowId,
    name: value.displayName,
    description: value.description,
    version: Number(value.version),
    isProjectDefault: value.isProjectDefault,
    validForTaskCreation: value.validForTaskCreation,
    validationErrors: value.validationErrors.map(workflowValidationError),
  };
}

export function workflowBoard(value: pb.Board | undefined): WorkflowBoard {
  if (value?.project === undefined || value.generatedAt === undefined) {
    throw new ContractError("Board Project and generation time are required.");
  }
  const columns = value.columns.map((column) => {
    if (column.node === undefined) throw new ContractError("Board column Node is required.");
    return {
      id: column.node.nodeId,
      key: column.node.key,
      kind: workflowNodeKind.decode(column.node.kind),
      name: column.node.displayName,
      assigneeRole: column.node.assigneeRole ?? null,
      outputFields: column.node.outputFields.map((field) => ({ name: field.name, description: field.description })),
      groupID: column.groupId ?? null,
      sortOrder: column.sortOrder,
      isBacklog: column.isBacklog,
      isDone: column.isDone,
      taskCount: column.taskCount,
    };
  }).filter((column) => column.kind !== "join");
  const visibleNodeIDs = new Set(columns.map((column) => column.id));
  return {
    projectID: value.projectId,
    projectKey: value.project.projectKey,
    projectName: value.project.displayName,
    defaultWorkspaceID: value.project.defaultWorkspaceId,
    attachedWorkspaceCount: value.project.attachedWorkspaceCount,
    selectedWorkflow: value.selectedWorkflow === undefined ? null : workflowPickerItem(value.selectedWorkflow),
    workflows: value.workflows.map(workflowPickerItem),
    groups: value.groups.map((group) => ({
      id: group.groupId,
      key: group.key,
      name: group.displayName,
      sortOrder: group.sortOrder,
      nodeIDs: group.nodeIds.filter((nodeID) => visibleNodeIDs.has(nodeID)),
    })).filter((group) => group.nodeIDs.length > 0),
    columns,
    generatedAt: timestampMillis(value.generatedAt),
  };
}

export function boardNodeCardsPage(value: pb.BoardNodeCardsListSuccess): BoardNodeCardsPage {
  if (value.generatedAt === undefined) throw new ContractError("Board card page generation time is required.");
  return {
    projectID: value.projectId,
    workflowID: value.workflowId,
    nodeID: value.nodeId,
    nextOffset: value.nextOffset ?? null,
    generatedAt: timestampMillis(value.generatedAt),
    cards: value.cards.map((card) => {
      if (card.preview === undefined || card.updatedAt === undefined) {
        throw new ContractError("Task card preview and update time are required.");
      }
      return {
        id: card.taskId,
        shortID: card.shortId,
        title: card.title,
        preview: { markdown: card.preview.markdown, truncated: card.preview.truncated },
        workflowID: card.workflowId,
        activeNodeIDs: card.activeNodeIds,
        sourceWorkspace: taskSourceWorkspace(card.sourceWorkspace),
        status: taskStatus(card.status),
        actions: taskActions(card.actions),
        labelIDs: card.labelIds,
        dependencyProgress: taskDependencyProgress(card.dependencyProgress),
        updatedAt: timestampMillis(card.updatedAt),
      };
    }),
  };
}

export function taskCurrentNode(value: AttentionCurrentNode): TaskCurrentNode {
  return {
    nodeID: value.nodeId,
    transitionBranchKey: value.transitionBranchKey ?? null,
    sessionID: value.sessionId ?? null,
    effectiveAssignee: value.effectiveAssignee ?? null,
    effectiveThinking: value.effectiveThinking ?? null,
  };
}

export function taskTargetSelectionRequired(value: SelectionRequired): WorkflowExecutionTargetSelectionRequirement {
  switch (value.reason.case) {
    case "policyRequiresSelection":
      return { reason: "policy_requires_selection" };
    case "originalTargetUnavailable":
      return { reason: "original_target_unavailable", originalTargetCause: taskOriginalTargetCause.decode(value.reason.value.cause) };
    case "configuredTargetUnavailable": {
      const facts = value.reason.value;
      const mode = workflowExecutionTargetMode.decode(facts.mode);
      if (mode === "none" || mode === "ask_on_first_execution") throw new ContractError("Configured target must be managed.");
      return {
        reason: "configured_target_unavailable",
        configuredTarget: { mode, requestedRef: facts.requestedRef ?? null },
        unavailableCause: taskUnavailableTargetCause.decode(facts.cause),
      };
    }
    case undefined:
      throw new ContractError("Execution target selection reason is required.");
  }
}

export function taskExecutionTarget(value: pb.ExecutionTarget | undefined): WorkflowExecutionTarget | null {
  if (value === undefined) return null;
  const mode = workflowExecutionTargetMode.decode(value.mode);
  if (mode === "none") {
    return { mode, requestedRef: null, resolvedRef: null, commitOID: null, provenance: "resolved" };
  }
  if (mode === "ask_on_first_execution" || value.requestedRef === undefined || value.commitOid === undefined) {
    throw new ContractError("Task execution target is unresolved.");
  }
  return {
    mode,
    requestedRef: value.requestedRef,
    resolvedRef: value.resolvedRef ?? null,
    commitOID: value.commitOid,
    provenance: taskExecutionProvenance.decode(value.provenance),
  };
}

export function taskDependencyItem(value: pb.DependencyItem): TaskDependencyItem {
  return {
    taskID: value.taskId,
    shortID: value.shortId,
    title: value.title,
    workflowID: value.workflowId,
    status: taskStatus(value.status),
    satisfaction: value.satisfaction === undefined ? null : taskDependencySatisfaction.decode(value.satisfaction),
  };
}

function taskDependencyAvailability(value: pb.DependencyAddAvailability | undefined): TaskDependencyAddAvailability {
  const availability = value?.availability;
  switch (availability?.case) {
    case "available":
      return { kind: "available", remainingCapacity: availability.value.remainingCapacity };
    case "limitReached":
      return { kind: "limit_reached" };
    default:
      throw new ContractError("Task dependency availability is required.");
  }
}

export function taskDependencies(value: pb.TaskDependencies | undefined): TaskDependencies {
  if (value === undefined) throw new ContractError("Task dependencies are required.");
  return {
    blockerCount: value.blockerCount,
    unsatisfiedBlockerCount: value.unsatisfiedBlockerCount,
    directlyBlockedTaskCount: value.directlyBlockedTaskCount,
    directions: value.directions.map((direction) => ({
      direction: taskDependencyDirection.decode(direction.direction),
      totalCount: direction.totalCount,
      unsatisfiedCount: direction.unsatisfiedCount ?? null,
      items: direction.items.map(taskDependencyItem),
      addAvailability: taskDependencyAvailability(direction.addAvailability),
    })),
  };
}

export function taskDetail(value: pb.TaskDetail | undefined): TaskDetail {
  if (value?.summary?.createdAt === undefined || value.summary.updatedAt === undefined ||
      value.project === undefined || value.workflow === undefined) {
    throw new ContractError("Task summary, times, Project, and Workflow are required.");
  }
  return {
    id: value.summary.id,
    shortID: value.summary.shortId,
    projectID: value.summary.projectId,
    projectName: value.project.displayName,
    workflowID: value.summary.workflowId,
    workflowName: value.workflow.displayName,
    workflowVersion: Number(value.workflow.version),
    title: value.summary.title,
    body: value.body,
    sourceURL: value.sourceUrl ?? null,
    sourceWorkspace: taskSourceWorkspace(value.sourceWorkspace),
    status: taskStatus(value.status),
    actions: taskActions(value.actions),
    labelIDs: value.labelIds,
    attentionCount: value.attentionCount,
    dependencies: taskDependencies(value.dependencies),
    executionTarget: taskExecutionTarget(value.executionTarget),
    worktreePath: value.worktreePath ?? null,
    currentNodes: value.currentNodes.map(taskCurrentNode),
    liveSessions: value.liveSessions.map((session) => ({
      sessionID: session.sessionId,
      sessionName: session.sessionName ?? null,
      nodeDisplayName: session.nodeDisplayName,
    })),
    currentScripts: value.currentScripts.map((script) => {
      if (script.currentNode === undefined) throw new ContractError("Current Script Node is required.");
      return {
        currentNode: { nodeID: script.currentNode.nodeId, transitionBranchKey: script.currentNode.transitionBranchKey ?? null, sessionID: null },
        path: script.path,
      };
    }),
    retainedSessionCount: value.retainedSessionCount,
    createdAt: timestampMillis(value.summary.createdAt),
    updatedAt: timestampMillis(value.summary.updatedAt),
    done: value.summary.done,
  };
}
