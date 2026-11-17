import { afterEach, beforeEach, expect, it, vi } from "bun:test";
import { fireEvent, render, waitFor } from "@testing-library/react";
import { QueryClient } from "@tanstack/react-query";
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
beforeEach(() => {
  vi.spyOn(document, "hasFocus").mockReturnValue(true);
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

it.each([false, true])(
  "uses the current Session's Agent without copying settings (popup=%s)",
  async (newWindow) => {
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
    fireEvent.keyDown(window, { key: "n", ctrlKey: true, shiftKey: newWindow });
    await waitFor(() => {
      expect(newWindow ? openWindow : openNewChat).toHaveBeenCalledTimes(1);
    });
    if (newWindow) {
      expect(openWindow.mock.calls[0]?.[0]).toMatchObject({ params: { agentRole: "reviewer" } });
      expect(openNewChat).not.toHaveBeenCalled();
    } else {
      expect(openNewChat).toHaveBeenCalledWith("project-1", "reviewer");
      expect(openWindow).not.toHaveBeenCalled();
    }
  },
);

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

it("queues presses and rechecks the destination after the previous opening completes", async () => {
  const services = createTestServices([]);
  const client = new QueryClient();
  const opening = deferred<undefined>();
  const openNewChat = vi.fn(async () => {
    await opening.promise;
  });
  const view = render(
    <TestAppProviders services={services} queryClient={client}>
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
  view.rerender(
    <TestAppProviders services={services} queryClient={client}>
      <NewSessionShortcuts source={{ kind: "new_chat", projectID: "project-1" }} openNewChat={openNewChat} />
    </TestAppProviders>,
  );
  opening.resolve(undefined);
  await waitFor(() => {
    expect(client.isMutating()).toBe(0);
  });
  expect(openNewChat).toHaveBeenCalledTimes(1);
});

it("executes an already queued press after an earlier opening fails", async () => {
  const services = createTestServices([]);
  const opening = deferred<"created">();
  const openWindow = vi
    .spyOn(services.nativeBridge.dialogs, "openWindow")
    .mockReturnValueOnce(opening.promise)
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
  fireEvent.keyDown(window, { key: "N", ctrlKey: true, shiftKey: true });
  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledTimes(1);
  });
  opening.reject(new Error("window failed"));
  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledTimes(2);
  });
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
  const client = new QueryClient();
  const openWindow = vi
    .spyOn(services.nativeBridge.dialogs, "openWindow")
    .mockRejectedValueOnce(new Error("window creation failed"))
    .mockResolvedValue("created");
  render(
    <TestAppProviders services={services} queryClient={client}>
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
    expect(client.isMutating()).toBe(0);
  });
  fireEvent.keyDown(window, { key: "N", ctrlKey: true, shiftKey: true });
  await waitFor(() => {
    expect(openWindow).toHaveBeenCalledTimes(2);
  });
});
