import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import type { ReactElement } from "react";
import { beforeAll, beforeEach, expect, it, mock, vi } from "bun:test";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterContextProvider,
} from "@tanstack/react-router";

import { createBrowserNativeBridge } from "@/test-support/native-bridge";
import { createTaskDetailTestServices, taskDetailResponse } from "@/test-support/task-detail";
import { createTestSidebarController } from "@/test-support/sidebar";
import { TestAppProviders } from "@/test-support/app-services";
import { appI18n, initializeI18n } from "@/i18n";

const fixture = {
  copyText: vi.fn(async () => undefined),
  navigation: {
    openHome: vi.fn(async () => undefined),
    openSessionChat: vi.fn(async () => undefined),
  },
};

const actualAppFacade = await import("@/app-facade");
await mock.module("@/app-facade", () => ({
  ...actualAppFacade,
  useAppNavigation: () => fixture.navigation,
}));
await mock.module("./taskDetailDismissal", () => ({
  useExactTaskDetailDeleteDismissal: () => async () => ({ kind: "accepted" }),
}));

const { SidebarRootContext } = actualAppFacade;
const { StandaloneTaskRoute } = await import("./StandaloneTaskRoute");
const { TaskDetailWindowRoute } = await import("./TaskDetailWindowRoute");

beforeAll(async () => initializeI18n());
beforeEach(() => {
  fixture.copyText.mockClear();
  fixture.navigation.openHome.mockClear();
  fixture.navigation.openSessionChat.mockClear();
});

function renderTaskDetailHost(child: ReactElement) {
  const browserBridge = createBrowserNativeBridge();
  const nativeBridge = {
    ...browserBridge,
    capabilities: {
      ...browserBridge.capabilities,
      clipboard: { ...browserBridge.capabilities.clipboard, writeText: true },
    },
    clipboard: { ...browserBridge.clipboard, writeText: fixture.copyText },
  };
  const services = createTaskDetailTestServices(taskDetailResponse, { nativeBridge });
  const router = createRouter({
    history: createMemoryHistory({ initialEntries: ["/tasks/task-1"] }),
    routeTree: createRootRoute(),
  });
  return render(
    <RouterContextProvider router={router}>
      <SidebarRootContext.Provider value={createTestSidebarController()}>
        <TestAppProviders services={services}>{child}</TestAppProviders>
      </SidebarRootContext.Provider>
    </RouterContextProvider>,
  );
}

it.each([
  ["development", true, true],
  ["production", false, true],
] as const)("renders the %s main-window Task Detail actions", async (_name, enabled, hasChat) => {
  globalThis.__KENT_TEST_ENV__.DEV = enabled;
  renderTaskDetailHost(<StandaloneTaskRoute taskId="task-1" />);

  const flow = await screen.findByTestId("task-detail-action-flow");
  const openInCli = within(flow).queryByRole("button", {
    name: appI18n.t("task.openInCli", { name: "Review chat" }),
  });
  expect(openInCli !== null).toBe(!hasChat);
  if (openInCli !== null) {
    const session = taskDetailResponse.task.liveSessions[0];
    if (session === undefined) throw new Error("Live Session fixture is required.");
    fireEvent.click(openInCli);
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Review chat" }));
    await waitFor(() => {
      expect(fixture.copyText).toHaveBeenCalledWith(`kent --session=${session.sessionId}`);
    });
  }

  const openChat = within(flow).queryByRole("button", {
    name: appI18n.t("task.openInChat"),
  });
  expect(openChat !== null).toBe(hasChat);
  if (openChat !== null) {
    fireEvent.click(openChat);
    fireEvent.click(within(screen.getByRole("dialog")).getByRole("button", { name: "Review chat" }));
    await waitFor(() => {
      expect(fixture.navigation.openSessionChat).toHaveBeenCalledWith({
        projectID: "project-1",
        sessionID: "33333333-3333-4333-8333-333333333333",
      });
    });
  }
});

it("keeps native Task Detail on its CLI action without a Chat action", async () => {
  renderTaskDetailHost(<TaskDetailWindowRoute taskID="task-1" />);

  const flow = await screen.findByTestId("task-detail-action-flow");
  expect(
    within(flow).getByRole("button", {
      name: appI18n.t("task.openInCli", { name: "Review chat" }),
    }),
  ).toBeInTheDocument();
  expect(
    within(flow).queryByRole("button", {
      name: appI18n.t("task.openInChat"),
    }),
  ).not.toBeInTheDocument();
});
