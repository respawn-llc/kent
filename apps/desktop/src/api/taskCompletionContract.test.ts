import { create, validate } from "@app/server-api-contract";
import { CompleteRequestSchema, CompleteActorKind } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";

describe("Task completion commentary", () => {
  it("limits UTF-8 bytes rather than UTF-16 units", () => {
    for (const overflow of [false, true]) {
      const request = create(CompleteRequestSchema, {
        taskId: "task-1",
        actorKind: CompleteActorKind.USER,
        force: true,
        commentary: "é".repeat(32768) + (overflow ? "x" : ""),
      });
      if (overflow) {
        expect(() => validate(CompleteRequestSchema, request)).toThrow();
      } else {
        expect(() => validate(CompleteRequestSchema, request)).not.toThrow();
      }
    }
  });
});
