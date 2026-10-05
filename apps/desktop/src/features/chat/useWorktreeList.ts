import { useCallback, useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  replaceWorktreeListRead,
  useAppServices,
  worktreeListQueryOptions,
  type WorktreeListReadOwner,
} from "@/app-facade";
import type { ChatExecutionTarget } from "@/api";

export function useWorktreeList(
  sessionID: string | null,
  target?: ChatExecutionTarget | null,
  owner: WorktreeListReadOwner = "sidebar",
) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const query = useQuery({
    ...worktreeListQueryOptions(api, sessionID, owner),
    enabled: false,
  });
  const refresh = useCallback(() => {
    if (sessionID === null) return;
    void replaceWorktreeListRead(client, api, sessionID, owner);
  }, [api, client, sessionID, owner]);
  useEffect(() => {
    refresh();
  }, [
    refresh,
    target?.workspaceID,
    target?.worktree?.ID,
    target?.worktree?.Name,
    target?.worktree?.Availability,
  ]);
  return { ...query, refresh };
}
