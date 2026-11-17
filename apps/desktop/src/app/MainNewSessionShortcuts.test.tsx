import { afterEach, expect, it, mock, vi } from "bun:test";
import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { createMemoryHistory, Outlet, RouterProvider } from "@tanstack/react-router";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { MainNewSessionShortcuts } from "./MainNewSessionShortcuts";
import { sessionSettingsRead } from "@/test-support/chat-destination";

await mock.module("./routeComponents", () => ({
  RootRoute: () => (
    <>
      <MainNewSessionShortcuts />
      <Outlet />
    </>
  ),
  HomeShellRoute: () => null,
  ChatRoute: () => null,
  NewChatRoute: () => null,
  ProjectRoute: () => null,
  ProjectTasksRoute: () => null,
  TaskRoute: () => null,
  WorkflowEditorShellRoute: () => null,
  WorkflowLibraryShellRoute: () => null,
}));
const { createAppRouter } = await import("./routes");

afterEach(() => {
  vi.restoreAllMocks();
});

it.each(["/?projectId=project-1", "/projects/project-1", "/projects/project-1/tasks"])(
  "opens New Chat in the Project selected by %s",
  async (path) => {
    const services = createTestServices([]);
    const router = createAppRouter();
    router.update({ history: createMemoryHistory({ initialEntries: [path] }) });
    render(
      <TestAppProviders services={services}>
        <RouterProvider router={router} />
      </TestAppProviders>,
    );
    await act(async () => router.load());
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    await waitFor(() => {
      expect(router.state.location.pathname).toBe("/projects/project-1/chat");
    });
  },
);

it("does not choose a Project on the unselected Home screen", async () => {
  const services = createTestServices([]);
  const openWindow = vi.spyOn(services.nativeBridge.dialogs, "openWindow");
  const router = createAppRouter();
  router.update({ history: createMemoryHistory({ initialEntries: ["/"] }) });
  render(
    <TestAppProviders services={services}>
      <RouterProvider router={router} />
    </TestAppProviders>,
  );
  await act(async () => router.load());
  fireEvent.keyDown(window, { key: "N", ctrlKey: true, shiftKey: true });
  expect(router.state.location.pathname).toBe("/");
  expect(openWindow).not.toHaveBeenCalled();
});

it.each(["/projects/project-1/sessions/session-1", "/projects/project-1/chat"])(
  "opens a new window from an existing Session at %s",
  async (path) => {
    const services = createTestServices([]);
    const settings = vi.spyOn(services.api.chat, "getSettings").mockResolvedValue(sessionSettingsRead());
    const openWindow = vi.spyOn(services.nativeBridge.dialogs, "openWindow").mockResolvedValue("created");
    const history = createMemoryHistory({ initialEntries: [path] });
    history.replace(path, {
      sessionChat: { projectID: "project-1", deliveredSessionID: "session-1", catalogOrigin: null },
    });
    const router = createAppRouter();
    router.update({ history });
    render(
      <TestAppProviders services={services}>
        <RouterProvider router={router} />
      </TestAppProviders>,
    );
    await act(async () => router.load());
    fireEvent.keyDown(window, { key: "n", ctrlKey: true });
    await waitFor(() => {
      expect(openWindow).toHaveBeenCalledTimes(1);
    });
    expect(settings).toHaveBeenCalledWith({
      kind: "session",
      projectID: "project-1",
      sessionID: "session-1",
    });
    expect(router.state.location.pathname).toBe(path);
  },
);
