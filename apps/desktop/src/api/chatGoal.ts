import { create, validate } from "@app/server-api-contract";
import * as R from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import {
  ChatTargetSchema,
  ExistingSessionTargetSchema,
  NewChatTargetSchema,
} from "@app/server-api-contract/gen/kent/api/chat/chat_pb";
import { StreamCompletionSchema } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import { requireProjectAttachment } from "./chatAttachment";
import { isValidChatSessionID, requireChatSessionID } from "./chatTarget";
import { initialChatSettingsToWire } from "./chatSettings";
import { enumValue, required, safeNumber } from "./chatWire";
import { timestampMillis } from "./clientTime";
import { ContractError } from "./errors";
import { chatOperationError, type ChatOperationError } from "./chatErrors";
import { requireUnarySuccess, streamCompletionFailure } from "./protobufRpc";
import { defaultSubscriptionEstablishmentTimeoutMs } from "./jsonRpcSubscription";
import type { ChatApi, ChatSessionTarget, InitialChatSettings } from "./chatTypes";
import type { ChatGoalFacts } from "./chatTranscriptTypes";
import type { DescriptorRpcTransport } from "./transport";

export type ChatGoalAvailability = "available" | "agent_capability_missing";
export type ChatGoalStatus = "active" | "paused" | "complete";
export type ChatGoal = Readonly<{
  id: string;
  objective: string;
  status: ChatGoalStatus;
  createdAt: string;
  updatedAt: string;
}>;
export type ChatGoalFact = Readonly<{ goal: ChatGoal | null; availability: ChatGoalAvailability | null }>;
export type ChatGoalProjection =
  Readonly<{ kind: "unobserved" }> | Readonly<{ kind: "observed"; value: ChatGoalFact }>;
export type ChatGoalMutationResult =
  | Readonly<{ kind: "authoritative_goal"; fact: ChatGoalFact & Readonly<{ goal: ChatGoal }> }>
  | Readonly<{ kind: "authoritative_clear"; fact: ChatGoalFact & Readonly<{ goal: null }> }>;
export type ChatGoalSetTarget =
  | Readonly<{
      kind: "session";
      sessionID: string;
      projectID?: string;
      workspace?: Readonly<{ workspaceID: string } | { workspaceRoot: string }>;
    }>
  | Readonly<{
      kind: "new_chat";
      projectID: string;
      workspaceID: string;
      initialSettings: InitialChatSettings;
      initialInputDraft?: string;
    }>;
export type ChatGoalSetResult = Readonly<{
  sessionID: string;
  outcome:
    | Readonly<{
        kind: "mutation";
        mutation: ChatGoalMutationResult;
        diagnostic: ChatOperationError | null;
      }>
    | Readonly<{ kind: "rejected"; error: ChatOperationError }>;
}>;
export type ChatGoalObservation = Readonly<{
  sequence: number;
  kind: "hydration" | "update";
  fact: ChatGoalFact;
}>;

export function goalAvailability(value: R.GoalAvailability | undefined): ChatGoalAvailability | null {
  return value === undefined
    ? null
    : enumValue(value, {
        [R.GoalAvailability.AVAILABLE]: "available",
        [R.GoalAvailability.AGENT_CAPABILITY_MISSING]: "agent_capability_missing",
      });
}

function goal(value: R.Goal): ChatGoal {
  return {
    id: value.id,
    objective: value.objective,
    status: enumValue(value.status, {
      [R.GoalStatus.RUNTIME_GOAL_STATUS_ACTIVE]: "active",
      [R.GoalStatus.RUNTIME_GOAL_STATUS_PAUSED]: "paused",
      [R.GoalStatus.RUNTIME_GOAL_STATUS_COMPLETE]: "complete",
    }),
    createdAt: new Date(timestampMillis(required(value.createdAt))).toISOString(),
    updatedAt: new Date(timestampMillis(required(value.updatedAt))).toISOString(),
  };
}

export function goalFactFromWire(
  value: R.GoalView | R.GoalProjection | R.GoalShowSuccess | undefined,
): ChatGoalFact {
  return {
    goal: value?.goal === undefined ? null : goal(value.goal),
    availability: goalAvailability(value?.availability),
  };
}

