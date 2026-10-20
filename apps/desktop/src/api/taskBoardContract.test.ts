import { describe, expect, it } from "bun:test";
import { create, validate } from "@app/server-api-contract";
import { BoardGetRequestSchema } from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";

describe("Task Board Project", () => {
  it("requires substantive Project text without requiring exact trim", () => {
    for (const projectId of [" \t", "\u0085", " project-1 ", "\uFEFF"]) {
      const request = create(BoardGetRequestSchema, {
        projectId,
        labelFilter: { filter: { case: "none", value: {} } },
      });
      if (projectId === " project-1 " || projectId === "\uFEFF") {
        expect(() => validate(BoardGetRequestSchema, request)).not.toThrow();
      } else {
        expect(() => validate(BoardGetRequestSchema, request)).toThrow();
      }
    }
  });
});
