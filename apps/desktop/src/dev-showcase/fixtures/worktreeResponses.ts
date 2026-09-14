import { create } from "@app/server-api-contract";
import {
  BranchCleanupOutcomeKind,
  CreateTargetResolutionKind,
  CreateTargetResolveSuccessSchema,
  DeletePreconditionDetailsSchema,
  DeleteSuccessSchema,
  DirtyStateKind,
  ScheduledAcknowledgementSchema,
  SetupRetainedDetailsSchema,
} from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";
import { RpcError, WorktreeError } from "@/api";
import { worktreeBrowserFixtureEntry } from "./api";

export function worktreeResolutionFixture(input: string, kind: CreateTargetResolutionKind) {
  return create(CreateTargetResolveSuccessSchema, {
    resolution: {
      input,
      kind,
      resolvedRef:
        kind === CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH
          ? undefined
          : "abc123",
    },
  });
}

export function worktreeAcknowledgementFixture() {
  return create(ScheduledAcknowledgementSchema, { operationId: crypto.randomUUID() });
}

export function worktreeDeleteSuccessFixture(
  options: Readonly<{
    retainedBranch?: string | undefined;
    diagnostic?: string;
    leftoverRoot?: string;
  }> = {},
) {
  return create(DeleteSuccessSchema, {
    cleanup:
      options.retainedBranch === undefined
        ? { kind: BranchCleanupOutcomeKind.WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_REQUESTED }
        : {
            kind: BranchCleanupOutcomeKind.WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED,
            branchName: options.retainedBranch,
            diagnostic: options.diagnostic,
          },
    leftoverRoot: options.leftoverRoot,
  });
}

export function worktreeErrorFixture(
  kind: "setup" | "base_ref" | "form" | "delete_precondition" | "blocked",
  diagnostic = "Fixture failure",
) {
  const error = new RpcError({ code: 1, method: "worktree", message: diagnostic });
  switch (kind) {
    case "base_ref":
    case "form":
      return new WorktreeError(error, { kind: "create", owner: kind, diagnostic });
    case "blocked":
      return new WorktreeError(error, { kind: "blocked" });
    case "delete_precondition":
      return new WorktreeError(error, {
        kind,
        details: create(DeletePreconditionDetailsSchema, {
          dirtyState: { kind: DirtyStateKind.DIRTY_STATE_DIRTY, dirtyFileCount: 1 },
        }),
      });
    case "setup": {
      const topology = worktreeBrowserFixtureEntry("registered", false).topology?.topology;
      if (topology?.case !== "registered") throw new Error("Registered fixture required");
      return new WorktreeError(error, {
        kind: "setup_retained",
        details: create(SetupRetainedDetailsSchema, {
          worktree: topology.value,
          scriptPath: "setup.sh",
          diagnostic,
        }),
      });
    }
  }
}
