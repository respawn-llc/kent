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
