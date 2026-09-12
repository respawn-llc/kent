import { create, decode } from "@app/server-api-contract";
import {
  GoalMutationResultKind,
  GoalService,
  GoalSetErrorSchema,
  GoalSetResultSchema,
  GoalStatus,
} from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import { FakeRpcTransport } from "@/test-support/api";
import { ApiClient } from "./client";
import { ChatOperationError } from "./chatErrors";
import { chatGoalSetResultFromGenerated } from "./chatGoal";
import { ContractError } from "./errors";

const sessionID = "123e4567-e89b-42d3-a456-426614174000";

describe("Desktop Chat Goal Set contract", () => {
  it.each([
    ["runtime_unavailable", { kind: "runtime_unavailable" as const, sessionID }],
    [
      "internal_failure",
      { kind: "internal_failure" as const, operation: "goal.set", cause: "fixture failure" },
    ],
    ["future_code", { kind: "unknown" as const, code: "future_code" }],
  ])("rejects a top-level Goal Set failure for %s", async (code, expected) => {
    const transport = new FakeRpcTransport([
      {
        descriptor: GoalService.method.set,
        result: create(GoalSetResultSchema, {
          outcome: {
            case: "error",
            value:
              code === "runtime_unavailable"
                ? { code, detail: { case: "runtimeUnavailable", value: { sessionId: sessionID } } }
                : code === "internal_failure"
                  ? {
                      code,
                      detail: {
                        case: "internalFailure",
                        value: { operation: "goal.set", cause: "fixture failure" },
                      },
                    }
                  : { code },
          },
        }),
      },
    ]);

    await expect(
      new ApiClient(transport).chat.setGoal({ kind: "session", sessionID }, "ship"),
    ).rejects.toMatchObject({ detail: expected });
  });

  it("retains complete generated evidence for a future Goal Set error", async () => {
    const futureError = decode(
      GoalSetErrorSchema,
      Uint8Array.from([
        0x0a,
        0x0b,
        ...new TextEncoder().encode("future_code"),
        0x1a,
        0x1e,
        0x0a,
        0x08,
        ...new TextEncoder().encode("goal.set"),
        0x12,
        0x0f,
        ...new TextEncoder().encode("fixture failure"),
        0xa0,
        0x06,
        0x07,
        0xd8,
        0x07,
        0x07,
      ]),
    );
    const transport = new FakeRpcTransport([
      {
        descriptor: GoalService.method.set,
        result: create(GoalSetResultSchema, {
          outcome: { case: "error", value: futureError },
        }),
      },
    ]);

    const error = await new ApiClient(transport).chat
      .setGoal({ kind: "session", sessionID }, "ship")
      .catch((cause: unknown) => cause);
    expect(error).toBeInstanceOf(ChatOperationError);
    expect(error).toMatchObject({ detail: { kind: "unknown", code: "future_code" } });
    if (!(error instanceof ChatOperationError)) throw new Error("Expected a ChatOperationError.");
    expect(error.data).toEqual(futureError);
  });

  it("returns a Session-bearing rejection with the shared typed error owner", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: GoalService.method.set,
        result: create(GoalSetResultSchema, {
          outcome: {
            case: "success",
            value: {
              session: { sessionId: sessionID },
              outcome: {
                case: "rejected",
                value: {
                  code: "runtime_unavailable",
                  detail: { case: "runtimeUnavailable", value: { sessionId: sessionID } },
                },
              },
            },
          },
        }),
      },
    ]);

    const result = await new ApiClient(transport).chat.setGoal({ kind: "session", sessionID }, "ship");
    expect(result.sessionID).toBe(sessionID);
    expect(result.outcome.kind).toBe("rejected");
    if (result.outcome.kind !== "rejected") throw new Error("Expected a rejected Goal Set outcome.");
    expect(result.outcome.error).toBeInstanceOf(ChatOperationError);
    expect(result.outcome.error.detail).toEqual({ kind: "runtime_unavailable", sessionID });
    expect(result.outcome.error.data).toMatchObject({
      code: "runtime_unavailable",
      detail: {
        case: "runtimeUnavailable",
        value: { sessionId: sessionID },
      },
    });
  });

  it("returns a successful mutation with one typed post-commit diagnostic", async () => {
    const createdAt = { seconds: 10n, nanos: 0 };
    const transport = new FakeRpcTransport([
      {
        descriptor: GoalService.method.set,
        result: create(GoalSetResultSchema, {
          outcome: {
            case: "success",
            value: {
              session: { sessionId: sessionID },
              outcome: {
                case: "mutation",
                value: {
                  kind: GoalMutationResultKind.AUTHORITATIVE_GOAL,
                  goal: {
                    id: "goal-1",
                    objective: "ship",
                    status: GoalStatus.RUNTIME_GOAL_STATUS_ACTIVE,
                    createdAt,
                    updatedAt: createdAt,
                  },
                },
              },
              diagnostic: {
                code: "internal_failure",
                detail: {
                  case: "internalFailure",
                  value: { operation: "runtime.detach", cause: "release failed" },
                },
              },
            },
          },
        }),
      },
    ]);

    const result = await new ApiClient(transport).chat.setGoal({ kind: "session", sessionID }, "ship");
    expect(result.sessionID).toBe(sessionID);
    expect(result.outcome.kind).toBe("mutation");
    if (result.outcome.kind !== "mutation") throw new Error("Expected a mutation Goal Set outcome.");
    expect(result.outcome.diagnostic).toBeInstanceOf(ChatOperationError);
    expect(result.outcome.diagnostic?.detail).toEqual({
      kind: "internal_failure",
      operation: "runtime.detach",
      cause: "release failed",
    });
  });

  it("rejects malformed known Goal Set details before conversion", () => {
    const malformed = create(GoalSetResultSchema, {
      outcome: {
        case: "success",
        value: {
          session: { sessionId: sessionID },
          outcome: {
            case: "rejected",
            value: { code: "runtime_unavailable" },
          },
        },
      },
    });

    expect(() => chatGoalSetResultFromGenerated(malformed)).toThrow(ContractError);
  });
});
