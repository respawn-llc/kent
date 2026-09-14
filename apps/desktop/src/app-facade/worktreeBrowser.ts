import type { QueryClient } from "@tanstack/react-query";
import type { ApiService, ChatTranscriptPayloadByKind } from "@/api";
import { replaceWorktreeListRead } from "./worktreeQueries";
import type { SidebarShellController } from "./sidebarContext";
import type { StatusController } from "./statusContextValue";
import type { TFunction } from "i18next";

export function createRefreshOpenWorktreeList(
  client: QueryClient,
  api: ApiService,
  currentSurface: SidebarShellController["currentSurface"],
) {
  return (sessionID: string) => {
    const surface = currentSurface();
    if (surface?.kind === "worktree" && surface.page === "list" && surface.sessionID === sessionID) {
      void replaceWorktreeListRead(client, api, sessionID);
    }
  };
}

export function worktreeTransitionOutcomeHandler(
  refreshOpenWorktreeList: (sessionID: string) => void,
  sessionID: string,
  push: StatusController["push"],
  t: TFunction,
) {
  return (outcome: ChatTranscriptPayloadByKind["worktree_transition_outcome"]) => {
    refreshOpenWorktreeList(sessionID);
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
