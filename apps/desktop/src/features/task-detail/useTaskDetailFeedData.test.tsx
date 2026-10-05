import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { useState, type ReactNode } from "react";
import { RegistryProvider } from "@effect/atom-react";
import { createTestServices } from "@/test-support/app-services";
import type { TaskComment } from "@/api";
import { appI18n } from "@/i18n";
import { createTaskDetailViewModel, useTaskDetailReads } from "./TaskDetailViewModel";
import { afterEach, vi } from "vitest";

type Request = Readonly<{ feed: "activity" | "comments"; offset: number }>;

const testState = vi.hoisted((): { requests: Request[]; emptyNonzeroPage: boolean } => ({
  requests: [],
  emptyNonzeroPage: false,
}));

function useFeeds(client: QueryClient) {
  const [model] = useState(() => {
    const services = createTestServices([]);
    vi.spyOn(services.api, "getTask").mockRejectedValue(new Error("Not used by feed presentation"));
    vi.spyOn(services.api, "listTaskAttention").mockResolvedValue({ items: [], generatedAt: 1 });
    vi.spyOn(services.api, "listTaskComments").mockImplementation(async (_id, offset) => {
      testState.requests.push({ feed: "comments", offset });
      return taskPage("comments", offset);
    });
    vi.spyOn(services.api, "listTaskActivity").mockImplementation(async (_id, offset) => {
      testState.requests.push({ feed: "activity", offset });
      const page = taskPage("activity", offset);
      return {
        ...page,
        items: page.items.map((comment) => ({
          id: comment.id,
          type: "comment" as const,
          taskID: comment.taskID,
          occurredAt: 1,
          updatedAt: 1,
          comment,
        })),
      };
    });
    return createTaskDetailViewModel({
      services,
      client,
      taskID: "task-1",
      enabled: true,
      t: appI18n.t,
      push: vi.fn(),
    });
  });
  return useTaskDetailReads(model, true);
}

describe("Task Detail feed queries", () => {
  afterEach(() => {
    testState.requests.length = 0;
    testState.emptyNonzeroPage = false;
  });

  it("opens Activity and Comments independently at offset zero and traverses both edges", async () => {
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useFeeds(queryClient), { wrapper: queryWrapper(queryClient) });

    await waitFor(() => {
      expect(testState.requests).toHaveLength(2);
    });
    expect(testState.requests).toEqual([
      { feed: "activity", offset: 0 },
      { feed: "comments", offset: 0 },
    ]);

    await act(async () => {
      result.current.activity.fetchNextPage();
      result.current.comments.fetchNextPage();
    });
    await waitFor(() => {
      expect(result.current.activity.data?.pages).toHaveLength(2);
    });
    expect(testState.requests).toContainEqual({ feed: "activity", offset: 50 });
    expect(testState.requests).toContainEqual({ feed: "comments", offset: 50 });

    for (let index = 0; index < 9; index += 1) {
      await act(async () => {
        result.current.activity.fetchNextPage();
      });
      await waitFor(() => {
        expect(result.current.activity.data?.pages).toHaveLength(Math.min(index + 3, 10));
      });
    }
    expect(result.current.activity.hasPreviousPage).toBe(true);

    await act(async () => {
      result.current.activity.fetchPreviousPage();
    });
    await waitFor(() => {
      expect(result.current.activity.data?.pageParams[0]).toBe(0);
    });
    expect(testState.requests).toContainEqual({ feed: "activity", offset: 0 });
  });

  it("evicts the opposite edge at ten pages and reloads an evicted side", async () => {
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useFeeds(queryClient).comments, {
      wrapper: queryWrapper(queryClient),
    });

    await waitFor(() => {
      expect(testState.requests.filter((request) => request.feed === "comments")).toHaveLength(1);
    });
    for (let index = 0; index < 10; index += 1) {
      await act(async () => {
        result.current.fetchNextPage();
      });
      await waitFor(() => {
        expect(result.current.data?.pages).toHaveLength(Math.min(index + 2, 10));
      });
    }
    expect(result.current.data?.pages).toHaveLength(10);
    expect(result.current.data?.pageParams).toEqual([50, 100, 150, 200, 250, 300, 350, 400, 450, 500]);

    await act(async () => {
      result.current.fetchPreviousPage();
    });
    expect(testState.requests.at(-1)).toEqual({ feed: "comments", offset: 0 });
    await waitFor(() => {
      expect(result.current.data?.pages).toHaveLength(10);
      expect(result.current.data?.pageParams).toEqual([0, 50, 100, 150, 200, 250, 300, 350, 400, 450]);
    });

    await act(async () => {
      result.current.fetchNextPage();
    });
    expect(testState.requests.at(-1)).toEqual({ feed: "comments", offset: 500 });
  });

  it("keeps retained rows usable when a mutable nonzero edge returns empty", async () => {
    testState.emptyNonzeroPage = true;
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useFeeds(queryClient).comments, {
      wrapper: queryWrapper(queryClient),
    });

    await waitFor(() => {
      expect(testState.requests.filter((request) => request.feed === "comments")).toHaveLength(1);
    });
    await waitFor(() => {
      expect(result.current.data?.pages).toHaveLength(1);
    });
    await act(async () => {
      result.current.fetchNextPage();
    });
    expect(
      testState.requests.filter((request) => request.feed === "comments").map((request) => request.offset),
    ).toEqual([0, 50]);
    await waitFor(() => {
      expect(result.current.hasNextPage).toBe(false);
    });
    expect(result.current.data?.pages.flatMap((page) => page.items)).toHaveLength(1);

    await act(async () => {
      result.current.fetchNextPage();
    });
    expect(
      testState.requests.filter((request) => request.feed === "comments").map((request) => request.offset),
    ).toEqual([0, 50]);
  });

  it("retains repeated identities from mutable offset pages", async () => {
    const queryClient = createQueryClient();
    const { result } = renderHook(() => useFeeds(queryClient).activity, {
      wrapper: queryWrapper(queryClient),
    });

    await waitFor(() => {
      expect(testState.requests.filter((request) => request.feed === "activity")).toHaveLength(1);
    });
    await waitFor(() => {
      expect(result.current.data?.pages).toHaveLength(1);
    });
    await act(async () => {
      result.current.fetchNextPage();
    });
    await waitFor(() => {
      expect(result.current.data?.pages).toHaveLength(2);
    });
    const items = result.current.data?.pages.flatMap((page) => page.items) ?? [];
    expect(items).toHaveLength(2);
    expect(items[0]?.id).toBe(items[1]?.id);
  });
});

function taskPage(feed: Request["feed"], offset: number) {
  if (testState.emptyNonzeroPage && offset !== 0) {
    return { items: [], nextOffset: null, totalCount: 1 };
  }
  return {
    totalCount: 11,
    items: [
      {
        id: `${feed}-stable`,
        taskID: "task-1",
        body: "Comment",
        authorKind: "user",
        authorID: null,
        createdAt: 1,
        updatedAt: 1,
      } satisfies TaskComment,
    ],
    nextOffset: offset < 500 ? offset + 50 : null,
  };
}

function createQueryClient() {
  return new QueryClient({ defaultOptions: { queries: { retry: false } } });
}

function queryWrapper(queryClient: QueryClient) {
  return function QueryWrapper({ children }: Readonly<{ children: ReactNode }>) {
    return (
      <QueryClientProvider client={queryClient}>
        <RegistryProvider>{children}</RegistryProvider>
      </QueryClientProvider>
    );
  };
}
