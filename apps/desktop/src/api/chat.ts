import { z } from "zod";
import { create } from "@app/server-api-contract";
import {
  GoalExecutionPolicy,
  GoalService,
  type GoalSetRequest,
} from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import {
  ChatTargetSchema,
  ExistingSessionTargetSchema,
  NewChatTargetSchema,
} from "@app/server-api-contract/gen/kent/api/chat/chat_pb";
import {
  InitialChatSettingsSchema,
  SupervisorValue,
} from "@app/server-api-contract/gen/kent/api/chat_settings/chat_settings_pb";

import { activateRuntime } from "./chatActivation";
import { createChatMutationApi } from "./chatMutations";
import { createChatSettingsApi } from "./chatSettings";
import { ContractError, RpcError, TransportError } from "./errors";
import { parseRpcResponse } from "./clientParse";
import { committedRowSchema, contextSchema, mainViewSchema, pageSchema } from "./chatSchemas";
import type { runtimeStatusSchema } from "./chatSchemas";
import { transcriptEventSchema } from "./chatTranscriptSchemas";
import { chatExecutionTarget, chatRuntimeActivity } from "./chatProjection";
import {
  goalFactFromMainView,
  chatGoalSetResultFromGenerated,
  parseGoalEnvelope,
  parseGoalMutationResult,
  parseGoalObservation,
} from "./chatGoal";
import type { ChatGoalMutationResult, ChatGoalSetTarget } from "./chatGoal";
import { requireProjectAttachment } from "./chatAttachment";
import { requireSessionAttachment } from "./jsonRpcSocket";
import { SubscriptionErrorAlreadyReported } from "./jsonRpcSubscription";
import { chatContextSessionID, isValidChatSessionID, requireChatSessionID } from "./chatTarget";
import type { ChatApi, ChatRuntimeStatus, ChatTranscriptMessage } from "./chatTypes";
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
  ChatGoalError,
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
import type { RpcEventHandler, DescriptorRpcTransport } from "./transport";
class RecoverableTranscriptEventError extends Error {
  constructor(readonly contractError: ContractError) {
    super(contractError.message);
  }
}
function transcriptMessageFromTarget(input: ChatTranscriptMessage, sessionID: string): ChatTranscriptMessage {
  if (input.kind === "session_identity") {
    if (input.payload.SessionID !== sessionID) {
      throw new RecoverableTranscriptEventError(
        new ContractError("Transcript event Session identity does not match the requested Session."),
      );
    }
  }
  if (input.kind === "hydration") {
    if (input.payload.SessionIdentity.SessionID !== sessionID) {
      throw new RecoverableTranscriptEventError(
        new ContractError("Transcript hydration Session identity does not match the requested Session."),
      );
    }
  }
  return input;
}
function runtimeStatus(input: z.output<typeof runtimeStatusSchema>): ChatRuntimeStatus {
  return {
    reviewerFrequency: input.ReviewerFrequency,
    reviewerEnabled: input.ReviewerEnabled,
    autoCompactionEnabled: input.AutoCompactionEnabled,
    questionsEnabled: input.QuestionsEnabled,
    fastModeAvailable: input.FastModeAvailable,
    fastModeEnabled: input.FastModeEnabled,
    conversationFreshness: input.ConversationFreshness,
    previousSessionID: input.PreviousSessionID ?? null,
    parentAgentSessionID: input.ParentAgentSessionID ?? null,
    navigationTargetSessionID: input.NavigationTargetSessionID ?? null,
    lastCommittedAssistantFinalAnswer: input.LastCommittedAssistantFinalAnswer ?? null,
    thinkingLevel: input.ThinkingLevel,
    compactionMode: input.CompactionMode,
    contextUsage: {
      usedTokens: input.ContextUsage.UsedTokens,
      windowTokens: input.ContextUsage.WindowTokens,
      cacheHitPercent: input.ContextUsage.CacheHitPercent,
      hasCacheHitPercentage: input.ContextUsage.HasCacheHitPercentage,
    },
    compactionCount: input.CompactionCount,
    workflowSession:
      input.WorkflowSession === null
        ? null
        : { taskID: input.WorkflowSession.TaskID, workflowID: input.WorkflowSession.WorkflowID },
  };
}
export function createChatApi(transport: DescriptorRpcTransport): ChatApi {
  return {
    ...createChatMutationApi(transport),
    ...createChatSettingsApi(transport),
    async getMainView(target) {
      const requestedSessionID = requireChatSessionID(target);
      const call = await transport.callAttachedProject({
        projectID: target.projectID,
        selector: target.workspace,
        method: "session.getMainView",
        request: { kind: "value", value: { SessionID: requestedSessionID } },
      });
      requireProjectAttachment(call.attachment, target);
      const response = parseRpcResponse("session.getMainView", mainViewSchema, call.result);
      if (response.MainView.Session.SessionID !== requestedSessionID)
        throw new ContractError("Session Main View does not match the requested Session.");
      const converted = {
        version: {
          epoch: response.MainView.Version.Epoch,
          generation: response.MainView.Version.Generation,
          sequence: response.MainView.Version.Sequence,
        },
        status: runtimeStatus(response.MainView.Status),
        sessionID: response.MainView.Session.SessionID,
        sessionName:
          response.MainView.Session.SessionName === "" ? null : response.MainView.Session.SessionName,
        executionTarget: chatExecutionTarget(response.MainView.Session.ExecutionTarget),
        activity: chatRuntimeActivity(response.MainView.Activity),
      };
      return {
        mainView: converted,
        goal: goalFactFromMainView(response.MainView.Status.Goal),
      };
    },
    async getGoal(target) {
      const requestedSessionID = requireChatSessionID(target);
      const call = await transport.callAttachedProject({
        projectID: target.projectID,
        selector: target.workspace,
        method: "runtime.goal.show",
        request: { kind: "value", value: { session_id: requestedSessionID } },
      });
      requireProjectAttachment(call.attachment, target);
      return parseGoalEnvelope(call.result);
    },
    async setGoal(target: ChatGoalSetTarget, objective: string) {
      if (target.kind === "session") {
        const sessionID = target.sessionID.trim();
        if (!isValidChatSessionID(sessionID)) {
          throw new TypeError("Session ID is required.");
        }
        const request = createGoalSetRequest({
          target: create(ChatTargetSchema, {
            target: {
              case: "session",
              value: create(ExistingSessionTargetSchema, { sessionId: sessionID }),
            },
          }),
          objective,
          actor: "user",
          executionPolicy: GoalExecutionPolicy.START_OR_CONTINUE,
        });
        const result = await transport.callDescriptor(GoalService.method.set, request);
        return chatGoalSetResultFromGenerated(result, sessionID);
      }
      const call = await transport.callDescriptorAttachedProject({
        projectID: target.projectID,
        selector: { workspaceID: target.workspaceID },
        method: GoalService.method.set,
        createRequest: (attachment) =>
          createGoalSetRequest({
            target: create(ChatTargetSchema, {
              target: {
                case: "newChat",
                value: create(NewChatTargetSchema, {
                  projectId: attachment.projectID,
                  workspaceId: attachment.workspaceID,
                  initialSettings: createInitialChatSettings(target.initialSettings),
                }),
              },
            }),
            objective,
            actor: "user",
            executionPolicy: GoalExecutionPolicy.START_OR_CONTINUE,
            ...(target.initialInputDraft === undefined
              ? {}
              : { initialInputDraft: target.initialInputDraft }),
          }),
      });
      requireProjectAttachment(call.attachment, {
        projectID: target.projectID,
        workspace: { workspaceID: target.workspaceID },
      });
      return chatGoalSetResultFromGenerated(call.result);
    },
    async pauseGoal(target) {
      return mutateGoal(transport, target, "runtime.goal.pause");
    },
    async resumeGoal(target) {
      return mutateGoal(transport, target, "runtime.goal.resume");
    },
    async completeGoal(target) {
      return mutateGoal(transport, target, "runtime.goal.complete");
    },
    async clearGoal(target) {
      return mutateGoal(transport, target, "runtime.goal.clear");
    },
    async getContext(target) {
      const requestedSessionID = chatContextSessionID(target);
      const call = await transport.callAttachedProject({
        projectID: target.projectID,
        selector: target.workspace,
        method: "chat.context.get",
        request: {
          kind: "value",
          value:
            requestedSessionID === undefined
              ? { target: { workspace_chat: {} } }
              : { target: { session: { session_id: requestedSessionID } } },
        },
      });
      requireProjectAttachment(call.attachment, target);
      const value = parseRpcResponse("chat.context.get", contextSchema, call.result).context;
      return {
        contextWindowTokens: value.context_window_tokens,
        usedTokens: value.used_tokens,
        remainingTokens: value.remaining_tokens,
        automaticThresholdTokens: value.automatic_threshold_tokens,
        autoCompactionEnabled: value.auto_compaction_enabled,
        compactionMode: value.compaction_mode,
        completedCompactionCount: value.completed_compaction_count,
        compactionRunning: value.compaction_running,
        manualCompactAvailable: value.manual_compact_available,
      };
    },
    async getTranscriptPage(target, cursor) {
      const requestedSessionID = requireChatSessionID(target);
      const call = await transport.callAttachedProject({
        projectID: target.projectID,
        selector: target.workspace,
        method: "session.getTranscriptPage",
        request: {
          kind: "value",
          value: {
            session_id: requestedSessionID,
            ...(cursor === undefined
              ? {}
              : cursor.direction === "older"
                ? { cursor: cursor.value }
                : { newer_cursor: cursor.value }),
          },
        },
      });
      requireProjectAttachment(call.attachment, target);
      const value = parseRpcResponse("session.getTranscriptPage", pageSchema, call.result).transcript;
      if (value.SessionID !== requestedSessionID)
        throw new ContractError("Transcript page does not match the requested Session.");
      return {
        sessionID: value.SessionID,
        sessionName: value.SessionName === "" ? null : value.SessionName,
        conversationFreshness: value.ConversationFreshness,
        olderCursor: value.OlderCursor ?? null,
        hasMoreAbove: value.HasMoreAbove,
        newerCursor: value.NewerCursor ?? null,
        hasMoreBelow: value.HasMoreBelow,
        latestRollbackCandidate: value.LatestRollbackCandidate ?? null,
        entries: value.Entries.map((entry) =>
          parseRpcResponse("session.getTranscriptPage", committedRowSchema, entry),
        ),
      };
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
          const result = parseRpcResponse(
            "session.runtime.release",
            z.object({ released: z.boolean(), active: z.boolean().optional() }).strict(),
            await owner.call("session.runtime.release", {
              attachment: { session_id: requestedSessionID, generation: attachment.generation },
              drop_owner: true,
              close_policy: "close_if_idle",
            }),
          );
          return { released: result.released, active: result.active ?? false };
        },
      );
    },
    subscribeTranscript(target, handler) {
      const requestedSessionID = requireChatSessionID(target);
      const rpcHandler: RpcEventHandler = {
        ...(handler.onOpen === undefined ? {} : { onOpen: handler.onOpen }),
        onEvent(method, params) {
          if (method !== "session.transcript")
            throw new ContractError("Transcript subscription received an unexpected event.");
          let event: ChatTranscriptMessage;
          try {
            event = transcriptMessageFromTarget(
              parseRpcResponse("session.transcript", transcriptEventSchema, params).message,
              requestedSessionID,
            );
          } catch (error) {
            if (error instanceof RecoverableTranscriptEventError) throw error;
            if (error instanceof ContractError)
              throw new RecoverableTranscriptEventError(new ContractError("Transcript event is invalid."));
            throw error;
          }
          handler.onEvent(event);
        },
        onEventFailure(error) {
          if (!(error instanceof RecoverableTranscriptEventError)) return false;
          try {
            handler.onError(error.contractError);
          } catch (cause) {
            throw new SubscriptionErrorAlreadyReported(
              cause instanceof Error ? cause : new ContractError("Subscription error handler failed."),
            );
          }
          return true;
        },
        onComplete(code, message, reason) {
          handler.onComplete({
            code,
            message,
            reason: reason === "subscriber_overflow" || reason === "contract_violation" ? reason : null,
          });
        },
        onError(error) {
          if (error instanceof ContractError || error instanceof RpcError) {
            handler.onError(error);
          } else if (error instanceof TransportError) {
            handler.onTransportLoss?.();
          }
        },
      };
      return transport.subscribeChatSession({
        projectID: target.projectID,
        sessionID: requestedSessionID,
        method: "session.subscribeTranscript",
        params: { SessionID: requestedSessionID },
        handler: rpcHandler,
        establishmentTimeoutMs: null,
      });
    },
    subscribeGoal(target, handler) {
      const requestedSessionID = requireChatSessionID(target);
      const rpcHandler: RpcEventHandler = {
        ...(handler.onOpen === undefined ? {} : { onOpen: handler.onOpen }),
        onEvent(method, params) {
          if (method !== "goal.observation")
            throw new ContractError("Goal observation received an unexpected event.");
          handler.onEvent(parseGoalObservation(params));
        },
        onComplete(code, message) {
          handler.onComplete(code, message);
        },
        onError(error) {
          if (error instanceof ContractError || error instanceof RpcError || error instanceof TransportError)
            handler.onError(error);
        },
      };
      return transport.subscribeChatSession({
        projectID: target.projectID,
        sessionID: requestedSessionID,
        method: "goal.observe",
        params: { session_id: requestedSessionID },
        handler: rpcHandler,
      });
    },
  };
}

