import { describe, expect, it } from "bun:test";
import { create, validate } from "@app/server-api-contract";
import { ApproveRequestSchema } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";

describe("Workflow Approval identity boundary", () => {
  it("accepts only canonical UUID-v4 request identities", () => {
    const canonical = "11111111-1111-4111-8111-111111111111";
    expect(() =>
      validate(ApproveRequestSchema, create(ApproveRequestSchema, { approvalId: canonical })),
    ).not.toThrow();
    for (const approvalId of [
      "",
      "approval-1",
      ` ${canonical}`,
      `${canonical}\t`,
      "11111111-1111-1111-8111-111111111111",
    ]) {
      expect(() => validate(ApproveRequestSchema, create(ApproveRequestSchema, { approvalId }))).toThrow();
    }
  });
});