export function goalFacts(value: R.GoalView): ChatGoalFacts {
  const fact = goalFactFromWire(value);
  return {
    Goal:
      fact.goal === null
        ? null
        : {
            id: fact.goal.id,
            objective: fact.goal.objective,
            status: fact.goal.status,
            created_at: fact.goal.createdAt,
            updated_at: fact.goal.updatedAt,
            Suspended: value.suspended,
          },
    Availability: fact.availability,
  };
}

export function goalFactFromTranscript(input: ChatGoalFacts): ChatGoalFact {
  return {
    goal:
      input.Goal === null
        ? null
        : {
            id: input.Goal.id,
            objective: input.Goal.objective,
            status: input.Goal.status,
            createdAt: input.Goal.created_at,
            updatedAt: input.Goal.updated_at,
          },
    availability: input.Availability,
  };
}

export function goalMutationFromGenerated(value: R.GoalMutationSuccess): ChatGoalMutationResult {
  const availability = goalAvailability(value.availability);
  switch (value.kind) {
    case R.GoalMutationResultKind.AUTHORITATIVE_GOAL:
      return { kind: "authoritative_goal", fact: { goal: goal(required(value.goal)), availability } };
    case R.GoalMutationResultKind.AUTHORITATIVE_CLEAR:
      if (value.goal !== undefined) throw new ContractError("Cleared Goal result contains a Goal.");
      return { kind: "authoritative_clear", fact: { goal: null, availability } };
    case R.GoalMutationResultKind.UNSPECIFIED:
      throw new ContractError("Goal mutation result is invalid.");
  }
}

function observation(value: R.GoalObservation): ChatGoalObservation {
  const kind = enumValue(value.kind, {
    [R.GoalObservationKind.HYDRATION]: "hydration",
    [R.GoalObservationKind.UPDATE]: "update",
  });
  const sequence = safeNumber(value.sequence);
  if ((kind === "hydration" && sequence !== 1) || (kind === "update" && sequence < 2))
    throw new ContractError("Goal observation sequence does not match kind.");
  return { sequence, kind, fact: goalFactFromWire(required(value.status)) };
}

export function chatGoalSetResultFromGenerated(
  result: R.GoalSetResult,
  requestedSessionID?: string,
): ChatGoalSetResult {
  try {
    validate(R.GoalSetResultSchema, result);
  } catch {
    throw new ContractError("Goal Set result did not match the GUI contract.");
  }
  switch (result.outcome.case) {
    case "error":
      throw chatOperationError(R.GoalService.method.set, result.outcome.value);
    case "success": {
      const success = result.outcome.value;
      const sessionID = goalSetSessionID(success.session?.sessionId, requestedSessionID);
      return { sessionID, outcome: goalSetOutcome(success) };
    }
    case undefined:
      throw new ContractError("Goal Set result outcome is required.");
  }
}

function goalSetSessionID(sessionID: string | undefined, requestedSessionID?: string): string {
  if (sessionID === undefined || sessionID.trim().length === 0) {
    throw new ContractError("Goal Set success Session is required.");
  }
  if (requestedSessionID !== undefined && sessionID !== requestedSessionID) {
    throw new ContractError("Goal Set success Session does not match the requested Session.");
  }
  return sessionID;
}

function goalSetOutcome(success: R.GoalSetSuccess): ChatGoalSetResult["outcome"] {
  switch (success.outcome.case) {
    case "mutation": {
      const mutation = goalMutationFromGenerated(success.outcome.value);
      if (mutation.kind !== "authoritative_goal") {
        throw new ContractError("Goal Set response returned an illegal mutation result.");
      }
      return {
        kind: "mutation",
        mutation,
        diagnostic:
          success.diagnostic === undefined
            ? null
            : chatOperationError(R.GoalService.method.set, success.diagnostic),
      };
    }
    case "rejected":
      if (success.diagnostic !== undefined) {
        throw new ContractError("Goal Set rejection cannot include a diagnostic.");
      }
      return {
        kind: "rejected",
        error: chatOperationError(R.GoalService.method.set, success.outcome.value),
      };
    case undefined:
      throw new ContractError("Goal Set success outcome is required.");
  }
}

export function createChatGoalApi(
  transport: DescriptorRpcTransport,
): Pick<
  ChatApi,
  "getGoal" | "setGoal" | "pauseGoal" | "resumeGoal" | "completeGoal" | "clearGoal" | "subscribeGoal"
