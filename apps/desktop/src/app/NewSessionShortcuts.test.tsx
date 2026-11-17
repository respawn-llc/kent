import { afterEach, expect, it, vi } from "bun:test";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { NewSessionShortcuts } from "./NewSessionShortcuts";
import { SidebarShellContext } from "@/app-facade";
import { createTestSidebarShell } from "@/test-support/sidebar";
import { sessionSettingsRead } from "@/test-support/chat-destination";
import { deferred } from "@/test-support/chat-runtime";
import { createTaskDetailTestServices, taskDetailResponse } from "@/test-support/task-detail";

afterEach(() => {
  vi.restoreAllMocks();
});

it("opens a default-Agent Chat in the selected Project without creating a Session yet", async () => {
  const services = createTestServices([]);
  const openNewChat = vi.fn(async () => undefined);
  render(
    <TestAppProviders services={services}>
      <NewSessionShortcuts source={{ kind: "project", projectID: "project-1" }} openNewChat={openNewChat} />
    </TestAppProviders>,
  );
  fireEvent.keyDown(window, { key: "n", ctrlKey: true });
  await waitFor(() => {
    expect(openNewChat).toHaveBeenCalledWith("project-1");
  });
});

it.each(["linux", "windows", "macos"] as const)("opens Shift+N in a new window on %s", async (platform) => {
  const services = createTestServices([], undefined, { platform });
  const openWindow = vi.spyOn(services.nativeBridge.dialogs, "openWindow").mockResolvedValue("created");
  const openNewChat = vi.fn(async () => undefined);
  render(
    <TestAppProviders services={services}>
      <NewSessionShortcuts source={{ kind: "project", projectID: "project-1" }} openNewChat={openNewChat} />
    </TestAppProviders>,
  );
  fireEvent.keyDown(window, {
    key: "N",
    shiftKey: true,
    metaKey: platform === "macos",
    ctrlKey: platform !== "macos",
  });
  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledTimes(1);
  });
  expect(openWindow.mock.calls[0]?.[0]).toMatchObject({
    route: "/projects/project-1/chat",
    params: {},
    chrome: "app",
    resizable: true,
  });
  expect(openNewChat).not.toHaveBeenCalled();
});

it("uses the current Session's Agent for a new window without copying its settings", async () => {
  const services = createTestServices([]);
  const read = sessionSettingsRead();
  vi.spyOn(services.api.chat, "getSettings").mockResolvedValue({
    ...read,
    settings: { ...read.settings, selectedAgent: { ...read.settings.selectedAgent, role: "reviewer" } },
  });
  const openWindow = vi.spyOn(services.nativeBridge.dialogs, "openWindow").mockResolvedValue("created");
  const openNewChat = vi.fn(async () => undefined);
  render(
    <TestAppProviders services={services}>
      <NewSessionShortcuts
        source={{ kind: "session", projectID: "project-1", sessionID: "session-1" }}
        openNewChat={openNewChat}
      />
    </TestAppProviders>,
  );
  fireEvent.keyDown(window, { key: "n", ctrlKey: true });
  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledTimes(1);
  });
  expect(openWindow.mock.calls[0]?.[0]).toMatchObject({ params: { agentRole: "reviewer" } });
  expect(openNewChat).not.toHaveBeenCalled();
});

it("gives the sidebar Project authority over Chat and uses the default Agent", async () => {
  const services = createTestServices([]);
  const read = vi.spyOn(services.api.chat, "getSettings");
  const openNewChat = vi.fn(async () => undefined);
  const sidebar = {
    ...createTestSidebarShell(),
    activeDestination: { kind: "projectEdit", projectID: "sidebar-project" } as const,
  };
  render(
    <TestAppProviders services={services}>
      <SidebarShellContext value={sidebar}>
        <NewSessionShortcuts
          source={{ kind: "session", projectID: "behind-project", sessionID: "session-1" }}
          openNewChat={openNewChat}
        />
      </SidebarShellContext>
    </TestAppProviders>,
  );
  fireEvent.keyDown(window, { key: "n", ctrlKey: true });
  await waitFor(() => {
    expect(openNewChat).toHaveBeenCalledWith("sidebar-project");
  });
  expect(read).not.toHaveBeenCalled();
});

