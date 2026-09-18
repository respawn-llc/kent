import type { TFunction } from "i18next";

import { errorMessage, SelectorErrorKind, WorktreeError } from "@/api";

export function worktreeErrorMessage(error: unknown, t: TFunction): string {
  if (!(error instanceof WorktreeError)) return errorMessage(error);
  const detail = error.detail;
  switch (detail.kind) {
    case "internal":
      return detail.cause ?? t("chat.worktree.internalFailure");
    case "delete_partial":
      return [
        t("chat.worktree.deletePartial", {
          sessionCount: detail.details.retargetedSessions.toString(),
        }),
        detail.details.blocked === undefined
          ? detail.details.diagnostic
          : blockedMessage(detail.details.blocked, t),
      ].join("\n");
    case "create":
      return detail.diagnostic;
    case "setup_retained":
      return detail.details.diagnostic;
    case "selector":
      return selectorMessage(detail.details, t);
    case "blocked":
      return blockedMessage(detail.details, t);
    case "capacity":
      return t("chat.worktree.pendingCapacity");
    case "delete_precondition":
      return t("chat.worktree.deleteChanged");
  }
}

function blockedMessage(
  details: Extract<WorktreeError["detail"], { kind: "blocked" }>["details"],
  t: TFunction,
): string {
  const blockers = details.activeSessions;
  if (blockers === undefined) return t("chat.worktree.operationBlocked");
  return [
    t("chat.worktree.activeSessionsBlocked"),
    ...blockers.sessions.map((session) =>
      session.name === undefined ? session.sessionId : `${session.name} (${session.sessionId})`,
    ),
    ...(blockers.hasMore ? [t("chat.worktree.moreBlockingSessions")] : []),
  ].join("\n");
}

function selectorMessage(
  details: Extract<WorktreeError["detail"], { kind: "selector" }>["details"],
  t: TFunction,
): string {
  switch (details.kind) {
    case SelectorErrorKind.WORKTREE_SELECTOR_ERROR_KIND_NOT_FOUND:
      return t("chat.worktree.selectorNotFound", { target: details.input });
    case SelectorErrorKind.WORKTREE_SELECTOR_ERROR_KIND_AMBIGUOUS:
      return t("chat.worktree.selectorAmbiguous", {
        target: details.input,
        candidates: details.candidates.map((candidate) => candidate.selector).join(", "),
      });
    case SelectorErrorKind.WORKTREE_SELECTOR_ERROR_KIND_UNAVAILABLE:
      return t("chat.worktree.selectorUnavailable", { target: details.input });
    case SelectorErrorKind.WORKTREE_SELECTOR_ERROR_KIND_UNSPECIFIED:
      throw new Error("Worktree selector failure requires a valid reason");
  }
}
