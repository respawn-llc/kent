import { describe, expect, it } from "bun:test";
import { create, validate } from "@app/server-api-contract";
import { SessionSelfTargetDetailsSchema } from "@app/server-api-contract/gen/kent/api/shared/session_selector_pb";

describe("Self-target rejections", () => {
  it("requires a resolved Session identity", () => {
    const sessionId = "00000000-0000-4000-8000-000000000001";
    const canonical = create(SessionSelfTargetDetailsSchema, {
      sessionTarget: { stage: { case: "canonicalSessionId", value: sessionId } },
    });
    const projected = create(SessionSelfTargetDetailsSchema, {
      sessionTarget: { stage: { case: "projected", value: { sessionId } } },
    });
    const unresolved = create(SessionSelfTargetDetailsSchema, {
      sessionTarget: { stage: { case: "unresolved", value: {} } },
    });
    expect(() => validate(SessionSelfTargetDetailsSchema, canonical)).not.toThrow();
    expect(() => validate(SessionSelfTargetDetailsSchema, projected)).not.toThrow();
    expect(() => validate(SessionSelfTargetDetailsSchema, unresolved)).toThrow();
  });
});