> {
  const mutate = async (
    target: ChatSessionTarget,
    method:
      | typeof R.GoalService.method.set
      | typeof R.GoalService.method.pause
      | typeof R.GoalService.method.resume
      | typeof R.GoalService.method.complete
      | typeof R.GoalService.method.clear,
    objective?: string,
  ) => {
    const sessionId = requireChatSessionID(target);
    const call = await transport.callDescriptorAttachedProject({
      projectID: target.projectID,
      selector: target.workspace,
      method,
      createRequest: () =>
        create(method.input, { sessionId, actor: "user", ...(objective === undefined ? {} : { objective }) }),
    });
    requireProjectAttachment(call.attachment, target);
    return goalMutationFromGenerated(requireUnarySuccess(method, call.result));
  };
  return {
    async getGoal(target) {
      const sessionId = requireChatSessionID(target);
      const method = R.GoalService.method.show;
      const call = await transport.callDescriptorAttachedProject({
        projectID: target.projectID,
        selector: target.workspace,
        method,
        createRequest: () => create(method.input, { sessionId }),
      });
      requireProjectAttachment(call.attachment, target);
      return goalFactFromWire(requireUnarySuccess(method, call.result));
    },
    async setGoal(target, objective) {
      if (target.kind === "session") {
        const sessionID = target.sessionID.trim();
        if (!isValidChatSessionID(sessionID)) throw new TypeError("Session ID is required.");
        const result = await transport.callDescriptorAttachedSession(
          sessionID,
          R.GoalService.method.set,
          create(R.GoalSetRequestSchema, {
            target: create(ChatTargetSchema, {
              target: {
                case: "session",
                value: create(ExistingSessionTargetSchema, { sessionId: sessionID }),
              },
            }),
            objective,
            actor: "user",
            executionPolicy: R.GoalExecutionPolicy.START_OR_CONTINUE,
          }),
        );
        return chatGoalSetResultFromGenerated(result, sessionID);
      }
      const call = await transport.callDescriptorAttachedProject({
        projectID: target.projectID,
        selector: { workspaceID: target.workspaceID },
        method: R.GoalService.method.set,
        createRequest: (attachment) =>
          create(R.GoalSetRequestSchema, {
            target: create(ChatTargetSchema, {
              target: {
                case: "newChat",
                value: create(NewChatTargetSchema, {
                  projectId: attachment.projectID,
                  workspaceId: attachment.workspaceID,
                  initialSettings: initialChatSettingsToWire(target.initialSettings),
                }),
              },
            }),
            objective,
            actor: "user",
            executionPolicy: R.GoalExecutionPolicy.START_OR_CONTINUE,
            ...(target.initialInputDraft === undefined ? {} : { initialInputDraft: target.initialInputDraft }),
          }),
      });
      requireProjectAttachment(call.attachment, {
        projectID: target.projectID,
        workspace: { workspaceID: target.workspaceID },
      });
      return chatGoalSetResultFromGenerated(call.result);
    },
    pauseGoal: async (target) => mutate(target, R.GoalService.method.pause),
    resumeGoal: async (target) => mutate(target, R.GoalService.method.resume),
    completeGoal: async (target) => mutate(target, R.GoalService.method.complete),
    clearGoal: async (target) => mutate(target, R.GoalService.method.clear),
    subscribeGoal(target, handler) {
      const sessionID = requireChatSessionID(target);
      const method = R.GoalService.method.observe;
      return transport.subscribeDescriptor({
        method,
        request: create(method.input, { sessionId: sessionID }),
        attachment: { projectID: target.projectID, sessionID },
        establishmentTimeoutMs: defaultSubscriptionEstablishmentTimeoutMs,
        eventDescriptor: R.GoalObservationSchema,
        completionDescriptor: StreamCompletionSchema,
        onStart: (result) => {
          requireUnarySuccess(method, result);
        },
        handler: {
          ...(handler.onOpen === undefined ? {} : { onOpen: handler.onOpen }),
          onEvent: (value) => {
            handler.onEvent(observation(value));
          },
          onComplete(value) {
            handler.onComplete(value.code ?? 0, value.message ?? "");
            return streamCompletionFailure(value);
          },
          onError: handler.onError,
        },
      });
    },
  };
}
