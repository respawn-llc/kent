import { create } from "@app/server-api-contract";
import {
  SessionRuntimeService,
  SessionRuntimeReleaseClosePolicy,
} from "@app/server-api-contract/gen/kent/api/session_launch/session_lifecycle_pb";
import { ReadService as SessionReadService } from "@app/server-api-contract/gen/kent/api/session/session_pb";
import { ChatContextService } from "@app/server-api-contract/gen/kent/api/chat_context/chat_context_pb";
import {
  ReadService,
  StreamService,
  MessageSchema,
} from "@app/server-api-contract/gen/kent/api/transcript/transcript_pb";
import {
  StreamCompletionSchema,
  TranscriptCloseReason,
} from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import { requireUnarySuccess, streamCompletionFailure } from "./protobufRpc";
import { activateRuntime } from "./chatActivation";
import { createChatMutationApi } from "./chatMutations";
import { context, createChatSettingsApi } from "./chatSettings";
import { createChatGoalApi } from "./chatGoal";
import { ContractError, RpcError, TransportError } from "./errors";
import { mainView } from "./chatReadModel";
import { transcriptMessage, transcriptPage } from "./chatTranscript";
import { enumValue, required } from "./chatWire";
import { requireProjectAttachment } from "./chatAttachment";
import { requireSessionAttachment } from "./jsonRpcSocket";
import { InvalidTranscriptEventError } from "./subscriptionErrors";
import { isValidChatSessionID, requireChatSessionID } from "./chatTarget";
import type { ChatApi, ChatTranscriptMessage } from "./chatTypes";
import type { DescriptorRpcTransport } from "./transport";
export type {
  ChatApi,
  ChatAcceptedDiagnostic,
  ChatCompactionInvocation,
  ChatCompactionResult,
  ChatContext,
  ChatContextTarget,
  ChatExecutionTarget,
  ChatForkEditInput,
  InitialChatSettings,
  ChatInputMutationResult,
  ChatMainView,
  ChatMainViewRead,
  ChatActivation,
  ChatMutationTarget,
  ChatNotAcceptedReason,
  ChatProjectTarget,
  ChatRuntimeActivity,
  ChatRuntimeAttachment,
  ChatRuntimeRelease,
  ChatRuntimeStatus,
  ChatSessionTarget,
  ChatSettings,
  ChatSettingsTarget,
  ChatTranscriptCompletion,
  ChatTranscriptCommittedRow,
  ChatTranscriptHandler,
  ChatGoalObservationHandler,
  ChatTranscriptKind,
  ChatTranscriptMessage,
  ChatTranscriptMessageByKind,
  ChatTranscriptPage,
  ChatTranscriptPayload,
  ChatTranscriptPayloadByKind,
  ChatWorkspaceSelector,
} from "./chatTypes";
export type {
  ChatGoal,
  ChatGoalAvailability,
  ChatGoalFact,
  ChatGoalMutationResult,
  ChatGoalObservation,
  ChatGoalProjection,
  ChatGoalStatus,
} from "./chatGoal";

