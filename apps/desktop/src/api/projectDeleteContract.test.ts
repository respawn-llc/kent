import { describe, expect, it } from "bun:test";
import { create, validate } from "@app/server-api-contract";
import { DeleteProjectSuccessSchema } from "@app/server-api-contract/gen/kent/api/project/project_pb";

describe("Project Delete outcome blockers", () => {
  it("requires blockers exactly when not deleted", () => {
    for (const deleted of [false, true]) {
      for (const blocked of [false, true]) {
        const response = create(DeleteProjectSuccessSchema, {
          projectId: "project-1",
          deleted,
          blockers: blocked ? [{ code: "active_sessions", count: 1 }] : [],
        });
        if (deleted !== blocked) {
          expect(() => validate(DeleteProjectSuccessSchema, response)).not.toThrow();
        } else {
          expect(() => validate(DeleteProjectSuccessSchema, response)).toThrow();
        }
      }
    }
  });
});
