import { act, renderHook, waitFor } from "@testing-library/react";
import type { TaskSearchResponse } from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { useTaskSearch } from "./useTaskSearch";

it("keeps completed same-Project results during replacement without paginating those retained results", async () => {
  const services = createTestServices([]);
  const replacement = deferred<TaskSearchResponse>();
  const search = vi
    .spyOn(services.api, "searchTasks")
    .mockResolvedValueOnce(page("first", 1))
    .mockReturnValue(replacement.promise);
  const view = renderHook(({ project, query }) => useTaskSearch(project, true, query), {
    initialProps: { project: "project-1", query: "first" },
    wrapper: ({ children }) => <TestAppProviders services={services}>{children}</TestAppProviders>,
  });
  await waitFor(() => {
    expect(view.result.current.results[0]?.group.taskID).toBe("first");
  });
  view.rerender({ project: "project-1", query: "second" });
  await waitFor(() => {
    expect(search).toHaveBeenCalledTimes(2);
  });
  expect(view.result.current.results[0]?.group.taskID).toBe("first");
  expect(view.result.current.paginationUsesVisibleData).toBe(false);
  await act(async () => {
    view.result.current.request.fetchNextPage();
  });
  expect(search).toHaveBeenCalledTimes(2);
  await act(async () => {
    replacement.resolve(page("second", null));
  });
  expect(view.result.current.results[0]?.group.taskID).toBe("second");
  search.mockReturnValue(new Promise(() => undefined));
  view.rerender({ project: "project-2", query: "second" });
  expect(view.result.current.results).toEqual([]);
});

it("retains at most three pages while paging through a larger result", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api, "searchTasks").mockImplementation(async (input) => {
    const offset = input.offset ?? 0;
    return page(`task-${String(offset)}`, offset < 4 ? offset + 1 : null);
  });
  const view = renderHook(() => useTaskSearch("project-1", true, "needle"), {
    wrapper: ({ children }) => <TestAppProviders services={services}>{children}</TestAppProviders>,
  });
  await waitFor(() => {
    expect(view.result.current.request.isSuccess).toBe(true);
  });
  for (let offset = 1; offset <= 4; offset++) {
    await act(async () => {
      view.result.current.request.fetchNextPage();
    });
    await waitFor(() => {
      expect(view.result.current.results.at(-1)?.group.taskID).toBe(`task-${String(offset)}`);
    });
    expect(view.result.current.request.data?.pages.length).toBeLessThanOrEqual(3);
  }
  expect(view.result.current.results.map((result) => result.group.taskID)).toEqual([
    "task-2",
    "task-3",
    "task-4",
  ]);
});

function page(taskID: string, nextOffset: number | null): TaskSearchResponse {
  return {
    mode: "literal",
    nextOffset,
    groups: [
      {
        projectID: "project-1",
        projectKey: "KNT",
        taskID,
        shortID: "KNT-1",
        workflowID: "workflow-1",
        title: taskID,
        totalHitCount: 1,
        status: { kind: "backlog", nativeState: "active", nodeIDs: [], attentionTypes: [] },
        hits: [
          {
            ordinal: 1,
            source: { kind: "title" },
            literal: {
              before: "",
              match: taskID,
              after: "",
              leftTruncated: false,
              rightTruncated: false,
            },
          },
        ],
      },
    ],
  };
}
