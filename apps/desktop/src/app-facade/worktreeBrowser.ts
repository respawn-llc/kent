import type { QueryClient } from "@tanstack/react-query";
import type {
  ApiService,
  ChatTranscriptPayloadByKind,
  WorktreeDeletePreviewOperation,
  WorktreeSwitch,
} from "@/api";
import { replaceWorktreeListRead } from "./worktreeQueries";

export type WorktreeBrowserAction = Readonly<{
  sessionID: string;
  returnFocus: HTMLElement;
}> &
  (
    | Readonly<{ kind: "create" }>
    | Readonly<{ kind: "switch"; operation: WorktreeSwitch }>
    | Readonly<{ kind: "delete"; operation: Readonly<WorktreeDeletePreviewOperation> }>
  );

export type WorktreeBrowserActions = (action: WorktreeBrowserAction) => void;

export function worktreeTransitionOutcomeHandler(client: QueryClient, api: ApiService, sessionID: string) {
  return (outcome: ChatTranscriptPayloadByKind["worktree_transition_outcome"]) => {
    if (outcome.State === "completed") void replaceWorktreeListRead(client, api, sessionID);
  };
}
