import { act, render, renderHook, screen, waitFor } from "@testing-library/react";
import { useEffect, useState, type ReactNode } from "react";
import { QueryClient, useQueryClient } from "@tanstack/react-query";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  globalAttentionPage as attentionResponse,
  globalAttentionRoute,
  globalAttentionRequests,
  approvalAttentionFixture as attentionItem,
} from "@/test-support/attention";
import { SidebarRootContext, queryKeys, useAppServices, type SidebarDestination } from "@/app-facade";
import { createTestServices, TestAppProviders, type TestAppServices } from "@/test-support/app-services";
import type { FakeRpcTransport } from "@/test-support/api";
import { flushQueuedWork, installAnimationFrameTestSupport } from "@/test-support/scheduling";
import { createTestSidebarController, createTestSidebarNavigator } from "@/test-support/sidebar";
import { SidebarInboxNav } from "./SidebarInboxNav";
import { createHomeAttentionPages, useGlobalAttentionPages, useProjectPages } from "./useHomeData";
import { createHomeViewModel } from "./HomeViewModel";
import { appI18n } from "@/i18n";
import { useProjectCreationActions } from "./ProjectCreationModel";
import { deferred } from "@/test-support/chat-runtime";

function catalogFixture() {
  const services = createTestServices([]);
  const client = new QueryClient({
    defaultOptions: { queries: { retry: false }, mutations: { retry: false } },
  });
  const list = vi.spyOn(services.api, "listProjects").mockImplementation(async (token) => ({
    projects: [],
    nextPageToken: token === null ? "next" : null,
    generatedAt: 1,
  }));
  const model = createHomeViewModel({
    services,
    client,
    t: appI18n.t,
    push: vi.fn(),
    openProject: vi.fn(async () => undefined),
  });
  const view = renderHook(
    () => ({
      catalog: useProjectPages(model.projects),
      creation: useProjectCreationActions(model.creation),
    }),
    {
      wrapper: ({ children }: { children: ReactNode }) => (
        <TestAppProviders services={services} queryClient={client}>
          {children}
        </TestAppProviders>
      ),
    },
  );
  return { ...view, list, client, services };
}

it("continues the mounted Projects catalog after completion and refresh", async () => {
  const view = catalogFixture();
  await waitFor(() => {
    expect(view.result.current.catalog.isSuccess).toBe(true);
  });
  await act(async () => {
    view.result.current.catalog.refetch();
  });
  await act(async () => {
    view.result.current.catalog.fetchNextPage();
  });
  expect(view.list.mock.calls.map(([token]) => token)).toEqual([null, null, "next"]);
  expect(view.result.current.catalog.data?.pages).toHaveLength(2);
  await act(async () => {
    view.result.current.catalog.fetchNextPage();
  });
  expect(view.list).toHaveBeenCalledTimes(3);
});

it("refreshes the still-mounted Projects catalog after creation", async () => {
  const view = catalogFixture();
  await waitFor(() => {
    expect(view.result.current.catalog.isSuccess).toBe(true);
  });
  vi.spyOn(view.services.api, "planWorkspace").mockResolvedValue({
    kind: "local_unbound",
    canonicalRoot: "/Kent",
    binding: null,
  });
  vi.spyOn(view.services.api, "createProject").mockImplementation(async () => {
    view.list.mockResolvedValue({
      projects: [
        {
          id: "created-project",
          key: "KENT",
          name: "Kent",
          primaryWorkspace: {
            id: "workspace-1",
            name: "Kent",
            rootPath: "/Kent",
            availability: "available",
            isPrimary: true,
            updatedAt: 1,
          },
          defaultWorkflowID: null,
          defaultWorkflowName: null,
          defaultWorkflowValid: false,
          updatedAt: 1,
          taskCount: 0,
          attentionCount: 0,
          workflowCount: 0,
        },
      ],
      nextPageToken: null,
      generatedAt: 2,
    });
    return {
      projectID: "created-project",
      projectKey: "KENT",
      projectName: "Kent",
      workspaceID: "workspace-1",
      canonicalRoot: "/Kent",
      workspaceName: "Kent",
      workspaceStatus: "available",
    };
  });
  await act(async () => {
    view.result.current.creation.submit({
      draft: { name: "Kent", key: "KENT", workspaceRoot: "/Kent" },
      complete: vi.fn(async () => undefined),
      selectionRequired: vi.fn(),
    });
  });
  await waitFor(() => {
    expect(view.result.current.catalog.data?.pages[0]?.projects[0]?.id).toBe("created-project");
  });
  expect(view.list).toHaveBeenCalledTimes(2);
});

