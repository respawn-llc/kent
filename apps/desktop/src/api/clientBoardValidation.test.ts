import { expect, it } from "bun:test";
import { create } from "@app/server-api-contract";
import * as wf from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import * as taskRead from "@app/server-api-contract/gen/kent/api/workflow_task/read_pb";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
import { ApiClient } from "./client";
import { canonicalBoardFilter } from "./workflowBoardFilters";

it.each([
  { blocking: [], valid: true },
  { blocking: [false], valid: true },
  { blocking: [false, true], valid: false },
])("presents board workflow availability from server diagnostics: %j", async ({ blocking, valid }) => {
  const selectedWorkflow = create(taskRead.WorkflowPickerItemSchema, {
    workflowId: "11111111-1111-4111-8111-111111111111",
    displayName: "Workflow",
    version: 1n,
    validationErrors: blocking.map((blocksContext) => ({
      code: wf.ValidationErrorCode.INVALID_NODE_KIND,
      message: "Invalid node kind",
      blocksContext,
    })),
  });
  const response = create(taskRead.BoardGetResultSchema, {
    outcome: {
      case: "success",
      value: {
        board: {
          projectId: "project-1",
          project: {
            projectKey: "proj",
            displayName: "Project",
            defaultWorkspaceId: "workspace-1",
            attachedWorkspaceCount: 1,
          },
          selectedWorkflow,
          workflows: [selectedWorkflow],
        },
      },
    },
  });
  const client = new ApiClient(
    new FakeRpcTransport([{ descriptor: taskRead.BoardReadService.method.get, result: response }]),
    unexpectedProjectOverflow,
  );
  const board = await client.getBoard(
    "project-1",
    selectedWorkflow.workflowId,
    canonicalBoardFilter({ labelFilter: { kind: "none" }, dependencyFilter: null }),
  );
  expect(board.selectedWorkflow?.validForTaskCreation).toBe(valid);
  expect(board.workflows[0]?.validForTaskCreation).toBe(valid);
  expect(board.selectedWorkflow?.validationErrors.map((error) => error.blocksContext)).toEqual([...blocking]);
});
