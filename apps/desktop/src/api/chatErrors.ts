import type { DescMethod, Message, MessageShape } from "@app/server-api-contract";
import type { ChatOperationError as WireChatOperationError } from "@app/server-api-contract/gen/kent/api/chat/chat_pb";
import { AgentPreparationCategory } from "@app/server-api-contract/gen/kent/api/chat_settings/chat_settings_pb";
import { GoalService } from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import type { ReadError } from "@app/server-api-contract/gen/kent/api/chat_settings/chat_settings_pb";
import type {
  GoalSetError,
  ListPendingWorkError,
  LiveStopError,
  RemovePendingWorkError,
} from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import type {
  SessionResolveTransitionError,
  SessionInitialInputError,
  SessionPersistInputDraftError,
} from "@app/server-api-contract/gen/kent/api/session_launch/session_lifecycle_pb";

import { ContractError, RpcError } from "./errors";
import { protobufRpcError } from "./protobufRpc";

export type ChatRuntimeUnavailableError = Readonly<{ kind: "runtime_unavailable"; sessionID: string }>;
export type ChatInternalFailureError = Readonly<{
  kind: "internal_failure";
  operation: string | null;
  cause: string | null;
}>;
export type ChatKnownErrorDetail = ChatRuntimeUnavailableError | ChatInternalFailureError;

export type ChatError =
  | Readonly<{ kind: "session_not_found"; sessionID: string }>
  | Readonly<{ kind: "workspace_not_registered" }>
  | Readonly<{
      kind: "agent_preparation";
      agent: string;
      category: "invalid_configuration" | "provider_unavailable" | "internal_preparation";
    }>
  | Readonly<{ kind: "auth_required" }>
  | Readonly<{ kind: "server_not_ready" }>
  | ChatRuntimeUnavailableError
  | ChatInternalFailureError
  | Readonly<{ kind: "unknown"; code: string; knownDetail: ChatKnownErrorDetail | null }>;

export class ChatOperationError extends RpcError {
  constructor(
    rpcError: RpcError,
    readonly detail: ChatError,
  ) {
    super(rpcError);
    this.name = "ChatOperationError";
  }
}

type ChatWireError =
  | WireChatOperationError
  | GoalSetError
  | ReadError
  | ListPendingWorkError
  | LiveStopError
  | RemovePendingWorkError
  | SessionInitialInputError
  | SessionPersistInputDraftError
  | SessionResolveTransitionError;

type ChatRpcResult = Readonly<{
  outcome:
    | Readonly<{ case: "success"; value: Message }>
    | Readonly<{ case: "error"; value: ChatWireError }>
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

export function requireChatSuccess<Method extends DescMethod>(
  method: Method,
  result: ChatRpcResult,
): MethodSuccess<Method>;
export function requireChatSuccess(method: DescMethod, result: ChatRpcResult): Message {
  switch (result.outcome.case) {
    case "success":
      return result.outcome.value;
    case "error":
      throw chatOperationError(method, result.outcome.value);
    case undefined:
      throw new ContractError("Chat operation returned no outcome.");
  }
}

export function chatOperationError(method: DescMethod, failure: ChatWireError): ChatOperationError {
  const generic = protobufRpcError(method, failure);
  if (method === GoalService.method.set) {
    return goalSetOperationError(generic, failure);
  }
  return standardChatOperationError(generic, failure);
}

function goalSetOperationError(generic: RpcError, failure: ChatWireError): ChatOperationError {
  const knownDetail = understoodGoalErrorDetail(failure);
  switch (failure.code) {
    case "runtime_unavailable":
      if (knownDetail?.kind !== "runtime_unavailable") {
        throw new ContractError("Goal Set runtime-unavailable error detail is missing.");
      }
      return new ChatOperationError(generic, knownDetail);
    case "internal_failure":
      if (knownDetail?.kind !== "internal_failure") {
        throw new ContractError("Goal Set internal-failure error detail is missing.");
      }
      return new ChatOperationError(generic, knownDetail);
    case "":
      throw new ContractError("Chat operation returned an empty error code.");
    default:
      return new ChatOperationError(generic, {
        kind: "unknown",
        code: failure.code,
        knownDetail,
      });
  }
}

function standardChatOperationError(generic: RpcError, failure: ChatWireError): ChatOperationError {
  switch (failure.detail.case) {
    case "sessionNotFound":
      return new ChatOperationError(generic, {
        kind: "session_not_found",
        sessionID: failure.detail.value.sessionId,
      });
    case "workspaceNotRegistered":
      return new ChatOperationError(generic, { kind: "workspace_not_registered" });
    case "chatSettingsAgentPreparation":
      return new ChatOperationError(generic, {
        kind: "agent_preparation",
        agent: failure.detail.value.agent,
        category: agentPreparationCategory(failure.detail.value.category),
      });
    case "authRequired":
      return new ChatOperationError(generic, { kind: "auth_required" });
    case "serverNotReady":
      return new ChatOperationError(generic, { kind: "server_not_ready" });
    case "runtimeUnavailable":
      return new ChatOperationError(generic, {
        kind: "runtime_unavailable",
        sessionID: failure.detail.value.sessionId,
      });
    case "internalFailure":
      return new ChatOperationError(generic, {
        kind: "internal_failure",
        operation: failure.detail.value.operation ?? null,
        cause: failure.detail.value.cause ?? null,
      });
    case undefined:
      if (failure.code.length === 0) {
        throw new ContractError("Chat operation returned an empty error code.");
      }
      return new ChatOperationError(generic, { kind: "unknown", code: failure.code, knownDetail: null });
  }
}

function understoodGoalErrorDetail(failure: ChatWireError): ChatKnownErrorDetail | null {
  switch (failure.detail.case) {
    case "runtimeUnavailable":
      return { kind: "runtime_unavailable", sessionID: failure.detail.value.sessionId };
    case "internalFailure":
      return {
        kind: "internal_failure",
        operation: failure.detail.value.operation ?? null,
        cause: failure.detail.value.cause ?? null,
      };
    case "sessionNotFound":
    case "workspaceNotRegistered":
    case "chatSettingsAgentPreparation":
    case "authRequired":
    case "serverNotReady":
    case undefined:
      return null;
  }
}

function agentPreparationCategory(
  category: AgentPreparationCategory,
): "invalid_configuration" | "provider_unavailable" | "internal_preparation" {
  switch (category) {
    case AgentPreparationCategory.INVALID_CONFIGURATION:
      return "invalid_configuration";
    case AgentPreparationCategory.PROVIDER_UNAVAILABLE:
      return "provider_unavailable";
    case AgentPreparationCategory.INTERNAL_PREPARATION:
      return "internal_preparation";
    case AgentPreparationCategory.UNSPECIFIED:
      throw new ContractError("Chat operation returned an invalid Agent preparation category.");
  }
}
