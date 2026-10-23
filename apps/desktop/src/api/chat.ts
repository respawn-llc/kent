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
import { subscriptionStream } from "./subscriptionStream";
import { listPendingPrompts } from "./clientPendingPrompts";
import { answerPromptBatch } from "./clientPromptAnswers";
import { ContractError, RpcError, TransportError } from "./errors";
import { mainView } from "./chatReadModel";
import { transcriptMessage, transcriptPage } from "./chatTranscript";
import { enumValue } from "./chatWire";
import { InvalidTranscriptEventError } from "./subscriptionErrors";
import type { ChatApi, ChatTranscriptMessage, ChatTranscriptObservation } from "./chatTypes";
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
  ChatTranscriptObservation,
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
        sessionID: target.sessionID,
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
          sessionId: { value: { case: "sessionId", value: target.sessionID } },
        }),
      );
      return requireUnarySuccess(method, result).prompts;
    },
    async resolveViewedImageOriginalSource(target, path, signal) {
      const method = SessionReadService.method.resolveViewedImageOriginalSource;
      const result = await transport.callDescriptorAttachedSession(
        target,
        method,
        create(method.input, { sessionId: target.sessionID, path }),
        signal === undefined ? undefined : { signal },
      );
      return requireUnarySuccess(method, result).url;
    },
    async getMainView(target) {
      const sessionId = target.sessionID;
      const method = SessionReadService.method.getMainView;
      const result = await transport.callDescriptorAttachedSession(
        target,
        method,
        create(method.input, { sessionId: { value: { case: "sessionId", value: sessionId } } }),
      );
      return mainView(requireUnarySuccess(method, result).mainView, sessionId);
    },
    async getContext(target) {
      const sessionId = target.sessionID;
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
      return context(requireUnarySuccess(method, result).context);
    },
    async getTranscriptPage(target, cursor) {
      const sessionId = target.sessionID;
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
      return transcriptPage(requireUnarySuccess(method, result).transcript, sessionId);
    },
    async activateRuntime(target) {
      return activateRuntime(transport, target);
    },
    subscribeTranscript(target, reportOverflow) {
      return subscriptionStream<ChatTranscriptObservation>((emit) => {
        const sessionID = target.sessionID;
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
          transcriptRejection: {
            onInvalidEvent: (error) => {
              emit({ kind: "error", error });
            },
          },
          handler: {
            onOpen: () => {
              emit({ kind: "open" });
            },
            onEvent(value) {
              let event: ChatTranscriptMessage;
              try {
                event = transcriptMessage(value, sessionID);
              } catch (cause) {
                if (cause instanceof ContractError) throw new InvalidTranscriptEventError(cause);
                throw cause;
              }
              emit({ kind: "event", event });
            },
            onComplete(value) {
              emit({
                kind: "complete",
                completion: {
                  code: value.code ?? null,
                  message: value.message ?? null,
                  reason:
                    value.transcriptCloseReason === undefined
                      ? null
                      : enumValue(value.transcriptCloseReason, {
                          [TranscriptCloseReason.SUBSCRIBER_OVERFLOW]: "subscriber_overflow",
                          [TranscriptCloseReason.CONTRACT_VIOLATION]: "contract_violation",
                        }),
                },
              });
              return streamCompletionFailure(value);
            },
            onError(error) {
              emit({
                kind: "error",
                error:
                  error instanceof TransportError ||
                  error instanceof ContractError ||
                  error instanceof RpcError
                    ? error
                    : new ContractError("Transcript subscription is invalid."),
              });
            },
          },
        });
      }, reportOverflow);
    },
  };
}
