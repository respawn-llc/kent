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
import { rpcErrorCodes } from "./rpcErrorCodes";

const error = (code: number) => new RpcError({ code, message: "diagnostic", method: "test.operation" });

it("recognizes Task context selection from the generated execution detail", () => {
  const detail = {
    case: "contextSelectionRequired" as const,
    value: create(ContextSelectionRequiredDetailsSchema, { taskId: "task-1" }),
  };
  expect(
    isTaskContextSelectionRequiredError(new TaskExecutionError(error(rpcErrorCodes.internal), detail)),
  ).toBe(true);
  expect(isTaskContextSelectionRequiredError(error(rpcErrorCodes.workflowTaskContextSelectionRequired))).toBe(
    false,
  );
  expect(isTaskContextSelectionRequiredError(new Error("context_selection_required"))).toBe(false);
});

it("recognizes typed Task and Project missing errors without parsing messages", () => {
  expect(isTaskMissingError(error(rpcErrorCodes.workflowTaskNotFound))).toBe(true);
  expect(isProjectMissingError(error(rpcErrorCodes.projectNotFound))).toBe(true);
  expect(
    isProjectMissingError(
      new WorkflowLabelError(error(rpcErrorCodes.internal), {
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
