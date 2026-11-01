import { expect, it } from "bun:test";
import { create, encode, operationName } from "@app/server-api-contract";
import {
  GoalService,
  GoalSetErrorSchema,
  GoalSetResultSchema,
} from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import { ResultSchema } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import { completeDescriptorResponse } from "./descriptorRpc";
import { ContractError } from "./errors";

it("checks declared error details inside successful results while preserving unknown codes", () => {
  const method = GoalService.method.set;
  for (const test of [
    { code: "auth_required", detail: "authRequired", accepted: true },
    { code: "auth_required", detail: "internalFailure", accepted: false },
    { code: "auth_required", detail: undefined, accepted: false },
    { code: "future_error", detail: "internalFailure", accepted: true },
    { code: "future_error", detail: undefined, accepted: true },
  ] as const) {
    const failure = create(GoalSetErrorSchema, {
      code: test.code,
      detail:
        test.detail === "authRequired"
          ? { case: "authRequired", value: {} }
          : test.detail === "internalFailure"
            ? { case: "internalFailure", value: {} }
            : { case: undefined },
    });
    const result = create(GoalSetResultSchema, {
      outcome: {
        case: "success",
        value: {
          session: { sessionId: "00000000-0000-4000-8000-000000000001" },
          outcome: {
            case: "rejected",
            value: failure,
          },
        },
      },
    });
    const receive = () =>
      completeDescriptorResponse(method, "nested", {
        kind: "result",
        correlation: "nested",
        result: create(ResultSchema, {
          operation: operationName(method),
          correlation: "nested",
          payload: encode(method.output, result),
        }),
      });
    if (test.accepted) {
      expect(receive()).toMatchObject({
        outcome: { case: "success", value: { outcome: { case: "rejected", value: { code: test.code } } } },
      });
    } else {
      expect(receive).toThrow(ContractError);
    }
  }
});