it("releases Projects only after the owning Home binding departs", async () => {
  const view = catalogFixture();
  await waitFor(() => {
    expect(view.result.current.catalog.isSuccess).toBe(true);
  });
  expect(view.client.getQueryData(queryKeys.projects)).toBeDefined();
  view.rerender();
  expect(view.client.getQueryData(queryKeys.projects)).toBeDefined();
  view.unmount();
  await waitFor(() => {
    expect(view.client.getQueryData(queryKeys.projects)).toBeUndefined();
  });
});

it.each(["next", "retry"] as const)(
  "admits Projects %s once while retained data is fetching",
  async (action) => {
    const view = catalogFixture();
    await waitFor(() => {
      expect(view.result.current.catalog.isSuccess).toBe(true);
    });
    const response = deferred<Awaited<ReturnType<typeof view.services.api.listProjects>>>();
    view.list.mockReturnValue(response.promise);
    await act(async () => {
      if (action === "next") {
        view.result.current.catalog.fetchNextPage();
        view.result.current.catalog.fetchNextPage();
      } else {
        view.result.current.catalog.refetch();
        view.result.current.catalog.refetch();
      }
    });
    expect(view.list).toHaveBeenCalledTimes(2);
    await act(async () => {
      response.resolve({ projects: [], nextPageToken: null, generatedAt: 2 });
    });
  },
);

it.each(["next", "retry"] as const)(
  "admits Inbox %s once while retained data is fetching",
  async (action) => {
    const services = createTestServices([]);
    const list = vi.spyOn(services.api, "listAttention").mockResolvedValue({
      items: [],
      nextPageToken: "next",
      generatedAt: 1,
    });
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const model = createHomeAttentionPages(services.api, client, false);
    const view = renderHook(() => useGlobalAttentionPages(model), {
      wrapper: ({ children }: { children: ReactNode }) => (
        <TestAppProviders services={services} queryClient={client}>
          {children}
        </TestAppProviders>
      ),
    });
    await waitFor(() => {
      expect(view.result.current.isSuccess).toBe(true);
    });
    const response = deferred<Awaited<ReturnType<typeof services.api.listAttention>>>();
    list.mockReturnValue(response.promise);
    await act(async () => {
      if (action === "next") {
        view.result.current.fetchNextPage();
        view.result.current.fetchNextPage();
      } else {
        view.result.current.refetch();
        view.result.current.refetch();
      }
    });
    expect(list).toHaveBeenCalledTimes(2);
    await act(async () => {
      response.resolve({ items: [], nextPageToken: null, generatedAt: 2 });
    });
    await act(async () => {
      view.result.current.fetchNextPage();
    });
    expect(list).toHaveBeenCalledTimes(2);
  },
);

