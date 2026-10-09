import type { ProjectTaskGroupCountsInput, TaskListInput, TaskMutationInput } from "./clientInputs";
import { ContractError, WorkflowTaskCreateSelectionError } from "./errors";
import { classifyResultFailure, create } from "@app/server-api-contract";
import {
  ProjectLabelService,
  type ProjectLabelCatalog as GeneratedCatalog,
  type ProjectLabel as GeneratedLabel,
} from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import {
  TaskLabelReadService,
  TaskReadService,
  LabelFilterSchema,
  NamedLabelFilterMode,
  type LabelFilter,
  type AssignedLabelIds,
} from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import {
  TaskLabelService,
  TaskLifecycleService,
  DependencyRole,
} from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { protobufRpcError, requireUnarySuccess } from "./protobufRpc";
import { throwWorkflowLabelFailure } from "./workflowLabelFailure";
import { throwTaskDependencyFailure } from "./taskDependencyFailure";
import type { RpcTransport } from "./transport";
import { canonicalTaskLabelFilter } from "./workflowLabels";
import { taskListPage, projectTaskGroupCounts } from "./clientTaskProjection";
import {
  projectTaskGroup,
  taskStatusKind,
  taskAttentionKind,
  taskSortField,
  taskSortDirection,
  taskCreateSelectionReason,
} from "./workflowProtoValues";
import type { CreatedTaskSummary } from "./models";
import type {
  ProjectLabel,
  ProjectLabelCatalog,
  ProjectTaskGroupCounts,
  TaskLabelAssignment,
  TaskLabelFilter,
  TaskListPage,
} from "./workflowLabels";

export function taskLabelFilterPayload(filter: TaskLabelFilter): LabelFilter {
  const canonical = canonicalTaskLabelFilter(filter);
  switch (canonical.kind) {
    case "none":
    case "unlabeled":
      return create(LabelFilterSchema, { filter: { case: canonical.kind, value: {} } });
    case "named":
      return create(LabelFilterSchema, {
        filter: {
          case: "named",
          value: {
            mode: canonical.mode === "any" ? NamedLabelFilterMode.ANY : NamedLabelFilterMode.ALL,
            labelIds: [...canonical.labelIDs],
            excludedLabelIds: [...canonical.excludedLabelIDs],
          },
        },
      });
  }
}

export async function listProjectLabels(
  transport: RpcTransport,
  projectID: string,
): Promise<ProjectLabelCatalog> {
  const method = ProjectLabelService.method.list;
  const result = await transport.callDescriptor(method, create(method.input, { projectId: projectID }));
  throwWorkflowLabelFailure(method, result.outcome);
  return projectLabelCatalog(requireUnarySuccess(method, result).catalog, projectID);
}

export async function createProjectLabel(
  transport: RpcTransport,
  projectID: string,
  name: string,
): Promise<ProjectLabel> {
  const method = ProjectLabelService.method.create;
  const result = await transport.callDescriptor(method, create(method.input, { projectId: projectID, name }));
  throwWorkflowLabelFailure(method, result.outcome);
  return projectLabel(requireUnarySuccess(method, result).label);
}

export async function reorderProjectLabels(
  transport: RpcTransport,
  projectID: string,
  labelIDs: readonly string[],
): Promise<ProjectLabelCatalog> {
  const method = ProjectLabelService.method.reorder;
  const result = await transport.callDescriptor(
    method,
    create(method.input, { projectId: projectID, labelIds: [...labelIDs] }),
  );
  throwWorkflowLabelFailure(method, result.outcome);
  return projectLabelCatalog(requireUnarySuccess(method, result).catalog, projectID);
}

export async function renameProjectLabel(
  transport: RpcTransport,
  projectID: string,
  labelID: string,
  name: string,
): Promise<ProjectLabel> {
  const method = ProjectLabelService.method.rename;
  const result = await transport.callDescriptor(
    method,
    create(method.input, { projectId: projectID, labelId: labelID, name }),
  );
  throwWorkflowLabelFailure(method, result.outcome);
  const label = projectLabel(requireUnarySuccess(method, result).label);
  if (label.id !== labelID) throw new ContractError("Renamed label does not match the request.");
  return label;
}

export async function deleteProjectLabel(
  transport: RpcTransport,
  projectID: string,
  labelID: string,
): Promise<string> {
  const method = ProjectLabelService.method.delete;
  const result = await transport.callDescriptor(
    method,
    create(method.input, { projectId: projectID, labelId: labelID }),
  );
  throwWorkflowLabelFailure(method, result.outcome);
  const success = requireUnarySuccess(method, result);
  if (success.labelId !== labelID) throw new ContractError("Deleted label does not match the request.");
  return success.labelId;
}

