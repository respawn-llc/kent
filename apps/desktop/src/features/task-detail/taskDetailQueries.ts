import type { InfiniteData, InfiniteQueryObserverOptions, QueryClient } from "@tanstack/react-query";
import type { OffsetPage } from "@/api";
import { invalidateProjectTaskSearches, queryKeys } from "@/app-facade";

const taskDetailFeedPageSize = 50;
const taskDetailFeedMaxPages = 10;

export function taskDetailFeedOptions<Page extends OffsetPage<unknown>>(
  queryKey: readonly unknown[],
  enabled: boolean,
  loadPage: (offset: number) => Promise<Page>,
): InfiniteQueryObserverOptions<Page, Error, InfiniteData<Page, number>, readonly unknown[], number> {
  return {
    queryKey,
    queryFn: async ({ pageParam }: { pageParam: number }) => loadPage(pageParam),
    enabled,
    initialPageParam: 0,
    getNextPageParam: (lastPage: Page) => lastPage.nextOffset ?? undefined,
    getPreviousPageParam: (_firstPage: Page, _pages: Page[], firstPageParam: number) =>
      firstPageParam === 0 ? undefined : firstPageParam - taskDetailFeedPageSize,
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
