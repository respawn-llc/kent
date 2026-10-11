import type { TaskEditInput, TaskMoveInput, TaskResumeInput, TaskStartInput } from "./clientInputs";
import { create } from "@app/server-api-contract";
import {
  TaskLifecycleService,
  ExecutionTargetSelectionSchema,
} from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { requireUnarySuccess } from "./protobufRpc";
import { ContractError } from "./errors";
import { requireWorktreeSuccess } from "./clientWorktree";
import { taskCurrentNode, taskTargetSelectionRequired } from "./clientTaskProjection";
import { workflowExecutionTargetMode, taskMovePreviewBlocker } from "./workflowProtoValues";
import { throwTaskExecutionFailure } from "./taskExecutionFailure";
import type {
  TaskApproveResponse,
  TaskMoveResponse,
  TaskResumeResponse,
  TaskMovePreviewResponse,
  TaskStartResponse,
  WorkflowExecutionTargetSelection,
} from "./models";
import { newSetupOperationID } from "./setupOperationID";
import type { RpcTransport } from "./transport";

export async function updateTask(transport: RpcTransport, input: TaskEditInput): Promise<string> {
  const method = TaskLifecycleService.method.update;
  const result = await transport.callDescriptor(
    method,
    create(method.input, {
      taskId: input.taskID,
      title: input.title,
      body: input.body,
      sourceWorkspaceId: input.sourceWorkspaceID,
    }),
  );
  const { task } = requireUnarySuccess(method, result);
  if (task === undefined) throw new ContractError("Updated Task summary is required.");
  return task.id;
}

export async function deleteTask(transport: RpcTransport, taskID: string): Promise<void> {
  const method = TaskLifecycleService.method.delete;
  const result = await transport.callDescriptor(method, create(method.input, { taskId: taskID }));
  requireWorktreeSuccess(method, result);
}

export async function interruptTask(
  transport: RpcTransport,
  taskID: string,
  sessionID?: string,
): Promise<void> {
  const method = TaskLifecycleService.method.interrupt;
  const result = await transport.callDescriptor(
    method,
    create(method.input, { taskId: taskID, sessionId: sessionID }),
  );
  requireUnarySuccess(method, result);
}

export async function startTask(transport: RpcTransport, input: TaskStartInput): Promise<TaskStartResponse> {
  const setupOperationID = input.setupOperationID ?? newSetupOperationID();
  const method = TaskLifecycleService.method.start;
  const result = await transport.callDescriptor(
    method,
    create(method.input, {
      taskId: input.taskID,
      setupOperationId: setupOperationID.toJSONValue(),
      executionTarget: executionTargetPayload(input.executionTarget),
      proceedDespiteDependencies: input.proceedDespiteDependencies ?? false,
    }),
    { timeoutMs: null },
  );
  throwTaskExecutionFailure(method, result.outcome);
  const response = requireWorktreeSuccess(method, result);
  switch (response.outcome.case) {
    case "applied":
      return {
        outcome: "applied",
        applied: { currentNodes: response.outcome.value.currentNodes.map(taskCurrentNode) },
      };
    case "selectionRequired":
      return {
        outcome: "selection_required",
        selectionRequired: taskTargetSelectionRequired(response.outcome.value),
      };
    case "dependencyConfirmationRequired":
      return {
        outcome: "dependency_confirmation_required",
        unsatisfiedDependencyCount: response.outcome.value.unsatisfiedDependencyCount,
      };
    case undefined:
      throw new ContractError("Task Start outcome is required.");
  }
}

export async function moveTask(transport: RpcTransport, input: TaskMoveInput): Promise<TaskMoveResponse> {
  const method = TaskLifecycleService.method.move;
  const result = await transport.callDescriptor(
    method,
    create(method.input, {
      taskId: input.taskID,
      targetNodeId: input.targetNodeID,
      branchName: input.executionTarget?.mode === "none" ? undefined : input.branchName,
      transitionKey: input.transitionKey,
      values: Object.entries(input.values ?? {}).map(([nodeKey, outputs]) => ({
        nodeKey,
        outputs: Object.entries(outputs).map(([name, value]) => ({ name, value })),
      })),
      commentary: input.commentary,
      executionTarget: executionTargetPayload(input.executionTarget),
      proceedDespiteDependencies: input.proceedDespiteDependencies ?? false,
    }),
    { timeoutMs: null },
  );
  throwTaskExecutionFailure(method, result.outcome);
  const response = requireWorktreeSuccess(method, result);
  switch (response.outcome.case) {
    case "noOp":
      return {
        outcome: "no_op",
        noOp: {
          currentNodes: response.outcome.value.currentNodes.map(taskCurrentNode),
        },
      };
    case "applied":
      return {
        outcome: "applied",
        applied: {
          currentNodes: response.outcome.value.currentNodes.map(taskCurrentNode),
        },
      };
    case "selectionRequired":
      return {
        outcome: "selection_required",
        selectionRequired: taskTargetSelectionRequired(response.outcome.value),
      };
    case "dependencyConfirmationRequired":
      return {
        outcome: "dependency_confirmation_required",
        unsatisfiedDependencyCount: response.outcome.value.unsatisfiedDependencyCount,
      };
    case undefined:
      throw new ContractError("Task Move outcome is required.");
  }
}

