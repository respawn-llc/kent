import { z } from "zod";

import { parseRpcResponse } from "./clientParse";
import { goalSchema, type runtimeStatusSchema } from "./chatSchemas";
import { type goalStatusSchema } from "./chatTranscriptFactSchemas";

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
