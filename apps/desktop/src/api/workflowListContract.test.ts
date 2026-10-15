import { create, validate } from "@app/server-api-contract";
import { ListRequestSchema } from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";

describe("Workflow List Project scope", () => {
  it("accepts absence or substantive unpadded identity only", () => {
    for (const projectId of [undefined, "project-1", "\uFEFF", "", "\u0085", " project-1 "]) {
      const request = create(ListRequestSchema, { projectId });
      if (projectId === undefined || projectId === "project-1" || projectId === "\uFEFF") {
        expect(() => validate(ListRequestSchema, request)).not.toThrow();
      } else {
        expect(() => validate(ListRequestSchema, request)).toThrow();
      }
    }
  });
});
