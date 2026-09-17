import {
  BranchCleanupOutcomeKind,
  DirtyStateKind,
  RpcError,
  WorktreeError,
  type WorktreeErrorDetail,
} from "@/api";
import type { AppServices } from "@/app-facade";

interface FixtureSettings {
  delay: number;
  switchFailure: boolean;
  previewFailure: boolean;
  deleteResult: "live" | "success" | "warning" | "blocked" | "precondition";
}

export class WorktreeShowcaseControls {
  #settings: FixtureSettings = {
    delay: 0,
    switchFailure: false,
    previewFailure: false,
    deleteResult: "live",
  };
  #modelSubmissions = 0;
  read = () => this.#settings;
  update = (change: Partial<FixtureSettings>) => {
    this.#settings = { ...this.#settings, ...change };
  };
  recordSubmission = () => {
    this.#modelSubmissions++;
  };
  modelSubmissions = () => this.#modelSubmissions;
}

// Only this isolated development composition substitutes API outcomes. Reads, selector
// authority, Create, and all feature action owners remain real.
export function worktreeShowcaseServices(base: AppServices, controls: WorktreeShowcaseControls): AppServices {
  const wait = async () => {
    const { delay } = controls.read();
    if (delay > 0) await new Promise<void>((resolve) => window.setTimeout(resolve, delay));
  };
  const fail = (detail: WorktreeErrorDetail): never => {
    throw new WorktreeError(
      new RpcError({
        method: "worktree-showcase",
        code: -32603,
        message: "Worktree QA boundary fixture",
      }),
      detail,
    );
  };
  const preventModelInput = async (): Promise<never> => {
    controls.recordSubmission();
    throw new Error("Model input is disabled in the Worktree showcase.");
  };
  return {
    ...base,
    api: {
      ...base.api,
      chat: { ...base.api.chat, steer: preventModelInput, queue: preventModelInput },
      switchWorktree: async (...args) => {
        await wait();
        if (controls.read().switchFailure)
          fail({ kind: "blocked", details: { $typeName: "kent.api.worktree.BlockedDetails" } });
        return base.api.switchWorktree(...args);
      },
      previewWorktreeDelete: async (...args) => {
        await wait();
        if (controls.read().previewFailure)
          fail({ kind: "blocked", details: { $typeName: "kent.api.worktree.BlockedDetails" } });
        return base.api.previewWorktreeDelete(...args);
      },
      deleteWorktree: async (...args) => {
        await wait();
        switch (controls.read().deleteResult) {
          case "live":
            return base.api.deleteWorktree(...args);
          case "blocked":
            return fail({ kind: "blocked", details: { $typeName: "kent.api.worktree.BlockedDetails" } });
          case "precondition":
            return fail({
              kind: "delete_precondition",
              details: {
                $typeName: "kent.api.worktree.DeletePreconditionDetails",
                dirtyState: {
                  $typeName: "kent.api.worktree.DirtyState",
                  kind: DirtyStateKind.DIRTY_STATE_DIRTY,
                  dirtyFileCount: 1,
                },
              },
            });
          case "success":
            return {
              $typeName: "kent.api.worktree.DeleteSuccess",
              cleanup: {
                $typeName: "kent.api.worktree.BranchCleanupOutcome",
                kind: BranchCleanupOutcomeKind.WORKTREE_BRANCH_CLEANUP_OUTCOME_NOT_REQUESTED,
              },
            };
          case "warning":
            return {
              $typeName: "kent.api.worktree.DeleteSuccess",
              leftoverRoot: "/fixture/retained-root",
              cleanup: {
                $typeName: "kent.api.worktree.BranchCleanupOutcome",
                kind: BranchCleanupOutcomeKind.WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED,
                branchName: "fixture-retained-branch",
              },
            };
        }
      },
    },
  };
}
