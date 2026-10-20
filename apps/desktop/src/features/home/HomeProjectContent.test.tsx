import { act, fireEvent, render as renderReact, renderHook, screen, waitFor } from "@testing-library/react";
import { useCallback, type ReactElement } from "react";
import { beforeAll, beforeEach, expect, it, mock, vi } from "bun:test";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { createBrowserNativeBridge } from "@/test-support/native-bridge";
import { deferred } from "@/test-support/chat-runtime";

import { appI18n, initializeI18n } from "@/i18n";
import { clearLastProjectRoute, type SessionChatTarget } from "@/app-facade";
import type { ProjectTasksViewMemory } from "./projectTasksViewMemory";

it.each(["main", "subagent"] as const)(
  "rejects repeated %s paging and retry while retaining a pending page",
  async (category) => {
    const services = createTestServices([]);
    const list = vi.spyOn(services.api, "listSessionPage").mockResolvedValue({
      projectID: "project-1",
      category,
      sessions: [],
      nextOffset: 50,
    });
    const view = renderHook(() => useHomeSessionPages("project-1", category, true), {
      wrapper: ({ children }: { children: React.ReactNode }) => (
        <TestAppProviders services={services}>{children}</TestAppProviders>
      ),
    });
    await waitFor(() => {
      expect(view.result.current.isSuccess).toBe(true);
    });
    const response = deferred<Awaited<ReturnType<typeof services.api.listSessionPage>>>();
    list.mockReturnValue(response.promise);
    await act(async () => {
      view.result.current.fetchNextPage();
      view.result.current.fetchNextPage();
      view.result.current.refetch();
    });
    expect(list).toHaveBeenCalledTimes(2);
    await act(async () => {
      response.resolve({ projectID: "project-1", category, sessions: [], nextOffset: null });
    });
  },
);

it.each(["main", "subagent"] as const)(
  "ignores disabled %s requests and missing continuation edges",
  async (category) => {
    const services = createTestServices([]);
    const list = vi.spyOn(services.api, "listSessionPage").mockResolvedValue({
      projectID: "project-1",
      category,
      sessions: [],
      nextOffset: null,
    });
    const view = renderHook(({ enabled }) => useHomeSessionPages("project-1", category, enabled), {
      initialProps: { enabled: false },
      wrapper: ({ children }: { children: React.ReactNode }) => (
        <TestAppProviders services={services}>{children}</TestAppProviders>
      ),
    });
    await act(async () => {
      view.result.current.fetchNextPage();
      view.result.current.refetch();
    });
    expect(list).not.toHaveBeenCalled();
    view.rerender({ enabled: true });
    await waitFor(() => {
      expect(view.result.current.isSuccess).toBe(true);
    });
    await act(async () => {
      view.result.current.fetchNextPage();
    });
    expect(list).toHaveBeenCalledTimes(1);
  },
);

type ProjectQueryFixture = Readonly<{
  data: Readonly<{ displayName: string; projectKey: string }> | undefined;
  error: Error | null;
  isPending: boolean;
}>;

const fixture: {
  projectQuery: ProjectQueryFixture;
  sessions: readonly {
    id: string;
    category: "main" | "subagent";
    name: string | null;
    shortID: bigint | null;
    firstPromptPreview: string | null;
    updatedAt: number;
  }[];
  sessionTargets: SessionChatTarget[];
} = {
  projectQuery: {
    data: { displayName: "Kent", projectKey: "KNT" },
    error: null,
    isPending: false,
  },
  sessions: [],
  sessionTargets: [],
};

const actualAppFacade = await import("@/app-facade");
await mock.module("@/app-facade", () => ({
  ...actualAppFacade,
  useAppNavigation: () => ({
    openSessionChat: async (target: SessionChatTarget) => {
      fixture.sessionTargets.push(target);
    },
    selectHomeProject: vi.fn(),
  }),
  useSessionChatCatalogReturn: () => null,
  useOwnedSidebarRoots: () => ({ open: vi.fn() }),
}));

function render(content: ReactElement, services = createTestServices([])) {
  vi.spyOn(services.api, "getProjectEdit").mockImplementation(async () => {
    if (fixture.projectQuery.isPending) {
      return deferred<Awaited<ReturnType<typeof services.api.getProjectEdit>>>().promise;
    }
    if (fixture.projectQuery.error !== null) throw fixture.projectQuery.error;
    return { projectID: "project-1", displayName: "Kent", projectKey: "KNT" };
  });
  vi.spyOn(services.api, "listSessionPage").mockImplementation(async (_projectID, category, offset) => ({
    projectID: "project-1",
    category,
    offset,
    nextOffset: null,
    sessions: fixture.sessions,
  }));
  return renderReact(<TestAppProviders services={services}>{content}</TestAppProviders>);
}

function nativeWindowServices() {
  const bridge = createBrowserNativeBridge();
  const openWindow = vi.fn(async () => "created" as const);
  const services = createTestServices([], {
    ...bridge,
    capabilities: { ...bridge.capabilities, dialogWindows: true },
    dialogs: { ...bridge.dialogs, openWindow },
  });
  return { openWindow, services };
}

await mock.module("./ProjectTasksSurface", () => ({
  ProjectTasksSurface: ({ viewMemory }: Readonly<{ viewMemory: ProjectTasksViewMemory }>) => {
    const registerGrid = useCallback(
      (element: HTMLDivElement | null) => {
        if (element === null) return;
        const memory = viewMemory.read();
        element.scrollTop = memory.verticalOffsetPx;
        element.scrollLeft = memory.horizontalOffsetPx;
      },
      [viewMemory],
    );
    return (
      <div
        aria-label={appI18n.t("home.prototype.projectTasksGrid")}
        onScroll={(event) => {
          viewMemory.setScrollOffsets(event.currentTarget.scrollTop, event.currentTarget.scrollLeft);
        }}
        ref={registerGrid}
        role="grid"
      />
    );
  },
}));

