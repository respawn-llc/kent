import { create } from "@app/server-api-contract";
import * as lifecycle from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { ExecutionTargetMode } from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import {
  ExecutionTargetUnavailableCause,
  LockedExecutionTargetCause,
} from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import { ApiClient } from "./client";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";

function moveClient(response: lifecycle.MoveSuccess) {
  return new ApiClient(
    new FakeRpcTransport([
      {
        descriptor: lifecycle.TaskLifecycleService.method.move,
        result: create(lifecycle.MoveResultSchema, { outcome: { case: "success", value: response } }),
      },
    ]),
    unexpectedProjectOverflow,
  );
}

function previewClient(response: lifecycle.MovePreviewSuccess) {
  return new ApiClient(
    new FakeRpcTransport([
      {
        descriptor: lifecycle.TaskLifecycleService.method.previewMove,
        result: create(lifecycle.MovePreviewResultSchema, { outcome: { case: "success", value: response } }),
      },
    ]),
    unexpectedProjectOverflow,
  );
}

describe("task lifecycle client", () => {
  it("uses Current Node responses and sends move selections", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: lifecycle.TaskLifecycleService.method.start,
        result: create(lifecycle.StartResultSchema, {
          outcome: {
            case: "success",
            value: {
              outcome: {
                case: "applied",
                value: { currentNodes: [{ nodeId: "node-1", sessionId: "session-1" }] },
              },
            },
          },
        }),
      },
      {
        descriptor: lifecycle.TaskLifecycleService.method.move,
        result: create(lifecycle.MoveResultSchema, {
          outcome: {
            case: "success",
            value: {
              outcome: { case: "applied", value: { currentNodes: [{ nodeId: "node-2" }] } },
            },
          },
        }),
      },
      {
        descriptor: lifecycle.TaskLifecycleService.method.approve,
        result: create(lifecycle.ApproveResultSchema, {
          outcome: {
            case: "success",
            value: {
              outcome: {
                case: "applied",
                value: {
                  taskId: "task-1",
                  currentNodes: [{ nodeId: "node-3", transitionBranchKey: "branch-a" }],
                },
              },
            },
          },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(client.startTask({ taskID: "task-1" })).resolves.toMatchObject({
      outcome: "applied",
      applied: { currentNodes: [{ nodeID: "node-1" }] },
    });
    await expect(
      client.moveTask({ taskID: "task-1", targetNodeID: "node-2", branchName: "task-reopened" }),
    ).resolves.toMatchObject({
      outcome: "applied",
      applied: { currentNodes: [{ nodeID: "node-2" }] },
    });
    await expect(client.approveApproval("approval-1")).resolves.toMatchObject({
      outcome: "applied",
      applied: { taskID: "task-1", currentNodes: [{ nodeID: "node-3" }] },
    });
    expect(transport.descriptorCalls[1]?.request).toMatchObject({
      branchName: "task-reopened",
      targetNodeId: "node-2",
    });
    expect(transport.descriptorCalls[2]).toMatchObject({
      descriptor: lifecycle.TaskLifecycleService.method.approve,
      request: create(lifecycle.ApproveRequestSchema, { approvalId: "approval-1" }),
      options: { timeoutMs: null },
    });
  });

  it("maps typed execution-target selection requirements", async () => {
    for (const mode of [
      ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_HEAD,
      ExecutionTargetMode.WORKFLOW_EXECUTION_TARGET_MODE_DEFAULT_BRANCH,
    ]) {
      const client = moveClient(
        create(lifecycle.MoveSuccessSchema, {
          outcome: {
            case: "selectionRequired",
            value: {
              reason: {
                case: "configuredTargetUnavailable",
                value: { mode, cause: ExecutionTargetUnavailableCause.GIT_FAILURE },
              },
            },
          },
        }),
      );
      await expect(client.moveTask({ taskID: "task-1", targetNodeID: "node-2" })).resolves.toMatchObject({
        selectionRequired: { configuredTarget: { requestedRef: null }, unavailableCause: "git_failure" },
      });
    }
    const original = moveClient(
      create(lifecycle.MoveSuccessSchema, {
        outcome: {
          case: "selectionRequired",
          value: {
            reason: {
              case: "originalTargetUnavailable",
              value: { cause: LockedExecutionTargetCause.MISSING_BRANCH },
            },
          },
        },
      }),
    );
    await expect(original.moveTask({ taskID: "task-1", targetNodeID: "node-2" })).resolves.toEqual({
      outcome: "selection_required",
      selectionRequired: { reason: "original_target_unavailable", originalTargetCause: "missing_branch" },
    });
    const confirmation = moveClient(
      create(lifecycle.MoveSuccessSchema, {
        outcome: { case: "dependencyConfirmationRequired", value: { unsatisfiedDependencyCount: 2 } },
      }),
    );
    await expect(confirmation.moveTask({ taskID: "task-1", targetNodeID: "node-2" })).resolves.toEqual({
      outcome: "dependency_confirmation_required",
      unsatisfiedDependencyCount: 2,
    });
  });

  it("maps an already-resumed Task response as a no-op", async () => {
    const client = new ApiClient(
      new FakeRpcTransport([
        {
          descriptor: lifecycle.TaskLifecycleService.method.resume,
          result: create(lifecycle.ResumeResultSchema, {
            outcome: {
              case: "success",
              value: {
                outcome: {
                  case: "noOp",
                  value: { currentNodes: [{ nodeId: "node-1", sessionId: "session-1" }] },
                },
              },
            },
          }),
        },
      ]),
      unexpectedProjectOverflow,
    );
    await expect(client.resumeTask({ taskID: "task-1" })).resolves.toMatchObject({
      outcome: "no_op",
      noOp: { currentNodes: [{ nodeID: "node-1", sessionID: "session-1" }] },
    });
  });

  it("maps Manual Move preview outcomes and same-current no-op", async () => {
    const unchanged = moveClient(
      create(lifecycle.MoveSuccessSchema, {
        outcome: { case: "noOp", value: { currentNodes: [{ nodeId: "node-1" }] } },
      }),
    );
    await expect(unchanged.moveTask({ taskID: "task-1", targetNodeID: "node-1" })).resolves.toMatchObject({
      outcome: "no_op",
      noOp: { currentNodes: [{ nodeID: "node-1" }] },
    });
    for (const response of [
      create(lifecycle.MovePreviewSuccessSchema, { outcome: { case: "direct", value: {} } }),
      create(lifecycle.MovePreviewSuccessSchema, {
        outcome: { case: "blocked", value: { reason: lifecycle.MovePreviewBlocker.LIFECYCLE_CONFLICT } },
      }),
    ]) {
      const result = await previewClient(response).previewMoveTask("task-1", "node-2");
      expect(result.outcome).toBe(response.outcome.case);
    }
  });

  it("preserves whitespace in resolved Manual Move values", async () => {
    const resolvedValue = "  indented code\n ";
    const client = previewClient(
      create(lifecycle.MovePreviewSuccessSchema, {
        outcome: {
          case: "transition",
          value: {
            choices: [
              {
                transitionKey: "next",
                label: "Next",
                sourceNodeDisplayName: "Plan",
                requiredValues: [
                  { nodeKey: "plan", outputName: "summary", description: "Summary", resolvedValue },
                ],
              },
            ],
          },
        },
      }),
    );
    await expect(client.previewMoveTask("task-1", "node-2")).resolves.toMatchObject({
      transition: { choices: [{ requiredValues: [{ resolvedValue }] }] },
    });
  });

  it("rejects blank Manual Move descriptions", async () => {
    const client = previewClient(
      create(lifecycle.MovePreviewSuccessSchema, {
        outcome: {
          case: "transition",
          value: {
            choices: [
              {
                transitionKey: "next",
                label: "Next",
                sourceNodeDisplayName: "Plan",
                requiredValues: [{ nodeKey: "plan", outputName: "summary", description: " \t" }],
              },
            ],
          },
        },
      }),
    );
    await expect(client.previewMoveTask("task-1", "node-2")).rejects.toThrow();
  });

  it("rejects empty applied Current Nodes", async () => {
    const client = moveClient(
      create(lifecycle.MoveSuccessSchema, { outcome: { case: "applied", value: {} } }),
    );
    await expect(client.moveTask({ taskID: "task-1", targetNodeID: "node-2" })).rejects.toThrow();
  });
});