it("does nothing without a Project, even when the page behind an ambiguous sidebar has one", () => {
  const services = createTestServices([]);
  const openNewChat = vi.fn(async () => undefined);
  const { rerender } = render(
    <TestAppProviders services={services}>
      <NewSessionShortcuts source={null} openNewChat={openNewChat} />
    </TestAppProviders>,
  );
  fireEvent.keyDown(window, { key: "n", ctrlKey: true });
  rerender(
    <TestAppProviders services={services}>
      <SidebarShellContext
        value={{ ...createTestSidebarShell(), activeDestination: { kind: "workflowCreate" } }}
      >
        <NewSessionShortcuts
          source={{ kind: "project", projectID: "behind-project" }}
          openNewChat={openNewChat}
        />
      </SidebarShellContext>
    </TestAppProviders>,
  );
  fireEvent.keyDown(window, { key: "n", ctrlKey: true });
  expect(openNewChat).not.toHaveBeenCalled();
});

it("ignores repeats, composition, and other modifiers while admitting only one pending opening", async () => {
  const services = createTestServices([]);
  const opening = deferred<undefined>();
  const openNewChat = vi.fn(async () => {
    await opening.promise;
  });
  render(
    <TestAppProviders services={services}>
      <NewSessionShortcuts source={{ kind: "project", projectID: "project-1" }} openNewChat={openNewChat} />
    </TestAppProviders>,
  );
  for (const extra of [{ repeat: true }, { isComposing: true }, { altKey: true }, { metaKey: true }]) {
    fireEvent.keyDown(window, { key: "n", ctrlKey: true, ...extra });
  }
  expect(openNewChat).not.toHaveBeenCalled();
  fireEvent.keyDown(window, { key: "n", ctrlKey: true });
  fireEvent.keyDown(window, { key: "n", ctrlKey: true });
  await waitFor(() => {
    expect(openNewChat).toHaveBeenCalledTimes(1);
  });
  opening.resolve(undefined);
});

it("resolves a Task sidebar's Project instead of using the page behind it", async () => {
  const services = createTaskDetailTestServices(taskDetailResponse);
  const openNewChat = vi.fn(async () => undefined);
  render(
    <TestAppProviders services={services}>
      <SidebarShellContext
        value={{ ...createTestSidebarShell(), activeDestination: { kind: "taskDetail", taskID: "task-1" } }}
      >
        <NewSessionShortcuts
          source={{ kind: "project", projectID: "behind-project" }}
          openNewChat={openNewChat}
        />
      </SidebarShellContext>
    </TestAppProviders>,
  );
  fireEvent.keyDown(window, { key: "n", ctrlKey: true });
  await waitFor(() => {
    expect(openNewChat).toHaveBeenCalledWith("project-1");
  });
});

it("permits another attempt after a failed window opening", async () => {
  const services = createTestServices([]);
  const openWindow = vi
    .spyOn(services.nativeBridge.dialogs, "openWindow")
    .mockRejectedValueOnce(new Error("window creation failed"))
    .mockResolvedValue("created");
  render(
    <TestAppProviders services={services}>
      <NewSessionShortcuts
        source={{ kind: "project", projectID: "project-1" }}
        openNewChat={async () => undefined}
      />
    </TestAppProviders>,
  );
  fireEvent.keyDown(window, { key: "N", ctrlKey: true, shiftKey: true });
  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledTimes(1);
  });
  await waitFor(() => {
    expect(screen.queryByTestId("new-session-opening")).toBeNull();
  });
  fireEvent.keyDown(window, { key: "N", ctrlKey: true, shiftKey: true });
  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledTimes(2);
  });
});