export async function getTaskLabels(transport: RpcTransport, taskID: string): Promise<TaskLabelAssignment> {
  const method = TaskLabelReadService.method.get;
  const result = await transport.callDescriptor(method, create(method.input, { taskId: taskID }));
  throwWorkflowLabelFailure(method, result.outcome);
  return taskLabelAssignment(requireUnarySuccess(method, result).assignment, taskID);
}

export async function updateTaskLabels(
  transport: RpcTransport,
  taskID: string,
  addLabelIDs: readonly string[],
  removeLabelIDs: readonly string[],
): Promise<TaskLabelAssignment> {
  const method = TaskLabelService.method.update;
  const result = await transport.callDescriptor(
    method,
    create(method.input, {
      taskId: taskID,
      addLabelIds: [...addLabelIDs],
      removeLabelIds: [...removeLabelIDs],
    }),
  );
  throwWorkflowLabelFailure(method, result.outcome);
  return taskLabelAssignment(requireUnarySuccess(method, result).assignment, taskID);
}

function projectLabel(value: GeneratedLabel | undefined): ProjectLabel {
  if (value === undefined) throw new ContractError("Project label is required.");
  return { id: value.id, name: value.name };
}

function projectLabelCatalog(value: GeneratedCatalog | undefined, projectID: string): ProjectLabelCatalog {
  if (value?.projectId !== projectID)
    throw new ContractError("Project label catalog does not match the request.");
  return { projectID: value.projectId, labels: value.labels.map(projectLabel) };
}

function taskLabelAssignment(value: AssignedLabelIds | undefined, taskID: string): TaskLabelAssignment {
  if (value?.taskId !== taskID) throw new ContractError("Task label assignment does not match the request.");
  return { taskID: value.taskId, labelIDs: value.labelIds };
}

export async function createTask(
  transport: RpcTransport,
  input: TaskMutationInput,
): Promise<CreatedTaskSummary> {
  const method = TaskLifecycleService.method.create;
  const result = await transport.callDescriptor(
    method,
    create(method.input, {
      projectId: input.projectID,
      workflowId: input.workflowID,
      title: input.title,
      body: input.body,
      sourceWorkspaceId: input.sourceWorkspaceID,
      labelIds: [...input.labelIDs],
      dependencyIntents: input.dependencyIntents.map((intent) => ({
        relatedTaskId: intent.relatedTaskID,
        newTaskRole: intent.newTaskRole === "blocker" ? DependencyRole.BLOCKER : DependencyRole.BLOCKED,
      })),
    }),
  );
  throwWorkflowLabelFailure(method, result.outcome);
  throwTaskDependencyFailure(method, result.outcome);
  if (
    result.outcome.case === "error" &&
    classifyResultFailure(method.output, result.outcome.value).kind !== "generic" &&
    result.outcome.value.detail.case === "createSelection"
  ) {
    const detail = result.outcome.value.detail.value;
    throw new WorkflowTaskCreateSelectionError(protobufRpcError(method, result.outcome.value), {
      reason: taskCreateSelectionReason.decode(detail.reason),
      projectID: detail.projectId,
      workflowID: detail.workflowId ?? null,
    });
  }
  const { task } = requireUnarySuccess(method, result);
  if (task === undefined) throw new ContractError("Created Task summary is required.");
  return { id: task.id, shortID: task.shortId, title: task.title, workflowID: task.workflowId };
}

export async function listTasks(transport: RpcTransport, input: TaskListInput): Promise<TaskListPage> {
  const method = TaskReadService.method.list;
  const result = await transport.callDescriptor(
    method,
    create(method.input, {
      projectId: input.projectID,
      workflowId: input.workflowID,
      group: input.group === undefined ? undefined : projectTaskGroup.encode(input.group),
      columnKeys: [...(input.columnKeys ?? [])],
      statusKinds: (input.statusKinds ?? []).map(taskStatusKind.encode),
      attentionKinds: (input.attentionKinds ?? []).map(taskAttentionKind.encode),
      labelFilter: taskLabelFilterPayload(input.labelFilter),
      sort: (input.sort ?? []).map((sort) => ({
        field: taskSortField.encode(sort.field),
        direction: taskSortDirection.encode(sort.direction),
      })),
      offset: input.offset ?? 0,
      limit: input.limit ?? 40,
    }),
  );
  throwWorkflowLabelFailure(method, result.outcome);
  return taskListPage(requireUnarySuccess(method, result));
}

export async function getProjectTaskGroupCounts(
  transport: RpcTransport,
  input: ProjectTaskGroupCountsInput,
): Promise<ProjectTaskGroupCounts> {
  const method = TaskReadService.method.getProjectGroupCounts;
  const result = await transport.callDescriptor(method, create(method.input, { projectId: input.projectID }));
  return projectTaskGroupCounts(requireUnarySuccess(method, result));
}
