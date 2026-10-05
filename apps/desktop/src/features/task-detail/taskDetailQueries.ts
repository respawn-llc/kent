import type { QueryClient } from "@tanstack/react-query";
import type { OffsetPage } from "@/api";
import { invalidateProjectTaskSearches, queryKeys } from "@/app-facade";

const taskDetailFeedPageSize = 50;
const taskDetailFeedMaxPages = 10;

export type TaskDetailFeedPage<T> = OffsetPage<T> &
  Readonly<{
    offset: number;
    totalCount?: number;
  }>;

export function taskDetailFeedOptions<T>(
  queryKey: readonly unknown[],
  enabled: boolean,
  loadPage: (offset: number) => Promise<OffsetPage<T>>,
) {
  return {
    queryKey,
    queryFn: async ({ pageParam }: { pageParam: number }): Promise<TaskDetailFeedPage<T>> => ({
      ...(await loadPage(pageParam)),
      offset: pageParam,
    }),
    enabled,
    initialPageParam: 0,
    getNextPageParam: (lastPage: TaskDetailFeedPage<T>) => lastPage.nextOffset ?? undefined,
    getPreviousPageParam: (firstPage: TaskDetailFeedPage<T>) =>
      firstPage.offset === 0 ? undefined : firstPage.offset - taskDetailFeedPageSize,
    maxPages: taskDetailFeedMaxPages,
  };
}

export async function refreshTaskDetail(
  client: QueryClient,
  taskID: string,
  projectID: string,
): Promise<void> {
  await refreshTaskDetailReads(client, taskID);
  for (const queryKey of [
    queryKeys.projects,
    queryKeys.allAttention,
    queryKeys.allBoards,
    queryKeys.allBoardNodeCards,
    queryKeys.allTasks,
    queryKeys.allActivity,
  ]) {
    await client.invalidateQueries({ queryKey });
  }
  await invalidateProjectTaskSearches(client, projectID);
}

export async function refreshTaskDetailReads(client: QueryClient, taskID: string): Promise<void> {
  await Promise.all(
    [
      queryKeys.task(taskID),
      queryKeys.taskAttention(taskID),
      queryKeys.activity(taskID),
      queryKeys.comments(taskID),
    ].map(async (queryKey) => client.invalidateQueries({ queryKey, refetchType: "active" })),
  );
}
