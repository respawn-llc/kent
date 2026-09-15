import type { TFunction } from "i18next";

import { errorMessage, WorktreeError } from "@/api";

export function worktreeErrorMessage(error: unknown, t: TFunction): string {
  if (error instanceof WorktreeError && error.detail.kind === "internal") {
    return error.detail.cause ?? t("chat.worktree.internalFailure");
  }
  return errorMessage(error);
}
