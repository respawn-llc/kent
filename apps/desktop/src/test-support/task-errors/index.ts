import { create } from "@app/server-api-contract";
import {
  SetupRetainedDetailsSchema,
  SetupRecoveryDisposition,
} from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";
import {
  TaskLifecycleService,
  MoveResultSchema,
  InitialBranchErrorReason,
} from "@app/server-api-contract/gen/kent/api/workflow_task/lifecycle_pb";
import { RpcError, WorktreeError } from "@/api";
import { createTestServices } from "../app-services";

export function retainedSetupError(
  recoveryDisposition: "retry_existing" | "fresh_replacement" = "retry_existing",
) {
  const root = "/worktrees/task-1";
  return new WorktreeError(
    new RpcError({ code: "worktree_setup_retained", message: "setup failed", method: "test.move" }),
    {
      kind: "setup_retained",
      details: create(SetupRetainedDetailsSchema, {
        recoveryDisposition:
          recoveryDisposition === "retry_existing"
            ? SetupRecoveryDisposition.RETRY_EXISTING
            : SetupRecoveryDisposition.FRESH_REPLACEMENT,
        scriptPath: "/repo/setup.sh",
        diagnostic: "setup failed twice",
        worktree: {
          git: {
            canonicalRoot: root,
            headObject: "abc",
            detached: false,
            bare: false,
            isMainWorktree: false,
            pathAvailable: true,
          },
          kent: {
            worktreeId: "worktree-1",
            canonicalRoot: root,
            displayName: "KENT-453",
            managed: true,
            createdBranch: true,
          },
        },
      }),
    },
  );
}

export async function taskMoveBranchCollision(): Promise<never> {
  const services = createTestServices([
    {
      descriptor: TaskLifecycleService.method.move,
      result: create(MoveResultSchema, {
        outcome: {
          case: "error",
          value: {
            code: "initial_branch",
            detail: {
              case: "initialBranch",
              value: {
                reason: InitialBranchErrorReason.LOCAL_COLLISION,
                branchName: "taken",
                ref: "refs/heads/taken",
              },
            },
          },
        },
      }),
    },
  ]);
  await services.api.moveTask({ taskID: "task-1", targetNodeID: "node-1" });
  throw new Error("Expected branch collision.");
}
