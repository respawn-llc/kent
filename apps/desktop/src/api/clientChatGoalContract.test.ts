import { create, decode } from "@app/server-api-contract";
import {
  GoalService,
  GoalSetErrorSchema,
  GoalSetResultSchema,
} from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import { FakeRpcTransport } from "@/test-support/api";
import { ApiClient } from "./client";

const sessionID = "123e4567-e89b-42d3-a456-426614174000";

describe("Desktop Chat Goal Set contract", () => {
  it.each([
    ["runtime_unavailable", { kind: "runtime_unavailable" as const }],
    [
      "internal_failure",
      { kind: "internal_failure" as const, operation: "goal.set", cause: "fixture failure" },
    ],
    ["future_code", { kind: "unknown" as const, code: "future_code", unknownFields: [] }],
  ])("returns a typed top-level Goal Set failure for %s", async (code, expected) => {
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
    ).resolves.toEqual({
      sessionID: null,
      outcome: { kind: "rejected", error: expected },
      diagnostic: null,
    });
  });

  it("preserves typed detail and generated unknown fields for a future Goal Set error", async () => {
    const futureError = decode(
      GoalSetErrorSchema,
      Uint8Array.from([
        0x0a,
        0x0b,
        ...new TextEncoder().encode("future_code"),
        0x1a,
        0x1b,
        0x0a,
        0x08,
        ...new TextEncoder().encode("goal.set"),
        0x12,
        0x0f,
        ...new TextEncoder().encode("fixture failure"),
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

    await expect(
      new ApiClient(transport).chat.setGoal({ kind: "session", sessionID }, "ship"),
    ).resolves.toEqual({
      sessionID: null,
      outcome: {
        kind: "rejected",
        error: {
          kind: "unknown",
          code: "future_code",
          internalFailureOperation: "goal.set",
          internalFailureCause: "fixture failure",
          unknownFields: futureError.$unknown,
        },
      },
      diagnostic: null,
    });
  });

  it("preserves a runtime-unavailable detail for a future Goal Set error", async () => {
    const futureError = create(GoalSetErrorSchema, {
      code: "future_code",
      detail: {
        case: "runtimeUnavailable",
        value: { sessionId: sessionID },
      },
    });
    const transport = new FakeRpcTransport([
      {
        descriptor: GoalService.method.set,
        result: create(GoalSetResultSchema, {
          outcome: { case: "error", value: futureError },
        }),
      },
    ]);

    await expect(
      new ApiClient(transport).chat.setGoal({ kind: "session", sessionID }, "ship"),
    ).resolves.toEqual({
      sessionID: null,
      outcome: {
        kind: "rejected",
        error: {
          kind: "unknown",
          code: "future_code",
          runtimeUnavailableSessionID: sessionID,
          unknownFields: [],
        },
      },
      diagnostic: null,
    });
  });
});
