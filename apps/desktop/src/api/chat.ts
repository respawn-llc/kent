import { create } from "@app/server-api-contract";
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
import { createChatCommandApi } from "./chatCommands";
import { createChatDraftApi } from "./chatDrafts";
import { createChatNameApi } from "./chatName";
import { chatSettingsObservation } from "./chatSettingsObservation";
import { listPendingPrompts } from "./clientPendingPrompts";
import { answerPromptBatch } from "./clientPromptAnswers";
import { ContractError, RpcError, TransportError } from "./errors";
import { mainView } from "./chatReadModel";
import { transcriptMessage, transcriptPage } from "./chatTranscript";
import { enumValue, required } from "./chatWire";
import { InvalidTranscriptEventError } from "./subscriptionErrors";
import { requireChatSessionID } from "./chatTarget";
import type { ChatApi, ChatTranscriptMessage } from "./chatTypes";
import type { RpcTransport } from "./transport";
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
  ChatRuntimeStatus,
  ChatSessionTarget,
  ChatSessionNameMutation,
  ChatSettingsSnapshot,
  ChatSettingsObservation,
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
  ChatGoalSetResult,
  ChatGoalSetTarget,
  ChatGoal,
  ChatGoalAvailability,
  ChatGoalFact,
  ChatGoalMutationResult,
  ChatGoalObservation,
  ChatGoalProjection,
  ChatGoalStatus,
} from "./chatGoal";

export function createChatApi(transport: RpcTransport): ChatApi {
  return {
    subscribeSettings: (target, reportOverflow) => chatSettingsObservation(transport, target, reportOverflow),
    listPendingPrompts: async (target) =>
      listPendingPrompts(transport, {
        sessionID: requireChatSessionID(target),
      }),
    answerPromptBatch: async (input) => answerPromptBatch(transport, input),
    ...createChatMutationApi(transport),
    ...createChatDraftApi(transport),
    ...createChatNameApi(transport),
    ...createChatSettingsApi(transport),
    ...createChatGoalApi(transport),
    ...createChatCommandApi(transport),
    async getPromptHistory(target) {
      const method = SessionReadService.method.getPromptHistory;
      const result = await transport.callDescriptorAttachedSession(
        target,
        method,
        create(method.input, {
          sessionId: { value: { case: "sessionId", value: requireChatSessionID(target) } },
        }),
      );
      return requireUnarySuccess(method, result).prompts;
    },
    async resolveViewedImageOriginalSource(target, path, signal) {
      const method = SessionReadService.method.resolveViewedImageOriginalSource;
      const result = await transport.callDescriptorAttachedSession(
        target,
        method,
        create(method.input, { sessionId: requireChatSessionID(target), path }),
        signal === undefined ? undefined : { signal },
      );
      return requireUnarySuccess(method, result).url;
    },
    async getMainView(target) {
      const sessionId = requireChatSessionID(target);
      const method = SessionReadService.method.getMainView;
      const result = await transport.callDescriptorAttachedSession(
        target,
        method,
        create(method.input, { sessionId: { value: { case: "sessionId", value: sessionId } } }),
      );
      return mainView(required(requireUnarySuccess(method, result).mainView), sessionId);
    },
    async getContext(target) {
      const sessionId = requireChatSessionID(target);
      const method = ChatContextService.method.get;
      const result = await transport.callDescriptorAttachedSession(
        target,
        method,
        create(method.input, {
          target: {
            target: {
              case: "session",
              value: { sessionId: { value: { case: "sessionId", value: sessionId } } },
            },
          },
        }),
      );
      return context(required(requireUnarySuccess(method, result).context));
    },
    async getTranscriptPage(target, cursor) {
      const sessionId = requireChatSessionID(target);
      const method = ReadService.method.getPage;
      const result = await transport.callDescriptorAttachedSession(
        target,
        method,
        create(method.input, {
          sessionId: { value: { case: "sessionId", value: sessionId } },
          ...(cursor === undefined
            ? {}
            : {
                direction: {
                  case: cursor.direction === "older" ? "cursor" : "newerCursor",
                  value: BigInt(cursor.value),
                },
              }),
        }),
      );
      return transcriptPage(required(requireUnarySuccess(method, result).transcript), sessionId);
    },
    async activateRuntime(target) {
      return activateRuntime(transport, target);
    },
    subscribeTranscript(target, handler) {
      const sessionID = requireChatSessionID(target);
      const method = StreamService.method.subscribe;
      return transport.subscribeDescriptor({
        method,
        request: create(method.input, { sessionId: { value: { case: "sessionId", value: sessionID } } }),
        attachment: { sessionID },
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
              code: value.code ?? null,
              message: value.message ?? null,
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
