import { classifyResultFailure, type DescMethod } from "@app/server-api-contract";
import type {
  StartResult,
  ResumeResult,
  ApproveResult,
  MoveResult,
} from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { TaskExecutionError } from "./errors";
import { protobufRpcError } from "./protobufRpc";

type ExecutionOutcome = (StartResult | ResumeResult | ApproveResult | MoveResult)["outcome"];
export type ExecutionDetail = Extract<ExecutionOutcome, { case: "error" }>["value"]["detail"];

export function throwTaskExecutionFailure(method: DescMethod, outcome: ExecutionOutcome): void {
  if (outcome.case !== "error") return;
  if (classifyResultFailure(method.output, outcome.value).kind === "generic") return;
  if (
    outcome.value.detail.case === "initialBranch" ||
    outcome.value.detail.case === "executionTargetResolution" ||
    outcome.value.detail.case === "contextSelectionRequired"
  ) {
    throw new TaskExecutionError(protobufRpcError(method, outcome.value), outcome.value.detail);
  }
}
