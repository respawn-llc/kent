import type { ProjectTaskGroupCountsInput, TaskListInput, TaskMutationInput } from "./clientInputs";
import { parseRpcResponse as parse } from "./clientParse";
import { ContractError, decodeWorkflowTaskCreateSelectionError } from "./errors";
import { create } from "@app/server-api-contract";
import {
  ProjectLabelService,
  type ProjectLabelCatalog as GeneratedCatalog,
  type ProjectLabel as GeneratedLabel,
} from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import {
  TaskLabelReadService,
  type AssignedLabelIds,
} from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import { TaskLabelService } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { requireUnarySuccess } from "./protobufRpc";
import { throwWorkflowLabelFailure } from "./workflowLabelFailure";
import { compactJsonObject, type JsonObject } from "./json";
import {
  projectTaskGroupCountsSchema,
  taskCreateResponseSchema,
  taskListPageSchema,
} from "./schemas/workflowBoard";
import { workflowIDSchema } from "./schemas/workflowID";
import type { DescriptorRpcTransport, RpcTransport } from "./transport";
import { canonicalTaskLabelFilter } from "./workflowLabels";
import type { CreatedTaskSummary } from "./models";
import type {
  ProjectLabel,
  ProjectLabelCatalog,
  ProjectTaskGroupCounts,
  TaskLabelAssignment,
  TaskLabelFilter,
  TaskListPage,
} from "./workflowLabels";

export function taskLabelFilterPayload(filter: TaskLabelFilter): JsonObject {
  const canonical = canonicalTaskLabelFilter(filter);
  switch (canonical.kind) {
    case "none":
    case "unlabeled":
      return { kind: canonical.kind };
    case "named":
      return {
        kind: canonical.kind,
        named: compactJsonObject({
          mode: canonical.mode,
          label_ids: canonical.labelIDs,
          excluded_label_ids:
            canonical.excludedLabelIDs.length === 0 ? undefined : canonical.excludedLabelIDs,
        }),
      };
  }
}

export async function listProjectLabels(
  transport: DescriptorRpcTransport,
  projectID: string,
): Promise<ProjectLabelCatalog> {
  const method = ProjectLabelService.method.list;
  const result = await transport.callDescriptor(method, create(method.input, { projectId: projectID }));
  throwWorkflowLabelFailure(method, result.outcome);
  return projectLabelCatalog(requireUnarySuccess(method, result).catalog, projectID);
}

export async function createProjectLabel(
  transport: DescriptorRpcTransport,
  projectID: string,
  name: string,
): Promise<ProjectLabel> {
  const method = ProjectLabelService.method.create;
  const result = await transport.callDescriptor(method, create(method.input, { projectId: projectID, name }));
  throwWorkflowLabelFailure(method, result.outcome);
  return projectLabel(requireUnarySuccess(method, result).label);
}

export async function reorderProjectLabels(
  transport: DescriptorRpcTransport,
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
  transport: DescriptorRpcTransport,
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
  transport: DescriptorRpcTransport,
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

export async function getTaskLabels(
  transport: DescriptorRpcTransport,
  taskID: string,
): Promise<TaskLabelAssignment> {
  const method = TaskLabelReadService.method.get;
  const result = await transport.callDescriptor(method, create(method.input, { taskId: taskID }));
  throwWorkflowLabelFailure(method, result.outcome);
  return taskLabelAssignment(requireUnarySuccess(method, result).assignment, taskID);
}

export async function updateTaskLabels(
  transport: DescriptorRpcTransport,
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
  try {
    const response = parse(
      "workflow.task.create",
      taskCreateResponseSchema,
      await transport.call(
        "workflow.task.create",
        compactJsonObject({
          project_id: input.projectID,
          workflow_id: input.workflowID === undefined ? undefined : workflowIDSchema.parse(input.workflowID),
          title: input.title,
          body: input.body,
          source_workspace_id: input.sourceWorkspaceID,
          label_ids: input.labelIDs,
          dependency_intents: input.dependencyIntents.map((intent) => ({
            related_task_id: intent.relatedTaskID,
            new_task_role: intent.newTaskRole,
          })),
        }),
      ),
    );
    return response;
  } catch (error) {
    throw decodeWorkflowTaskCreateSelectionError(error) ?? error;
  }
}

export async function listTasks(transport: RpcTransport, input: TaskListInput): Promise<TaskListPage> {
  return parse(
    "workflow.task.list",
    taskListPageSchema,
    await transport.call(
      "workflow.task.list",
      compactJsonObject({
        project_id: input.projectID,
        workflow_id: input.workflowID === undefined ? undefined : workflowIDSchema.parse(input.workflowID),
        group: input.group,
        column_keys: input.columnKeys ?? [],
        status_kinds: input.statusKinds ?? [],
        attention_kinds: input.attentionKinds ?? [],
        label_filter: taskLabelFilterPayload(input.labelFilter),
        sort: input.sort ?? [],
        offset: input.offset ?? 0,
        limit: input.limit ?? 40,
      }),
    ),
  );
}

export async function getProjectTaskGroupCounts(
  transport: RpcTransport,
  input: ProjectTaskGroupCountsInput,
): Promise<ProjectTaskGroupCounts> {
  return parse(
    "workflow.task.groupCounts",
    projectTaskGroupCountsSchema,
    await transport.call("workflow.task.groupCounts", {
      project_id: input.projectID,
    }),
  );
}
