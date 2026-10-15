import { create, validate } from "@app/server-api-contract";
import {
  CompleteRequestSchema,
  CompleteActorKind,
  MoveRequestSchema,
} from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";

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

describe("Task move commentary", () => {
  it("limits raw UTF-8 bytes before trimming", () => {
    for (const overflow of [false, true]) {
      const request = create(MoveRequestSchema, {
        taskId: "task-1",
        targetNodeId: "node-1",
        commentary: "é".repeat(32768) + (overflow ? " " : ""),
      });
      if (overflow) {
        expect(() => validate(MoveRequestSchema, request)).toThrow();
      } else {
        expect(() => validate(MoveRequestSchema, request)).not.toThrow();
      }
    }
  });
});
