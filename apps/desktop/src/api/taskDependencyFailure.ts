import { classifyResultFailure, type DescMethod } from "@app/server-api-contract";
import type { CreateResult, DependencyAddResult, DependencyRemoveResult } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { WorkflowTaskDependencyError } from "./errors";
import { protobufRpcError } from "./protobufRpc";
import { taskDependencyErrorReason } from "./workflowProtoValues";

type DependencyOutcome = (CreateResult | DependencyAddResult | DependencyRemoveResult)["outcome"];

export function throwTaskDependencyFailure(method: DescMethod, outcome: DependencyOutcome): void {
  if (outcome.case !== "error" || classifyResultFailure(method.output, outcome.value).kind === "generic") return;
  if (outcome.value.detail.case !== "dependency") return;
  const detail = outcome.value.detail.value;
  throw new WorkflowTaskDependencyError(protobufRpcError(method, outcome.value), {
    reason: taskDependencyErrorReason.decode(detail.reason),
    blockerTaskID: detail.blockerTaskId,
    blockedTaskID: detail.blockedTaskId,
    missingTaskID: detail.missingTaskId ?? null,
    currentCount: detail.currentCount ?? null,
    limit: detail.limit ?? null,
  });
}
