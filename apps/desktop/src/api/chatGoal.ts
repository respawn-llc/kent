import { z } from "zod";

import { validate } from "@app/server-api-contract";
import {
  GoalAvailability,
  GoalMutationResultKind,
  GoalSetResultSchema,
  GoalStatus,
} from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import type {
  GoalMutationSuccess,
  GoalSetError,
  GoalSetResult,
} from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import { ContractError } from "./errors";
import { parseRpcResponse } from "./clientParse";
import { timestampMillis } from "./clientTime";
import { goalSchema, type runtimeStatusSchema } from "./chatSchemas";
import { type goalStatusSchema } from "./chatTranscriptFactSchemas";
import type { ChatProjectTarget, InitialChatSettings } from "./chatTypes";

export type ChatGoalAvailability = "available" | "agent_capability_missing";
export type ChatGoalStatus = "active" | "paused" | "complete";
export type ChatGoal = Readonly<{
  id: string;
  objective: string;
  status: ChatGoalStatus;
  createdAt: string;
  updatedAt: string;
}>;
export type ChatGoalFact = Readonly<{
  goal: ChatGoal | null;
  availability: ChatGoalAvailability | null;
}>;
export type ChatGoalProjection =
  Readonly<{ kind: "unobserved" }> | Readonly<{ kind: "observed"; value: ChatGoalFact }>;
export type ChatGoalMutationResult =
  | Readonly<{ kind: "authoritative_goal"; fact: ChatGoalFact & Readonly<{ goal: ChatGoal }> }>
  | Readonly<{ kind: "authoritative_clear"; fact: ChatGoalFact & Readonly<{ goal: null }> }>;
export type ChatGoalError =
  | Readonly<{ kind: "runtime_unavailable" }>
  | Readonly<{ kind: "internal_failure"; operation: string | null; cause: string | null }>
  | Readonly<{
      kind: "unknown";
      code: string;
      runtimeUnavailableSessionID?: string;
      internalFailureOperation?: string | null;
      internalFailureCause?: string | null;
      unknownFields: readonly NonNullable<GoalSetError["$unknown"]>[number][];
    }>;
export type ChatGoalSetTarget =
  | Readonly<{
      kind: "session";
      sessionID: string;
      projectID?: string;
      workspace?: ChatProjectTarget["workspace"];
    }>
  | Readonly<{
      kind: "new_chat";
      projectID: string;
      workspaceID: string;
      initialSettings: InitialChatSettings;
      initialInputDraft?: string;
    }>;
export type ChatGoalSetResult = Readonly<{
  sessionID: string | null;
  outcome:
    | Readonly<{ kind: "mutation"; mutation: ChatGoalMutationResult }>
    | Readonly<{ kind: "rejected"; error: ChatGoalError }>;
  diagnostic: ChatGoalError | null;
}>;
export type ChatGoalObservation = Readonly<{
  sequence: number;
  kind: "hydration" | "update";
  fact: ChatGoalFact;
}>;

const availabilitySchema = z.enum(["available", "agent_capability_missing"]);
const nullableAvailabilitySchema = availabilitySchema.nullable();
const goalEnvelopeSchema = z
  .object({
    goal: goalSchema.optional(),
    availability: availabilitySchema,
  })
  .strict();
const goalMutationResponseSchema = z
  .object({
    result: z.discriminatedUnion("kind", [
      z
        .object({
          kind: z.literal("authoritative_goal"),
          goal: goalSchema,
          availability: nullableAvailabilitySchema,
        })
        .strict(),
      z
        .object({
          kind: z.literal("authoritative_clear"),
          availability: nullableAvailabilitySchema,
        })
        .strict(),
    ]),
  })
  .strict();
const goalObservationEventSchema = z
  .object({
    observation: z
      .object({
        sequence: z.number().int().positive(),
        kind: z.enum(["hydration", "update"]),
        status: z
          .object({
            goal: goalSchema.nullable(),
            availability: nullableAvailabilitySchema,
          })
          .strict(),
      })
      .strict()
      .superRefine((observation, context) => {
        if (
          (observation.kind === "hydration" && observation.sequence !== 1) ||
          (observation.kind === "update" && observation.sequence < 2)
        ) {
          context.addIssue({ code: "custom", message: "Goal observation sequence does not match kind." });
        }
      }),
  })
  .strict();

