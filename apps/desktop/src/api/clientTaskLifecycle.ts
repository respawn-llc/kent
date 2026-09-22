import type { TaskEditInput, TaskMoveInput, TaskResumeInput, TaskStartInput } from "./clientInputs";
import { create } from "@app/server-api-contract";
import { TaskLifecycleService, ExecutionTargetSelectionSchema } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { requireUnarySuccess } from "./protobufRpc";
import { ContractError } from "./errors";
import { requireWorktreeSuccess } from "./clientWorktree";
import { taskCurrentNode, taskTargetSelectionRequired } from "./clientTaskProjection";
import { workflowExecutionTargetMode } from "./workflowProtoValues";
import { throwTaskExecutionFailure } from "./taskExecutionFailure";
import { parseRpcResponse } from "./clientParse";
import { compactJsonObject } from "./json";
import type {
  TaskApproveResponse,
  TaskMoveResponse,
  TaskResumeResponse,
  TaskMovePreviewResponse,
  TaskStartResponse,
  WorkflowExecutionTargetSelection,
} from "./models";
import {
  taskApproveResponseSchema,
  taskMoveResponseSchema,
  taskMovePreviewResponseSchema,
} from "./schemas/workflowBoard";
import { newSetupOperationID } from "./setupOperationID";
import type { DescriptorRpcTransport, RpcTransport } from "./transport";

export async function updateTask(transport: DescriptorRpcTransport, input: TaskEditInput): Promise<string> {
  const method = TaskLifecycleService.method.update;
  const result = await transport.callDescriptor(method, create(method.input, {
    taskId: input.taskID,
    title: input.title,
    body: input.body,
    sourceWorkspaceId: input.sourceWorkspaceID,
  }));
  const { task } = requireUnarySuccess(method, result);
  if (task === undefined) throw new ContractError("Updated Task summary is required.");
  return task.id;
}

export async function deleteTask(transport: DescriptorRpcTransport, taskID: string): Promise<void> {
  const method = TaskLifecycleService.method.delete;
  const result = await transport.callDescriptor(method, create(method.input, { taskId: taskID }));
  requireWorktreeSuccess(method, result);
}

export async function startTask(transport: DescriptorRpcTransport, input: TaskStartInput): Promise<TaskStartResponse> {
  const setupOperationID = input.setupOperationID ?? newSetupOperationID();
  const method = TaskLifecycleService.method.start;
  const result = await transport.callDescriptor(method, create(method.input, {
    taskId: input.taskID,
    setupOperationId: setupOperationID.toJSONValue(),
    executionTarget: executionTargetPayload(input.executionTarget),
    proceedDespiteDependencies: input.proceedDespiteDependencies ?? false,
  }), { timeoutMs: null });
  throwTaskExecutionFailure(method, result.outcome);
  const response = requireWorktreeSuccess(method, result);
  switch (response.outcome.case) {
    case "applied":
      return { outcome: "applied", applied: { currentNodes: response.outcome.value.currentNodes.map(taskCurrentNode) } };
    case "selectionRequired":
      return { outcome: "selection_required", selectionRequired: taskTargetSelectionRequired(response.outcome.value) };
    case "dependencyConfirmationRequired":
      return { outcome: "dependency_confirmation_required", unsatisfiedDependencyCount: response.outcome.value.unsatisfiedDependencyCount };
    case undefined:
      throw new ContractError("Task Start outcome is required.");
  }
}

export async function moveTask(transport: RpcTransport, input: TaskMoveInput): Promise<TaskMoveResponse> {
  const response = parseRpcResponse(
    "workflow.task.move",
    taskMoveResponseSchema,
    await transport.call(
      "workflow.task.move",
      compactJsonObject({
        task_id: input.taskID,
        target_node_id: input.targetNodeID,
        branch_name: input.executionTarget?.mode === "none" ? undefined : input.branchName,
        transition_key: input.transitionKey,
        values: input.values,
        commentary: input.commentary,
        execution_target: executionTargetPayload(input.executionTarget),
        proceed_despite_dependencies: input.proceedDespiteDependencies ?? false,
      }),
      { timeoutMs: null },
    ),
  );
  return response;
}

export async function previewMoveTask(
  transport: RpcTransport,
  taskID: string,
  targetNodeID: string,
): Promise<TaskMovePreviewResponse> {
  return parseRpcResponse(
    "workflow.task.move.preview",
    taskMovePreviewResponseSchema,
    await transport.call(
      "workflow.task.move.preview",
      { task_id: taskID, target_node_id: targetNodeID },
      { timeoutMs: null },
    ),
  );
}

export async function approveApproval(
  transport: RpcTransport,
  approvalID: string,
): Promise<TaskApproveResponse> {
  return parseRpcResponse(
    "workflow.task.approve",
    taskApproveResponseSchema,
    await transport.call("workflow.task.approve", { approval_id: approvalID }, { timeoutMs: null }),
  );
}

export async function resumeTask(
  transport: DescriptorRpcTransport,
  input: TaskResumeInput,
): Promise<TaskResumeResponse> {
  const setupOperationID = input.setupOperationID ?? newSetupOperationID();
  const method = TaskLifecycleService.method.resume;
  const result = await transport.callDescriptor(method, create(method.input, {
    taskId: input.taskID,
    setupOperationId: setupOperationID.toJSONValue(),
    branchName: input.executionTarget?.mode === "none" ? undefined : input.branchName,
    executionTarget: executionTargetPayload(input.executionTarget),
  }), { timeoutMs: null });
  throwTaskExecutionFailure(method, result.outcome);
  const response = requireWorktreeSuccess(method, result);
  switch (response.outcome.case) {
    case "applied":
      return { outcome: "applied", applied: { currentNodes: response.outcome.value.currentNodes.map(taskCurrentNode) } };
    case "noOp":
      return { outcome: "no_op", noOp: { currentNodes: response.outcome.value.currentNodes.map(taskCurrentNode) } };
    case "selectionRequired":
      return { outcome: "selection_required", selectionRequired: taskTargetSelectionRequired(response.outcome.value) };
    case undefined:
      throw new ContractError("Task Resume outcome is required.");
  }
}

function executionTargetPayload(selection: WorkflowExecutionTargetSelection | undefined) {
  if (selection === undefined) {
    return undefined;
  }
  return create(ExecutionTargetSelectionSchema, {
    mode: workflowExecutionTargetMode.encode(selection.mode),
    customRef: selection.customRef ?? undefined,
  });
}