async function mutateGoal(
  transport: DescriptorRpcTransport,
  target: Parameters<ChatApi["pauseGoal"]>[0],
  method: "runtime.goal.pause" | "runtime.goal.resume" | "runtime.goal.complete" | "runtime.goal.clear",
): Promise<ChatGoalMutationResult> {
  const requestedSessionID = requireChatSessionID(target);
  const call = await transport.callAttachedProject({
    projectID: target.projectID,
    selector: target.workspace,
    method,
    request: { kind: "value", value: { session_id: requestedSessionID, actor: "user" } },
  });
  requireProjectAttachment(call.attachment, target);
  return parseGoalMutationResult(call.result);
}

function createGoalSetRequest(input: {
  target: GoalSetRequest["target"];
  objective: string;
  actor: string;
  executionPolicy: GoalExecutionPolicy;
  initialInputDraft?: string;
}): GoalSetRequest {
  return create(
    GoalService.method.set.input,
    input.initialInputDraft === undefined
      ? {
          target: input.target,
          objective: input.objective,
          actor: input.actor,
          executionPolicy: input.executionPolicy,
        }
      : {
          target: input.target,
          objective: input.objective,
          actor: input.actor,
          executionPolicy: input.executionPolicy,
          initialInputDraft: input.initialInputDraft,
        },
  );
}

function createInitialChatSettings(settings: {
  agentRole: string;
  supervisor: "off" | "edits" | "all";
  thinking: string | null;
  fast: boolean | null;
  questionsEnabled: boolean;
  autoCompactionEnabled: boolean;
}) {
  return create(InitialChatSettingsSchema, {
    agentRole: settings.agentRole,
    supervisor:
      settings.supervisor === "off"
        ? SupervisorValue.OFF
        : settings.supervisor === "edits"
          ? SupervisorValue.AFTER_EDITS
          : SupervisorValue.ALWAYS,
    ...(settings.thinking === null ? {} : { thinking: settings.thinking }),
    ...(settings.fast === null ? {} : { fast: settings.fast }),
    questionsEnabled: settings.questionsEnabled,
    autoCompactionEnabled: settings.autoCompactionEnabled,
  });
}