export function chatGoal(input: z.output<typeof goalSchema>): ChatGoal {
  return {
    id: input.id,
    objective: input.objective,
    status: input.status,
    createdAt: input.created_at,
    updatedAt: input.updated_at,
  };
}

export function goalFactFromMainView(input: z.output<typeof runtimeStatusSchema>["Goal"]): ChatGoalFact {
  if (input === null) return { goal: null, availability: null };
  return {
    goal: input.Goal === null ? null : chatGoal(input.Goal),
    availability: input.Availability,
  };
}

export function goalFactFromTranscript(input: z.output<typeof goalStatusSchema>): ChatGoalFact {
  return {
    goal: input.Goal === null ? null : chatGoal(input.Goal),
    availability: input.Availability,
  };
}

export function parseGoalEnvelope(input: unknown): ChatGoalFact {
  const parsed = parseRpcResponse("runtime.goal.show", goalEnvelopeSchema, input);
  return {
    goal: parsed.goal === undefined ? null : chatGoal(parsed.goal),
    availability: parsed.availability,
  };
}

export function parseGoalMutationResult(input: unknown): ChatGoalMutationResult {
  const result = parseRpcResponse("runtime.goal.mutate", goalMutationResponseSchema, input).result;
  switch (result.kind) {
    case "authoritative_goal":
      return {
        kind: result.kind,
        fact: { goal: chatGoal(result.goal), availability: result.availability },
      };
    case "authoritative_clear":
      return { kind: result.kind, fact: { goal: null, availability: result.availability } };
  }
}

export function goalMutationFromGenerated(success: GoalMutationSuccess): ChatGoalMutationResult {
  switch (success.kind) {
    case GoalMutationResultKind.AUTHORITATIVE_GOAL: {
      if (success.goal?.createdAt === undefined || success.goal.updatedAt === undefined) {
        throw new ContractError("Authoritative Goal result requires complete Goal timestamps.");
      }
      const status = goalStatusFromGenerated(success.goal.status);
      return {
        kind: "authoritative_goal",
        fact: {
          goal: chatGoal({
            id: success.goal.id,
            objective: success.goal.objective,
            status,
            created_at: new Date(timestampMillis(success.goal.createdAt)).toISOString(),
            updated_at: new Date(timestampMillis(success.goal.updatedAt)).toISOString(),
          }),
          availability:
            success.availability === undefined ? null : goalAvailabilityFromGenerated(success.availability),
        },
      };
    }
    case GoalMutationResultKind.AUTHORITATIVE_CLEAR:
      return {
        kind: "authoritative_clear",
        fact: {
          goal: null,
          availability:
            success.availability === undefined ? null : goalAvailabilityFromGenerated(success.availability),
        },
      };
    case GoalMutationResultKind.UNSPECIFIED:
      throw new ContractError("Goal mutation result kind is invalid.");
    default:
      throw new ContractError("Goal mutation result kind is invalid.");
  }
}

function goalStatusFromGenerated(status: GoalStatus): ChatGoalStatus {
  switch (status) {
    case GoalStatus.RUNTIME_GOAL_STATUS_ACTIVE:
      return "active";
    case GoalStatus.RUNTIME_GOAL_STATUS_PAUSED:
      return "paused";
    case GoalStatus.RUNTIME_GOAL_STATUS_COMPLETE:
      return "complete";
    case GoalStatus.RUNTIME_GOAL_STATUS_UNSPECIFIED:
      throw new ContractError("Goal status is invalid.");
    default:
      throw new ContractError("Goal status is invalid.");
  }
}

