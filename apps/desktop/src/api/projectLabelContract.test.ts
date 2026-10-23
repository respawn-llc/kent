import { describe, expect, it } from "bun:test";
import { create, validate } from "@app/server-api-contract";
import {
  ProjectLabelCatalogRequestSchema,
  ProjectLabelCreateRequestSchema,
  ProjectLabelDeleteRequestSchema,
  ProjectLabelRenameRequestSchema,
  ProjectLabelReorderRequestSchema,
} from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";

describe("Project Label request Project scope", () => {
  it("requires substantive Project identity without imposing exact trim", () => {
    for (const projectId of [" \t", "\u0085", " project-1 ", "\uFEFF"]) {
      for (const schema of [
        ProjectLabelCatalogRequestSchema,
        ProjectLabelCreateRequestSchema,
        ProjectLabelDeleteRequestSchema,
        ProjectLabelRenameRequestSchema,
        ProjectLabelReorderRequestSchema,
      ]) {
        const request = create(schema, {
          projectId,
          name: "Label",
          labelId: "11111111-1111-4111-8111-111111111111",
        });
        if (projectId === " project-1 " || projectId === "\uFEFF") {
          expect(() => validate(schema, request)).not.toThrow();
        } else {
          expect(() => validate(schema, request)).toThrow();
        }
      }
    }
  });
});
