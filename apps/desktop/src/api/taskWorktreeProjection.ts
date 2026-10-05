import {
  SetupRecoveryDisposition,
  type RegisteredFacts,
  type RetainedPreviousWorktree as GeneratedRetainedWorktree,
} from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";
import { ContractError } from "./errors";
import { WorktreeError } from "./clientWorktree";

export type WorkflowRegisteredWorktree = Readonly<{
  kent: Readonly<{ canonicalRoot: string; worktreeID: string }>;
}>;
export type RetainedPreviousWorktree = Readonly<{ worktree: WorkflowRegisteredWorktree }>;
export type WorktreeSetupRecovery = Readonly<{
  recoveryDisposition: "retry_existing" | "fresh_replacement";
  worktree: WorkflowRegisteredWorktree;
  scriptPath: string;
  diagnostic: string;
  retainedPreviousWorktree: RetainedPreviousWorktree | null;
}>;

export function taskRegisteredWorktree(facts: RegisteredFacts | undefined): WorkflowRegisteredWorktree {
  if (facts?.kent === undefined) throw new ContractError("Registered Worktree Kent facts are required.");
  return { kent: { canonicalRoot: facts.kent.canonicalRoot, worktreeID: facts.kent.worktreeId } };
}

export function taskRetainedPreviousWorktree(
  value: GeneratedRetainedWorktree | undefined,
): RetainedPreviousWorktree | null {
  return value === undefined ? null : { worktree: taskRegisteredWorktree(value.worktree) };
}

export function worktreeSetupRecovery(error: unknown): WorktreeSetupRecovery | null {
  if (!(error instanceof WorktreeError) || error.detail.kind !== "setup_retained") return null;
  const details = error.detail.details;
  let recoveryDisposition: WorktreeSetupRecovery["recoveryDisposition"];
  switch (details.recoveryDisposition) {
    case SetupRecoveryDisposition.RETRY_EXISTING:
      recoveryDisposition = "retry_existing";
      break;
    case SetupRecoveryDisposition.FRESH_REPLACEMENT:
      recoveryDisposition = "fresh_replacement";
      break;
    case SetupRecoveryDisposition.UNSPECIFIED:
    default:
      throw new ContractError("Worktree setup recovery disposition is invalid.");
  }
  return {
    recoveryDisposition,
    worktree: taskRegisteredWorktree(details.worktree),
    scriptPath: details.scriptPath,
    diagnostic: details.diagnostic,
    retainedPreviousWorktree: taskRetainedPreviousWorktree(details.retainedPreviousWorktree),
  };
}
