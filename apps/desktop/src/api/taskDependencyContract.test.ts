import { create, validate } from "@app/server-api-contract";
import {
  CreateRequestSchema,
  DependencyRole,
} from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";

describe("Task dependency intent limits", () => {
  it("limits each role independently while allowing fifty of each", () => {
    for (const { blockers, blocked } of [
      { blockers: 50, blocked: 50 },
      { blockers: 51, blocked: 0 },
      { blockers: 0, blocked: 51 },
    ]) {
      const request = create(CreateRequestSchema, {
        projectId: "project-1",
        title: "Task",
        dependencyIntents: [
          ...Array.from({ length: blockers }, (_, index) => ({
            relatedTaskId: `blocker-${String(index)}`,
            newTaskRole: DependencyRole.BLOCKER,
          })),
          ...Array.from({ length: blocked }, (_, index) => ({
            relatedTaskId: `blocked-${String(index)}`,
            newTaskRole: DependencyRole.BLOCKED,
          })),
        ],
      });
      if (blockers <= 50 && blocked <= 50) {
        expect(() => validate(CreateRequestSchema, request)).not.toThrow();
      } else {
        expect(() => validate(CreateRequestSchema, request)).toThrow();
      }
    }
  });
});
