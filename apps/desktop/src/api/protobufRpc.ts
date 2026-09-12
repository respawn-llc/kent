import {
  classifyResultFailure,
  operationName,
  type DescMethod,
  type Message,
  type MessageShape,
} from "@app/server-api-contract";

import { ContractError, RpcError, TransportError } from "./errors";
import { rpcErrorCodes } from "./rpcErrorCodes";
import type { StreamCompletion } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";

export function streamCompletionFailure(value: StreamCompletion): TransportError | undefined {
  return value.code === undefined
    ? undefined
    : new TransportError(`Subscription completed with code ${value.code.toString()}: ${value.message ?? ""}`);
}

type RpcFailure = Message & Readonly<{ code: string }>;
type RpcResult = Readonly<{
  outcome:
    | Readonly<{ case: "success"; value: Message }>
    | Readonly<{ case: "error"; value: RpcFailure }>
    | Readonly<{ case: undefined; value?: undefined }>;
}>;
type MethodOutcome<Method extends DescMethod> =
  MessageShape<Method["output"]> extends Readonly<{ outcome: infer Outcome }> ? Outcome : never;
type MethodSuccess<Method extends DescMethod> =
  Extract<MethodOutcome<Method>, Readonly<{ case: "success" }>> extends Readonly<{
    value: infer Success;
  }>
    ? Success
    : never;

export function requireUnarySuccess<Method extends DescMethod>(
  method: Method,
  result: RpcResult,
): MethodSuccess<Method>;
export function requireUnarySuccess(method: DescMethod, result: RpcResult): Message {
  const { outcome } = result;
  switch (outcome.case) {
    case "success":
      return outcome.value;
    case "error":
      throw protobufRpcError(method, outcome.value);
    case undefined:
      throw new ContractError(`${operationName(method)} returned no outcome.`);
  }
}

export function protobufRpcError(method: DescMethod, failure: RpcFailure): RpcError {
  const operation = operationName(method);
  try {
    classifyResultFailure(method.output, failure);
  } catch {
    throw new ContractError(`${operation} returned a malformed error outcome.`);
  }
  return new RpcError({
    code: rpcErrorCode(failure.code),
    message: `${operation} failed with code ${failure.code}.`,
    method: operation,
    data: failure,
  });
}

function rpcErrorCode(code: string): number {
  switch (code) {
    case "workspace_not_registered":
      return rpcErrorCodes.workspaceNotRegistered;
    case "project_not_found":
      return rpcErrorCodes.projectNotFound;
    case "project_unavailable":
      return rpcErrorCodes.projectUnavailable;
    case "auth_required":
      return rpcErrorCodes.authRequired;
    case "server_not_ready":
      return rpcErrorCodes.serverNotReady;
    case "workspace_path_identity":
      return rpcErrorCodes.workspacePathIdentity;
    case "workspace_detach_conflict":
      return rpcErrorCodes.workspaceDetachConflict;
    case "workspace_mutation_failed":
      return rpcErrorCodes.workspaceMutationFailed;
    case "pending_work_not_pending":
      return rpcErrorCodes.pendingWorkNotPending;
    default:
      return rpcErrorCodes.internal;
  }
}
