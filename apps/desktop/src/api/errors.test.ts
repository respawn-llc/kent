import { create } from "@app/server-api-contract";
import { ContextSelectionRequiredDetailsSchema } from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import {
  isProjectMissingError,
  isTaskMissingError,
  isTaskContextSelectionRequiredError,
  RpcError,
  TaskExecutionError,
  WorkflowLabelError,
} from "./errors";

const error = (code: string) => new RpcError({ code, message: "diagnostic", method: "test.operation" });

it("recognizes Task context selection from the generated execution detail", () => {
  const detail = {
    case: "contextSelectionRequired" as const,
    value: create(ContextSelectionRequiredDetailsSchema, { taskId: "task-1" }),
  };
  expect(isTaskContextSelectionRequiredError(new TaskExecutionError(error("internal_failure"), detail))).toBe(
    true,
  );
  expect(isTaskContextSelectionRequiredError(error("context_selection_required"))).toBe(false);
  expect(isTaskContextSelectionRequiredError(new Error("context_selection_required"))).toBe(false);
});

it("recognizes typed Task and Project missing errors without parsing messages", () => {
  expect(isTaskMissingError(error("task_not_found"))).toBe(true);
  expect(isProjectMissingError(error("project_not_found"))).toBe(true);
  expect(
    isProjectMissingError(
      new WorkflowLabelError(error("internal_failure"), {
        reason: "project_not_found",
        projectID: "project-1",
        taskID: null,
        labelID: null,
        field: null,
        limit: null,
      }),
    ),
  ).toBe(true);
  expect(isProjectMissingError(new Error("project_not_found"))).toBe(false);
});
