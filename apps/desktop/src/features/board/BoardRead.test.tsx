import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import type { WorkflowBoard } from "@/api";
import { TestAppProviders, createTestServices } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { BoardQueryProvider } from "./BoardQueryContext";
import { useBoardQuery } from "./BoardQueryRuntime";
import { useBoard, useBoardNodeCards } from "./useBoardData";
import type { BoardNodeCardsPage } from "@/api";
import { QueryClient } from "@tanstack/react-query";

it("rejects duplicate and disabled Board Retry without replacing the accepted refresh", async () => {
  const services = createTestServices([]);
  const board: WorkflowBoard = {
    projectID: "project",
    projectKey: "P",
    projectName: "Project",
    defaultWorkspaceID: "workspace",
    attachedWorkspaceCount: 1,
    selectedWorkflow: null,
    workflows: [],
    columns: [],
    groups: [],
    generatedAt: 1,
  };
  const pending = deferred<WorkflowBoard>();
  const read = vi
    .spyOn(services.api, "getBoard")
    .mockResolvedValueOnce(board)
    .mockReturnValue(pending.promise);
  let enabled = true;
  const { result, rerender } = renderHook(() => useBoard("project", undefined), {
    wrapper: ({ children }: { children: ReactNode }) => (
      <TestAppProviders services={services}>
        <BoardQueryProvider labelFilter={{ kind: "none" }} queriesEnabled={enabled}>
          {children}
        </BoardQueryProvider>
      </TestAppProviders>
    ),
  });
  await waitFor(() => { expect(result.current.data).toEqual(board); });
  act(() => {
    result.current.refetch();
    result.current.refetch();
  });
  expect(read).toHaveBeenCalledTimes(2);
  await act(async () => { pending.reject(new Error("Refresh failed")); });
  await waitFor(() => { expect(result.current.isError).toBe(true); });
  read.mockResolvedValue(board);
  act(() => { result.current.refetch(); });
  await waitFor(() => { expect(result.current.isFetching).toBe(false); });
  expect(result.current.isError).toBe(false);
  expect(read).toHaveBeenCalledTimes(3);
  enabled = false;
  rerender();
  act(() => { result.current.refetch(); });
  expect(read).toHaveBeenCalledTimes(3);
});

it("rejects duplicate and disabled column Retry and permits a later valid retry", async () => {
  const services = createTestServices([]);
  const page: BoardNodeCardsPage = {
    projectID: "project",
    workflowID: "workflow",
    nodeID: "node",
    cards: [],
    nextOffset: null,
    generatedAt: 1,
  };
  const pending = deferred<BoardNodeCardsPage>();
  const read = vi
    .spyOn(services.api, "listBoardNodeCards")
    .mockResolvedValueOnce(page)
    .mockReturnValue(pending.promise);
  let queriesEnabled = true;
  const { result, rerender } = renderHook(
    ({ active }) => useBoardNodeCards("project", "workflow", "node", active),
    {
      initialProps: { active: true },
      wrapper: ({ children }: { children: ReactNode }) => (
        <TestAppProviders services={services}>
          <BoardQueryProvider labelFilter={{ kind: "none" }} queriesEnabled={queriesEnabled}>
            {children}
          </BoardQueryProvider>
        </TestAppProviders>
      ),
    },
  );
  await waitFor(() => { expect(result.current.data?.pages).toEqual([page]); });
  act(() => {
    result.current.refetch();
    result.current.refetch();
  });
  expect(read).toHaveBeenCalledTimes(2);
  await act(async () => { pending.reject(new Error("Refresh failed")); });
  await waitFor(() => { expect(result.current.isError).toBe(true); });
  queriesEnabled = false;
  rerender({ active: true });
  act(() => { result.current.refetch(); });
  expect(read).toHaveBeenCalledTimes(2);
  queriesEnabled = true;
  rerender({ active: false });
  act(() => { result.current.refetch(); });
  expect(read).toHaveBeenCalledTimes(2);
  read.mockResolvedValue(page);
  rerender({ active: true });
  await waitFor(() => { expect(result.current.isFetching).toBe(false); });
  const completedReads = read.mock.calls.length;
  act(() => { result.current.refetch(); });
  await waitFor(() => { expect(result.current.isFetching).toBe(false); });
  expect(read).toHaveBeenCalledTimes(completedReads + 1);
  expect(result.current.isError).toBe(false);
});

