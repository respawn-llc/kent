import { afterEach, beforeEach, expect, it, mock, vi } from "bun:test";
import { act, fireEvent, render, waitFor } from "@testing-library/react";
import { createMemoryHistory, Outlet, RouterProvider } from "@tanstack/react-router";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { MainNewSessionShortcuts } from "./MainNewSessionShortcuts";
import { sessionSettingsRead } from "@/test-support/chat-destination";
import { QueryClient } from "@tanstack/react-query";

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
beforeEach(() => {
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
});

it("ignores both shortcuts in a not-yet-created popup Chat", async () => {
  const services = createTestServices([]);
  const client = new QueryClient();
  vi.spyOn(services.nativeBridge.app, "isMainWindow").mockReturnValue(false);
  const openWindow = vi.spyOn(services.nativeBridge.dialogs, "openWindow").mockResolvedValue("created");
  const path = "/projects/project-1/chat";
  const router = createAppRouter();
  const history = createMemoryHistory({ initialEntries: [path] });
  router.update({ history });
  render(
    <TestAppProviders services={services} queryClient={client}>
      <RouterProvider router={router} />
    </TestAppProviders>,
  );
  await act(async () => router.load());
  fireEvent.keyDown(window, { key: "n", ctrlKey: true });
  fireEvent.keyDown(window, { key: "N", ctrlKey: true, shiftKey: true });
  await waitFor(() => {
    expect(client.isMutating()).toBe(0);
  });
  expect(openWindow).not.toHaveBeenCalled();
  expect(history.length).toBe(1);
});

it("allows only Shift+N to pop out the main New Chat page and rechecks focus for queued presses", async () => {
  const services = createTestServices([]);
  const client = new QueryClient();
  vi.spyOn(services.nativeBridge.app, "isMainWindow").mockReturnValue(true);
  const openWindow = vi.spyOn(services.nativeBridge.dialogs, "openWindow").mockImplementation(async () => {
    vi.spyOn(document, "hasFocus").mockReturnValue(false);
    return "created";
  });
  const router = createAppRouter();
  const history = createMemoryHistory({ initialEntries: ["/projects/project-1/chat"] });
  router.update({ history });
  render(
    <TestAppProviders services={services} queryClient={client}>
      <RouterProvider router={router} />
    </TestAppProviders>,
  );
  await act(async () => router.load());
  fireEvent.keyDown(window, { key: "n", ctrlKey: true });
  fireEvent.keyDown(window, { key: "N", ctrlKey: true, shiftKey: true });
  fireEvent.keyDown(window, { key: "N", ctrlKey: true, shiftKey: true });
  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledTimes(1);
  });
  await waitFor(() => {
    expect(client.isMutating()).toBe(0);
  });
  expect(history.length).toBe(1);
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
