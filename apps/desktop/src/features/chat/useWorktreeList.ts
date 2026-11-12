import { useCallback, useEffect } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import {
  replaceWorktreeListRead,
  useAppServices,
  worktreeListQueryOptions,
  type WorktreeListReadOwner,
} from "@/app-facade";
import type { ChatExecutionTarget, WorktreeScope } from "@/api";

export function useWorktreeList(
  scope: WorktreeScope | null,
  target?: ChatExecutionTarget | null,
  owner: WorktreeListReadOwner = "sidebar",
) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const query = useQuery({
    ...worktreeListQueryOptions(api, scope, owner),
    enabled: false,
  });
  const refresh = useCallback(() => {
    if (scope === null) return;
    void replaceWorktreeListRead(client, api, scope, owner);
  }, [api, client, scope, owner]);
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
