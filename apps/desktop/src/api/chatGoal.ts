import { create } from "@app/server-api-contract";
import * as R from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import { StreamCompletionSchema } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import { requireProjectAttachment } from "./chatAttachment";
import { requireChatSessionID } from "./chatTarget";
import { enumValue, required, safeNumber } from "./chatWire";
import { timestampMillis } from "./clientTime";
import { ContractError } from "./errors";
import { requireUnarySuccess, streamCompletionFailure } from "./protobufRpc";
import { defaultSubscriptionEstablishmentTimeoutMs } from "./jsonRpcSubscription";
import type { ChatApi } from "./chatTypes";
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

function mutationResult(value: R.GoalMutationSuccess): ChatGoalMutationResult {
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

export function createChatGoalApi(
  transport: DescriptorRpcTransport,
): Pick<
  ChatApi,
  "getGoal" | "setGoal" | "pauseGoal" | "resumeGoal" | "completeGoal" | "clearGoal" | "subscribeGoal"
> {
  const mutate = async (
    target: Parameters<ChatApi["setGoal"]>[0],
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
    return mutationResult(requireUnarySuccess(method, call.result));
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
    setGoal: async (target, objective) => mutate(target, R.GoalService.method.set, objective),
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