function goalAvailabilityFromGenerated(availability: GoalAvailability): ChatGoalAvailability {
  switch (availability) {
    case GoalAvailability.AVAILABLE:
      return "available";
    case GoalAvailability.AGENT_CAPABILITY_MISSING:
      return "agent_capability_missing";
    case GoalAvailability.UNSPECIFIED:
      throw new ContractError("Goal availability is invalid.");
    default:
      throw new ContractError("Goal availability is invalid.");
  }
}

export function goalErrorFromGenerated(error: GoalSetError): ChatGoalError {
  switch (error.code) {
    case "runtime_unavailable":
      if (error.detail.case !== "runtimeUnavailable")
        throw new ContractError("Runtime-unavailable Goal error detail is missing.");
      return { kind: "runtime_unavailable" };
    case "internal_failure":
      if (error.detail.case !== "internalFailure")
        throw new ContractError("Internal Goal error detail is missing.");
      return {
        kind: "internal_failure",
        operation: error.detail.value.operation ?? null,
        cause: error.detail.value.cause ?? null,
      };
    default:
      return {
        kind: "unknown",
        code: error.code,
        ...unknownGoalErrorDetail(error),
        unknownFields: (error.$unknown ?? []).map((field) => ({
          no: field.no,
          wireType: field.wireType,
          data: field.data.slice(),
        })),
      };
  }
}

function unknownGoalErrorDetail(error: GoalSetError): Readonly<{
  runtimeUnavailableSessionID?: string;
  internalFailureOperation?: string | null;
  internalFailureCause?: string | null;
}> {
  switch (error.detail.case) {
    case "runtimeUnavailable":
      return { runtimeUnavailableSessionID: error.detail.value.sessionId };
    case "internalFailure":
      return {
        internalFailureOperation: error.detail.value.operation ?? null,
        internalFailureCause: error.detail.value.cause ?? null,
      };
    case undefined:
      return {};
  }
}

export function chatGoalSetResultFromGenerated(
  result: GoalSetResult,
  requestedSessionID?: string,
): ChatGoalSetResult {
  validateGoalSetResult(result);
  if (result.outcome.case === "error") {
    return topLevelGoalSetFailure(result.outcome.value);
  }
  if (result.outcome.case !== "success") {
    throw new ContractError("Goal Set result outcome is required.");
  }
  const success = result.outcome.value;
  const sessionID = goalSetSessionID(success.session?.sessionId, requestedSessionID);
  return {
    sessionID,
    outcome: goalSetOutcome(success),
    diagnostic: success.diagnostic === undefined ? null : goalErrorFromGenerated(success.diagnostic),
  };
}

function validateGoalSetResult(result: GoalSetResult): void {
  try {
    validate(GoalSetResultSchema, result);
  } catch {
    throw new ContractError("Goal Set result did not match the GUI contract.", [
      { code: "invalid_value", path: ["GoalSetResult"] },
    ]);
  }
}

function topLevelGoalSetFailure(error: GoalSetError): ChatGoalSetResult {
  return {
    sessionID: null,
    outcome: { kind: "rejected", error: goalErrorFromGenerated(error) },
    diagnostic: null,
  };
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

function goalSetOutcome(
  success: Extract<GoalSetResult["outcome"], { case: "success" }>["value"],
): ChatGoalSetResult["outcome"] {
  switch (success.outcome.case) {
    case "mutation": {
      const mutation = goalMutationFromGenerated(success.outcome.value);
      if (mutation.kind !== "authoritative_goal") {
        throw new ContractError("Goal Set response returned an illegal mutation result.");
      }
      return { kind: "mutation", mutation };
    }
    case "rejected":
      return { kind: "rejected", error: goalErrorFromGenerated(success.outcome.value) };
    case undefined:
      throw new ContractError("Goal Set success outcome is required.");
  }
}

export function parseGoalObservation(input: unknown): ChatGoalObservation {
  const observation = parseRpcResponse("goal.observation", goalObservationEventSchema, input).observation;
  return {
    sequence: observation.sequence,
    kind: observation.kind,
    fact: {
      goal: observation.status.goal === null ? null : chatGoal(observation.status.goal),
      availability: observation.status.availability,
    },
  };
}