const { HomeProjectContent } = await import("./HomeProjectContent");
const { useHomeSessionPages } = await import("./HomeProjectModel");

beforeAll(async () => initializeI18n());

beforeEach(() => {
  clearLastProjectRoute("project-1");
  fixture.projectQuery = {
    data: { displayName: "Kent", projectKey: "KNT" },
    error: null,
    isPending: false,
  };
  fixture.sessions = [];
  fixture.sessionTargets = [];
});

it("renders the Task surface while selected Project metadata is pending", () => {
  fixture.projectQuery = {
    data: undefined,
    error: null,
    isPending: true,
  };

  render(<HomeProjectContent projectID="project-1" />);

  expect(
    screen.getByRole("grid", { name: appI18n.t("home.prototype.projectTasksGrid") }),
  ).toBeInTheDocument();
});

it("restores Task-grid pixels after visiting another Project tab", () => {
  render(<HomeProjectContent projectID="project-1" />);
  const grid = screen.getByRole("grid", { name: appI18n.t("home.prototype.projectTasksGrid") });
  grid.scrollTop = 500;
  grid.scrollLeft = 0;
  fireEvent.scroll(grid);

  fireEvent.click(screen.getByRole("tab", { name: appI18n.t("home.prototype.sessions") }));
  expect(
    screen.queryByRole("grid", { name: appI18n.t("home.prototype.projectTasksGrid") }),
  ).not.toBeInTheDocument();

  fireEvent.click(screen.getByRole("tab", { name: appI18n.t("home.prototype.tasks") }));
  const restoredGrid = screen.getByRole("grid", { name: appI18n.t("home.prototype.projectTasksGrid") });
  expect(restoredGrid.scrollTop).toBe(500);
  expect(restoredGrid.scrollLeft).toBe(0);
});

it("offers Project tabs with Tasks selected initially", () => {
  render(<HomeProjectContent projectID="project-1" />);

  expect(screen.getByRole("tab", { name: appI18n.t("home.prototype.tasks") })).toBeInTheDocument();
  expect(
    screen.getByRole("grid", { name: appI18n.t("home.prototype.projectTasksGrid") }),
  ).toBeInTheDocument();
});

it.each([
  ["main", "sessions"],
  ["subagent", "subagents"],
] as const)("opens a %s catalog row through Session Chat navigation", async (category, tabLabel) => {
  const sessionName = "Review chat";
  fixture.sessions = [
    {
      category,
      firstPromptPreview: "Review the change",
      id: `${category}-session`,
      shortID: null,
      name: sessionName,
      updatedAt: 1,
    },
  ];

  render(<HomeProjectContent projectID="project-1" />);
  fireEvent.click(screen.getByRole("tab", { name: appI18n.t(`home.prototype.${tabLabel}`) }));
  fireEvent.click(await screen.findByRole("button", { name: sessionName }));

  expect(fixture.sessionTargets).toEqual([
    {
      catalogOrigin: { category },
      projectID: "project-1",
      sessionID: `${category}-session`,
    },
  ]);
});

it.each([
  ["main", "sessions"],
  ["subagent", "subagents"],
] as const)("Shift-click opens a %s catalog row natively without navigating", async (category, tabLabel) => {
  const { openWindow, services } = nativeWindowServices();
  fixture.sessions = [
    {
      category,
      firstPromptPreview: "Review the change",
      id: `${category}-session`,
      shortID: null,
      name: "Review chat",
      updatedAt: 1,
    },
  ];

  render(<HomeProjectContent projectID="project-1" />, services);
  fireEvent.click(screen.getByRole("tab", { name: appI18n.t(`home.prototype.${tabLabel}`) }));
  fireEvent.click(await screen.findByRole("button", { name: "Review chat" }), { shiftKey: true });

  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledTimes(1);
  });
  expect(openWindow).toHaveBeenCalledWith(
    expect.objectContaining({
      params: { projectID: "project-1", sessionID: `${category}-session` },
    }),
  );
  expect(fixture.sessionTargets).toEqual([]);
});

it.each([
  ["main", "sessions"],
  ["subagent", "subagents"],
] as const)("opens a %s catalog row from its independent pop-out icon", async (category, tabLabel) => {
  const { openWindow, services } = nativeWindowServices();
  fixture.sessions = [
    {
      category,
      firstPromptPreview: "Review the change",
      id: `${category}-session`,
      shortID: null,
      name: "Review chat",
      updatedAt: 1,
    },
  ];

  render(<HomeProjectContent projectID="project-1" />, services);
  fireEvent.click(screen.getByRole("tab", { name: appI18n.t(`home.prototype.${tabLabel}`) }));
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("app.popOut") }));

  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledTimes(1);
  });
  expect(openWindow).toHaveBeenCalledWith(
    expect.objectContaining({
      params: { projectID: "project-1", sessionID: `${category}-session` },
    }),
  );
  expect(fixture.sessionTargets).toEqual([]);
});

it("uses the Session ID as an unnamed row title and shows its supplied preview only once", async () => {
  const session = {
    category: "main" as const,
    firstPromptPreview: "A supplied preview",
    id: "unnamed-session",
    shortID: 73n,
    name: null,
    updatedAt: 1,
  };
  fixture.sessions = [session];
  render(<HomeProjectContent projectID="project-1" />);
  fireEvent.click(screen.getByRole("tab", { name: appI18n.t("home.prototype.sessions") }));
  expect(await screen.findByRole("button", { name: "73" })).toBeVisible();
  expect(screen.getAllByText(session.firstPromptPreview)).toHaveLength(1);
});
