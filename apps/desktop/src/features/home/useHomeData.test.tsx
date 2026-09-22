import { act, render, screen, waitFor } from "@testing-library/react";
import { useEffect, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  globalAttentionPage as attentionResponse,
  globalAttentionRoute,
  globalAttentionRequests,
  approvalAttentionFixture as attentionItem,
} from "@/test-support/attention";
import { SidebarRootContext, type SidebarDestination } from "@/app-facade";
import { createTestServices, TestAppProviders, type TestAppServices } from "@/test-support/app-services";
import type { FakeRpcTransport } from "@/test-support/api";
import { flushQueuedWork, installAnimationFrameTestSupport } from "@/test-support/scheduling";
import { createTestSidebarController, createTestSidebarNavigator } from "@/test-support/sidebar";
import { SidebarInboxNav } from "./SidebarInboxNav";
import { useGlobalAttentionPages } from "./useHomeData";

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
      await query.fetchNextPage();
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
  const query = useGlobalAttentionPages();
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