it.each(["next", "previous"] as const)(
  "admits %s paging during a background column refresh",
  async (direction) => {
    const services = createTestServices([]);
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const page: BoardNodeCardsPage = {
      projectID: "project",
      workflowID: "workflow",
      nodeID: "node",
      cards: [],
      nextOffset: 25,
      generatedAt: 1,
    };
    const refresh = deferred<BoardNodeCardsPage>();
    const directional = deferred<BoardNodeCardsPage>();
    const read = vi.spyOn(services.api, "listBoardNodeCards").mockImplementation(async ({ offset }) => ({
      ...page,
      nextOffset: (offset ?? 0) + 25,
    }));
    const { result } = renderHook(() => useBoardNodeCards("project", "workflow", "node", true), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <TestAppProviders services={services} queryClient={client}>
          <BoardQueryProvider labelFilter={{ kind: "none" }}>{children}</BoardQueryProvider>
        </TestAppProviders>
      ),
    });
    await waitFor(() => { expect(result.current.data?.pageParams).toEqual([0]); });
    if (direction === "previous") {
      for (const offset of [25, 50, 75]) {
        act(() => { result.current.fetchNextPage(); });
        await waitFor(() => { expect(result.current.data?.pageParams.at(-1)).toBe(offset); });
      }
    }
    read.mockReturnValueOnce(refresh.promise).mockReturnValueOnce(directional.promise);
    act(() => {
      void client.invalidateQueries();
    });
    await waitFor(() => { expect(result.current.isFetching).toBe(true); });
    expect(result.current.isFetchingNextPage).toBe(false);
    expect(result.current.isFetchingPreviousPage).toBe(false);
    const beforePaging = read.mock.calls.length;
    act(() => {
      if (direction === "next") result.current.fetchNextPage();
      else result.current.fetchPreviousPage();
    });
    await waitFor(() => { expect(read).toHaveBeenCalledTimes(beforePaging + 1); });
    expect(read.mock.calls.at(-1)?.[0].offset).toBe(direction === "next" ? 25 : 0);
    expect(
      direction === "next" ? result.current.isFetchingNextPage : result.current.isFetchingPreviousPage,
    ).toBe(true);
    act(() => {
      if (direction === "next") result.current.fetchNextPage();
      else result.current.fetchPreviousPage();
    });
    expect(read).toHaveBeenCalledTimes(beforePaging + 1);
    await act(async () => {
      directional.resolve({ ...page, nextOffset: direction === "next" ? 50 : 25 });
      refresh.resolve(page);
    });
    await waitFor(() => { expect(result.current.isFetching).toBe(false); });
    expect(result.current.data?.pageParams).toEqual(direction === "next" ? [0, 25] : [0, 25, 50]);
  },
);

it("retains Board content during filter replacement and a failed replacement read", async () => {
  const services = createTestServices([]);
  const board: WorkflowBoard = {
    projectID: "project",
    projectKey: "P",
    projectName: "Project",
    defaultWorkspaceID: "workspace",
    attachedWorkspaceCount: 1,
    selectedWorkflow: null,
    workflows: [],
    columns: [],
    groups: [],
    generatedAt: 1,
  };
  const replacement = deferred<WorkflowBoard>();
  const read = vi
    .spyOn(services.api, "getBoard")
    .mockResolvedValueOnce(board)
    .mockReturnValue(replacement.promise);
  const { result } = renderHook(
    () => ({
      read: useBoard("project", undefined),
      filters: useBoardQuery(),
    }),
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <TestAppProviders services={services}>
          <BoardQueryProvider labelFilter={{ kind: "none" }}>{children}</BoardQueryProvider>
        </TestAppProviders>
      ),
    },
  );
  await waitFor(() => {
    expect(result.current.read.data).toEqual(board);
  });
  act(() => {
    result.current.filters.setDependencyFilter(true);
  });
  await waitFor(() => {
    expect(read).toHaveBeenCalledTimes(2);
  });
  expect(result.current.read.data).toEqual(board);
  const failure = new Error("read failed");
  await act(async () => {
    replacement.reject(failure);
  });
  await waitFor(() => {
    expect(result.current.read.isError).toBe(true);
  });
  expect(result.current.read.error).toBe(failure);
  expect(result.current.read.data).toEqual(board);
});

it("does not page retained column content while replacement inputs are loading", async () => {
  const services = createTestServices([]);
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const page: BoardNodeCardsPage = {
    projectID: "project",
    workflowID: "workflow",
    nodeID: "node",
    cards: [],
    nextOffset: 25,
    generatedAt: 1,
  };
  const replacement = deferred<BoardNodeCardsPage>();
  const read = vi
    .spyOn(services.api, "listBoardNodeCards")
    .mockResolvedValueOnce(page)
    .mockReturnValue(replacement.promise);
  const { result, unmount } = renderHook(
    () => ({
      read: useBoardNodeCards("project", "workflow", "node", true),
      filters: useBoardQuery(),
    }),
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <TestAppProviders services={services} queryClient={client}>
          <BoardQueryProvider labelFilter={{ kind: "none" }}>{children}</BoardQueryProvider>
        </TestAppProviders>
      ),
    },
  );
  await waitFor(() => {
    expect(result.current.read.data?.pages).toEqual([page]);
  });
  act(() => {
    result.current.filters.setSort({ field: "created", direction: "asc" });
  });
  await waitFor(() => {
    expect(read).toHaveBeenCalledTimes(2);
  });
  expect(result.current.read.isPlaceholderData).toBe(true);
  act(() => {
    result.current.read.fetchNextPage();
  });
  expect(read).toHaveBeenCalledTimes(2);
  expect(read.mock.calls.map(([input]) => input.offset)).toEqual([0, 0]);
  await act(async () => {
    replacement.resolve(page);
  });
  await waitFor(() => {
    expect(result.current.read.isPlaceholderData).toBe(false);
  });
  unmount();
  await waitFor(() => {
    expect(
      client
        .getQueryCache()
        .getAll()
        .every((query) => query.getObserversCount() === 0),
    ).toBe(true);
  });
});