describe("Home global attention data", () => {
  beforeEach(() => {
    installAnimationFrameTestSupport();
  });

  afterEach(() => {
    vi.unstubAllGlobals();
  });

  it("does not refetch Sidebar navigation when Home already owns populated attention data", async () => {
    const services = createAttentionServices();
    const view = renderHome(services, <HomeAttentionQueryHarness />);

    await expectAttentionCalls(services.transport, 1);

    view.rerender(
      <TestAppProviders services={services}>
        <SidebarRootContext.Provider value={sidebarController}>
          <HomeAttentionQueryHarness />
          <SidebarInboxNav destination={taskDetailDestination} navigator={sidebarNavigator} />
        </SidebarRootContext.Provider>
      </TestAppProviders>,
    );
    await flushQueuedWork();

    expect(globalAttentionRequests(services.transport)).toEqual([null]);
  });

  it("loads a cold cache when Sidebar is the only global attention observer", async () => {
    const services = createAttentionServices();
    renderHome(
      services,
      <SidebarInboxNav destination={taskDetailDestination} navigator={sidebarNavigator} />,
    );

    await expectAttentionCalls(services.transport, 1);
    expect(globalAttentionRequests(services.transport)).toEqual([null]);
  });

  it("refreshes stale data for a sole Sidebar observer and renders the refreshed navigation", async () => {
    const services = createAttentionServices((pageToken, callIndex) => {
      if (pageToken !== null) {
        return attentionResponse([]);
      }
      return attentionResponse(
        callIndex === 0 ? [attentionItem("task-1")] : [attentionItem("task-1"), attentionItem("task-2")],
      );
    });
    const openedDestinations: SidebarDestination[] = [];
    const controller = createTestSidebarController((destination) => {
      openedDestinations.push(destination);
    });
    const navigator = {
      ...sidebarNavigator,
      replace: (destination: SidebarDestination) => {
        controller.open(destination);
        return "accepted" as const;
      },
    };
    let homeAttention: ReturnType<typeof useGlobalAttentionPages> | undefined;
    const view = renderHome(
      services,
      <HomeAttentionQueryHarness
        onQuery={(query) => {
          homeAttention = query;
        }}
      />,
    );

    await expectAttentionCalls(services.transport, 1);
    await waitFor(() => {
      expect(homeAttention?.data?.pages[0]?.items[0]?.taskID).toBe("task-1");
    });
    view.rerender(
      <TestAppProviders services={services}>
        <SidebarRootContext.Provider value={sidebarController}>
          <div />
        </SidebarRootContext.Provider>
      </TestAppProviders>,
    );
    await flushQueuedWork();
    view.rerender(
      <TestAppProviders services={services}>
        <SidebarRootContext.Provider value={controller}>
          <SidebarInboxNav destination={taskDetailDestination} navigator={navigator} />
        </SidebarRootContext.Provider>
      </TestAppProviders>,
    );

    await expectAttentionCalls(services.transport, 2);
    await waitFor(() => {
      expect(screen.getAllByRole("button")).toHaveLength(1);
    });
    const nextButton = screen.getAllByRole("button")[0];
    if (nextButton === undefined) {
      throw new Error("Sidebar Inbox navigation did not render its available control.");
    }
    act(() => {
      nextButton.click();
    });
    expect(openedDestinations).toHaveLength(1);
    expect(openedDestinations[0]).toMatchObject({
      inboxNav: true,
      kind: "taskDetail",
      taskID: "task-2",
    });
  });

  it("forwards the production infinite-query next-page token exactly once", async () => {
    let attentionQuery: ReturnType<typeof useGlobalAttentionPages> | undefined;
    const services = createAttentionServices((pageToken) =>
      pageToken === null ? attentionResponse([], "page-2") : attentionResponse([]),
    );
    renderHome(
      services,
      <HomeAttentionQueryHarness
        onQuery={(query) => {
          attentionQuery = query;
        }}
      />,
    );

    await expectAttentionCalls(services.transport, 1);
    await waitFor(() => {
      expect(attentionQuery?.hasNextPage).toBe(true);
    });
    const query = attentionQuery;
    if (query === undefined) {
      throw new Error("Home attention query was not exposed by the rendered harness.");
    }

    await act(async () => {
      query.fetchNextPage();
    });
    await expectAttentionCalls(services.transport, 2);
    expect(globalAttentionRequests(services.transport)).toEqual([null, "page-2"]);
  });
});

function renderHome(services: TestAppServices, children: ReactNode) {
  return render(
    <TestAppProviders services={services}>
      <SidebarRootContext.Provider value={sidebarController}>{children}</SidebarRootContext.Provider>
    </TestAppProviders>,
  );
}

function HomeAttentionQueryHarness({
  onQuery,
}: Readonly<{
  onQuery?: (query: ReturnType<typeof useGlobalAttentionPages>) => void;
}>) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const [model] = useState(() => createHomeAttentionPages(api, client, false));
  const query = useGlobalAttentionPages(model);
  useEffect(() => {
    onQuery?.(query);
  }, [onQuery, query]);
  return null;
}

type AttentionPageFactory = Parameters<typeof globalAttentionRoute>[0];

function createAttentionServices(page: AttentionPageFactory = () => attentionResponse([])): TestAppServices {
  return createTestServices([globalAttentionRoute(page)]);
}

async function expectAttentionCalls(transport: FakeRpcTransport, count: number): Promise<void> {
  await waitFor(() => {
    expect(globalAttentionRequests(transport)).toHaveLength(count);
  });
}

const taskDetailDestination = {
  kind: "taskDetail",
  inboxNav: true,
  taskID: "task-1",
} as const;

const sidebarController = createTestSidebarController();
const sidebarNavigator = createTestSidebarNavigator();
