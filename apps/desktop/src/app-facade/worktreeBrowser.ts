import type { QueryClient } from "@tanstack/react-query";
import type { ApiService, ChatTranscriptPayloadByKind, WorktreeScope } from "@/api";
import { replaceWorktreeListRead } from "./worktreeQueries";
import { queryKeys } from "./queryKeys";
import type { SidebarShellController } from "./sidebarContext";
import type { StatusController } from "./statusContextValue";
import type { TFunction } from "i18next";

export function createRefreshOpenWorktreeList(
  client: QueryClient,
  api: ApiService,
  currentSurface: SidebarShellController["currentSurface"],
) {
  return async (scope: WorktreeScope) => {
    const surface = currentSurface();
    const inventory = client.getQueryCache().find({ queryKey: queryKeys.worktreeList(scope), exact: true });
    if (
      surface?.kind === "worktree" &&
      surface.page === "list" &&
      inventory !== undefined &&
      inventory.getObserversCount() > 0
    ) {
      await replaceWorktreeListRead(client, api, scope);
    }
  };
}

export function worktreeTransitionOutcomeHandler(
  refreshOpenWorktreeList: (scope: WorktreeScope) => void | Promise<void>,
  sessionID: string,
  push: StatusController["push"],
  t: TFunction,
) {
  return (outcome: ChatTranscriptPayloadByKind["worktree_transition_outcome"]) => {
    void refreshOpenWorktreeList({ kind: "session", sessionID });
    if (outcome.State === "failed" && outcome.Failure != null) {
      push({
        id: outcome.OperationID,
        tone: "danger",
        title: t("chat.worktree.title"),
        body: outcome.Failure.Detail,
      });
    }
  };
}