export function createChatApi(transport: DescriptorRpcTransport): ChatApi {
  return {
    ...createChatMutationApi(transport),
    ...createChatSettingsApi(transport),
    ...createChatGoalApi(transport),
    async getMainView(target) {
      const sessionId = requireChatSessionID(target);
      const method = SessionReadService.method.getMainView;
      const call = await transport.callDescriptorAttachedProject({
        projectID: target.projectID,
        selector: target.workspace,
        method,
        createRequest: () => create(method.input, { sessionId }),
      });
      requireProjectAttachment(call.attachment, target);
      return mainView(required(requireUnarySuccess(method, call.result).mainView), sessionId);
    },
    async getContext(target) {
      const sessionId = requireChatSessionID(target);
      const method = ChatContextService.method.get;
      const call = await transport.callDescriptorAttachedProject({
        projectID: target.projectID,
        selector: target.workspace,
        method,
        createRequest: () =>
          create(method.input, {
            target: { target: { case: "session", value: { sessionId } } },
          }),
      });
      requireProjectAttachment(call.attachment, target);
      return context(required(requireUnarySuccess(method, call.result).context));
    },
    async getTranscriptPage(target, cursor) {
      const sessionId = requireChatSessionID(target);
      const method = ReadService.method.getPage;
      const call = await transport.callDescriptorAttachedProject({
        projectID: target.projectID,
        selector: target.workspace,
        method,
        createRequest: () =>
          create(method.input, {
            sessionId,
            ...(cursor === undefined
              ? {}
              : {
                  direction: {
                    case: cursor.direction === "older" ? "cursor" : "newerCursor",
                    value: BigInt(cursor.value),
                  },
                }),
          }),
      });
      requireProjectAttachment(call.attachment, target);
      return transcriptPage(required(requireUnarySuccess(method, call.result).transcript), sessionId);
    },
    async activateRuntime(target) {
      const requestedSessionID = requireChatSessionID(target);
      return transport.runRuntimeOwner(requestedSessionID, { createIfMissing: true }, async (owner) => {
        requireSessionAttachment(owner.attachment, {
          projectID: target.projectID,
          sessionID: requestedSessionID,
        });
        return activateRuntime(owner, requestedSessionID);
      });
    },
    async releaseRuntime(attachment) {
      if (
        !isValidChatSessionID(attachment.sessionID) ||
        !Number.isInteger(attachment.generation) ||
        attachment.generation <= 0
      )
        throw new TypeError("Runtime attachment is invalid.");
      const requestedSessionID = attachment.sessionID;
      return transport.runRuntimeOwner(
        requestedSessionID,
        { createIfMissing: false, closeAfter: true },
        async (owner) => {
          requireSessionAttachment(owner.attachment, { sessionID: requestedSessionID });
          const method = SessionRuntimeService.method.release;
          const result = requireUnarySuccess(
            method,
            await owner.callDescriptor(
              method,
              create(method.input, {
                attachment: { sessionId: requestedSessionID, generation: BigInt(attachment.generation) },
                dropOwner: true,
                closePolicy: SessionRuntimeReleaseClosePolicy.CLOSE_IF_IDLE,
              }),
            ),
          );
          return { released: result.released, active: result.active };
        },
      );
    },
    subscribeTranscript(target, handler) {
      const sessionID = requireChatSessionID(target);
      const method = StreamService.method.subscribe;
      return transport.subscribeDescriptor({
        method,
        request: create(method.input, { sessionId: sessionID }),
        attachment: { projectID: target.projectID, sessionID },
        establishmentTimeoutMs: null,
        eventDescriptor: MessageSchema,
        completionDescriptor: StreamCompletionSchema,
        onStart: (result) => {
          requireUnarySuccess(method, result);
        },
        transcriptRejection: { onInvalidEvent: handler.onError },
        handler: {
          ...(handler.onOpen === undefined ? {} : { onOpen: handler.onOpen }),
          onEvent(value) {
            let event: ChatTranscriptMessage;
            try {
              event = transcriptMessage(value, sessionID);
            } catch (cause) {
              if (cause instanceof ContractError) throw new InvalidTranscriptEventError(cause);
              throw cause;
            }
            handler.onEvent(event);
          },
          onComplete(value) {
            handler.onComplete({
              code: value.code ?? 0,
              message: value.message ?? "",
              reason:
                value.transcriptCloseReason === undefined
                  ? null
                  : enumValue(value.transcriptCloseReason, {
                      [TranscriptCloseReason.SUBSCRIBER_OVERFLOW]: "subscriber_overflow",
                      [TranscriptCloseReason.CONTRACT_VIOLATION]: "contract_violation",
                    }),
            });
            return streamCompletionFailure(value);
          },
          onError(error) {
            if (error instanceof TransportError) handler.onTransportLoss?.();
            else if (error instanceof ContractError || error instanceof RpcError) handler.onError(error);
            else handler.onError(new ContractError("Transcript subscription is invalid."));
          },
        },
      });
    },
  };
}
