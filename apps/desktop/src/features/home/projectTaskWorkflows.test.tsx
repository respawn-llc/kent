import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { RegistryProvider } from "@effect/atom-react";

import type { WorkflowListInput, WorkflowRecord, WorkflowPage } from "@/api";
import { deferred } from "@/test-support/chat-runtime";
import { projectTaskWorkflowItems, useProjectTaskWorkflowPages } from "./projectTaskWorkflows";

const projectID = "project-1";
interface ProjectTaskWorkflowFixture {
  requests: bigint[];
  workflows: WorkflowRecord[];
  pending: Promise<WorkflowPage> | null;
}

const fixture = vi.hoisted<ProjectTaskWorkflowFixture>(() => ({
  pending: null,
  requests: [],
  workflows: Array.from({ length: 130 }, (_value, index): WorkflowRecord => ({
    description: "",
    executionTargetPolicy: { customRef: null, mode: "default_branch" },
    id: `workflow-${index.toString()}`,
    name: `Workflow ${index.toString()}`,
    projectLink: { isDefault: index === 0 },
    version: 1,
  })),
}));

vi.mock("@/app-facade", async (importOriginal) => ({
  ...(await importOriginal()),
  useAppServices: () => ({
    api: {
      listWorkflows: async (input: WorkflowListInput) => {
        const offset = input.offset ?? 0n;
        const limit = BigInt(input.limit ?? 40);
        fixture.requests.push(offset);
        if (fixture.pending !== null) return fixture.pending;
        return {
          nextOffset: offset + limit < BigInt(fixture.workflows.length) ? offset + limit : null,
          workflows: fixture.workflows.slice(Number(offset), Number(offset + limit)),
        };
      },
    },
  }),
}));

beforeEach(() => {
  fixture.requests = [];
  fixture.pending = null;
});

it.each(["next", "previous", "retry"] as const)(
  "admits Workflow %s once while retained pages are fetching",
  async (action) => {
    const view = renderHook(() => useProjectTaskWorkflowPages(projectID), { wrapper: queryWrapper() });
    await waitFor(() => {
      expect(view.result.current.isSuccess).toBe(true);
    });
    if (action === "previous") {
      for (let page = 0; page < 3; page += 1) {
        await act(async () => {
          view.result.current.fetchNextPage();
        });
      }
      expect(view.result.current.hasPreviousPage).toBe(true);
    }
    const count = fixture.requests.length;
    const response = deferred<WorkflowPage>();
    fixture.pending = response.promise;
    await act(async () => {
      const invoke =
        action === "next"
          ? view.result.current.fetchNextPage
          : action === "previous"
            ? view.result.current.fetchPreviousPage
            : view.result.current.refetch;
      invoke();
      invoke();
      view.result.current.refetch();
    });
    expect(fixture.requests).toHaveLength(count + 1);
    await act(async () => {
      response.resolve({ workflows: [], nextOffset: null });
    });
  },
);

it("keeps a bounded bidirectional window of Project Workflow pages", async () => {
  const view = renderHook(() => useProjectTaskWorkflowPages(projectID), {
    wrapper: queryWrapper(),
  });

  await waitFor(() => {
    expect(view.result.current.isSuccess).toBe(true);
  });
  await act(async () => {
    view.result.current.fetchPreviousPage();
  });
  expect(fixture.requests).toEqual([0n]);
  for (let page = 0; page < 3; page += 1) {
    await act(async () => {
      view.result.current.fetchNextPage();
    });
  }

  expect(view.result.current.data?.pageParams).toEqual([40n, 80n, 120n]);
  expect(projectTaskWorkflowItems(view.result.current.data)).toHaveLength(90);
  await act(async () => {
    view.result.current.fetchNextPage();
  });
  expect(fixture.requests).toEqual([0n, 40n, 80n, 120n]);

  await act(async () => {
    view.result.current.fetchPreviousPage();
  });

  expect(view.result.current.data?.pageParams).toEqual([0n, 40n, 80n]);
  expect(projectTaskWorkflowItems(view.result.current.data)).toHaveLength(120);
  expect(fixture.requests).toEqual([0n, 40n, 80n, 120n, 0n]);
});

function queryWrapper() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return function QueryWrapper({ children }: Readonly<{ children: ReactNode }>) {
    return createElement(QueryClientProvider, {
      children: createElement(RegistryProvider, { children }),
      client: queryClient,
    });
  };
}
