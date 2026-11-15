import type { TFunction } from "i18next";
import { BranchCleanupOutcomeKind, type BranchCleanupOutcome } from "@/api";

export function branchCleanupWarnings(cleanup: BranchCleanupOutcome | undefined, t: TFunction): string[] {
  if (cleanup?.kind !== BranchCleanupOutcomeKind.WORKTREE_BRANCH_CLEANUP_OUTCOME_RETAINED) return [];
  const warnings = [t("chat.worktree.branchRetained", { branch: cleanup.branchName })];
  if (cleanup.diagnostic !== undefined) warnings.push(cleanup.diagnostic);
  return warnings;
}
