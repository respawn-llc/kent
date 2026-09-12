import { useCallback, useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { replaceWorktreeListRead, useAppServices, worktreeListQueryOptions } from "@/app-facade";
import type { ChatExecutionTarget } from "@/api";

export function useWorktreeList(sessionID: string, target?: ChatExecutionTarget | null) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const query = useQuery({
    ...worktreeListQueryOptions(api, sessionID),
    enabled: false,
  });
  const refresh = useCallback(() => {
    void replaceWorktreeListRead(client, api, sessionID);
  }, [api, client, sessionID]);
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
