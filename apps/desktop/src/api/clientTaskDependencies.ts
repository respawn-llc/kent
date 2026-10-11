import { create } from "@app/server-api-contract";
import {
  TaskDependencyService,
  type DependencyMutationSuccess,
} from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { requireUnarySuccess } from "./protobufRpc";
import { taskDependencyItem } from "./clientTaskProjection";
import { taskDependencyDirection, taskDependencyMutationOutcome } from "./workflowProtoValues";
import { throwTaskDependencyFailure } from "./taskDependencyFailure";
import type {
  TaskDependencyDirection,
  TaskDependencyListResponse,
  TaskDependencyMutationResponse,
} from "./models";
import type { RpcTransport } from "./transport";

function dependencyMutation(value: DependencyMutationSuccess): TaskDependencyMutationResponse {
  return {
    outcome: taskDependencyMutationOutcome.decode(value.outcome),
    blockerTaskID: value.blockerTaskId,
    blockerShortID: value.blockerShortId,
    blockedTaskID: value.blockedTaskId,
    blockedShortID: value.blockedShortId,
  };
}

export async function addTaskDependency(
  transport: RpcTransport,
  blockerTaskID: string,
  blockedTaskID: string,
): Promise<TaskDependencyMutationResponse> {
  const method = TaskDependencyService.method.add;
  const result = await transport.callDescriptor(
    method,
    create(method.input, { blockerTaskId: blockerTaskID, blockedTaskId: blockedTaskID }),
  );
  throwTaskDependencyFailure(method, result.outcome);
  return dependencyMutation(requireUnarySuccess(method, result));
}

export async function removeTaskDependency(
  transport: RpcTransport,
  blockerTaskID: string,
  blockedTaskID: string,
): Promise<TaskDependencyMutationResponse> {
  const method = TaskDependencyService.method.remove;
  const result = await transport.callDescriptor(
    method,
    create(method.input, { blockerTaskId: blockerTaskID, blockedTaskId: blockedTaskID }),
  );
  throwTaskDependencyFailure(method, result.outcome);
  return dependencyMutation(requireUnarySuccess(method, result));
}

export async function listTaskDependencies(
  transport: RpcTransport,
  taskID: string,
  direction?: TaskDependencyDirection,
): Promise<TaskDependencyListResponse> {
  const method = TaskDependencyService.method.list;
  const result = await transport.callDescriptor(
    method,
    create(method.input, {
      taskId: taskID,
      direction: direction === undefined ? undefined : taskDependencyDirection.encode(direction),
    }),
  );
  const response = requireUnarySuccess(method, result);
  return {
    taskID: response.taskId,
    shortID: response.shortId,
    directions: response.directions.map((direction) => ({
      direction: taskDependencyDirection.decode(direction.direction),
      totalCount: direction.totalCount,
      unsatisfiedCount: direction.unsatisfiedCount ?? null,
      items: direction.items.map(taskDependencyItem),
    })),
  };
}