export async function previewMoveTask(
  transport: RpcTransport,
  taskID: string,
  targetNodeID: string,
): Promise<TaskMovePreviewResponse> {
  const method = TaskLifecycleService.method.previewMove;
  const result = await transport.callDescriptor(
    method,
    create(method.input, { taskId: taskID, targetNodeId: targetNodeID }),
    { timeoutMs: null },
  );
  const response = requireUnarySuccess(method, result);
  switch (response.outcome.case) {
    case "noOp":
      return {
        outcome: "no_op",
        noOp: { currentNodes: response.outcome.value.currentNodes.map(taskCurrentNode) },
      };
    case "direct":
      return { outcome: "direct", direct: {} };
    case "blocked":
      return {
        outcome: "blocked",
        blocked: { reason: taskMovePreviewBlocker.decode(response.outcome.value.reason) },
      };
    case "transition":
      return {
        outcome: "transition",
        transition: {
          choices: response.outcome.value.choices.map((choice) => ({
            transitionKey: choice.transitionKey,
            label: choice.label,
            sourceNodeDisplayName: choice.sourceNodeDisplayName,
            requiredValues: choice.requiredValues.map((value) => ({
              nodeKey: value.nodeKey,
              outputName: value.outputName,
              description: value.description ?? null,
              resolvedValue: value.resolvedValue ?? null,
            })),
          })),
        },
      };
    case undefined:
      throw new ContractError("Manual Move preview outcome is required.");
  }
}

export async function approveApproval(
  transport: RpcTransport,
  approvalID: string,
): Promise<TaskApproveResponse> {
  const method = TaskLifecycleService.method.approve;
  const result = await transport.callDescriptor(method, create(method.input, { approvalId: approvalID }), {
    timeoutMs: null,
  });
  throwTaskExecutionFailure(method, result.outcome);
  const response = requireUnarySuccess(method, result);
  switch (response.outcome.case) {
    case "applied":
      return {
        outcome: "applied",
        applied: {
          taskID: response.outcome.value.taskId,
          currentNodes: response.outcome.value.currentNodes.map(taskCurrentNode),
        },
      };
    case "selectionRequired":
      return {
        outcome: "selection_required",
        selectionRequired: taskTargetSelectionRequired(response.outcome.value),
      };
    case undefined:
      throw new ContractError("Task Approval outcome is required.");
  }
}

export async function resumeTask(
  transport: RpcTransport,
  input: TaskResumeInput,
): Promise<TaskResumeResponse> {
  const setupOperationID = input.setupOperationID ?? newSetupOperationID();
  const method = TaskLifecycleService.method.resume;
  const result = await transport.callDescriptor(
    method,
    create(method.input, {
      taskId: input.taskID,
      setupOperationId: setupOperationID.toJSONValue(),
      branchName: input.executionTarget?.mode === "none" ? undefined : input.branchName,
      executionTarget: executionTargetPayload(input.executionTarget),
    }),
    { timeoutMs: null },
  );
  throwTaskExecutionFailure(method, result.outcome);
  const response = requireWorktreeSuccess(method, result);
  switch (response.outcome.case) {
    case "applied":
      return {
        outcome: "applied",
        applied: { currentNodes: response.outcome.value.currentNodes.map(taskCurrentNode) },
      };
    case "noOp":
      return {
        outcome: "no_op",
        noOp: { currentNodes: response.outcome.value.currentNodes.map(taskCurrentNode) },
      };
    case "selectionRequired":
      return {
        outcome: "selection_required",
        selectionRequired: taskTargetSelectionRequired(response.outcome.value),
      };
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
