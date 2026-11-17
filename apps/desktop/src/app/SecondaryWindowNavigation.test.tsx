import { afterEach, expect, it, mock, vi } from "bun:test";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createMemoryHistory, RouterProvider } from "@tanstack/react-router";
import type { ReactNode } from "react";
import { createTaskDetailTestServices, taskDetailResponse } from "@/test-support/task-detail";
import { TestAppProviders } from "@/test-support/app-services";

const actualStartup = await import("@/features/startup");
await mock.module("@/features/startup", () => ({
  ...actualStartup,
  StartupGate: ({ children }: Readonly<{ children: ReactNode }>) => <>{children}</>,
}));
const actualChat = await import("@/features/chat");
await mock.module("@/features/chat", () => ({
  ...actualChat,
  ChatDestination: () => <div data-testid="chat-destination" />,
}));
const actualTask = await import("@/features/task-detail");
await mock.module("@/features/task-detail", () => ({
  ...actualTask,
  TaskDetailWindowRoute: () => <div data-testid="task-window" />,
}));
const { createAppRouter } = await import("./routes");

afterEach(() => vi.restoreAllMocks());

it("opens same-window Chat from a secondary Task window with its required shell", async () => {
  const path = "/native-dialog/task-detail?taskID=task-1";
  const services = createTaskDetailTestServices(taskDetailResponse, { path });
  vi.spyOn(services.nativeBridge.app, "isMainWindow").mockReturnValue(false);
  vi.spyOn(services.nativeBridge.window, "isFocused").mockResolvedValue(true);
  const router = createAppRouter();
  router.update({ history: createMemoryHistory({ initialEntries: [path] }) });
  render(<TestAppProviders services={services} skipAnimations><RouterProvider router={router} /></TestAppProviders>);
  await screen.findByTestId("task-window");
  fireEvent.keyDown(window, { key: "n", ctrlKey: true });
  await screen.findByTestId("chat-destination");
  expect(router.state.location.pathname).toBe("/projects/project-1/chat");
});

it("does not register main-window navigation or notification controllers in a secondary Chat window", async () => {
  const services = createTaskDetailTestServices(taskDetailResponse, { path: "/projects/project-1/chat" });
  vi.spyOn(services.nativeBridge.app, "isMainWindow").mockReturnValue(false);
  const navigation = vi.spyOn(services.nativeBridge.window, "mainNavigationRequests");
  const activations = vi.spyOn(services.nativeBridge.notifications, "activations");
  const router = createAppRouter();
  router.update({ history: createMemoryHistory({ initialEntries: ["/projects/project-1/chat"] }) });
  render(<TestAppProviders services={services} skipAnimations><RouterProvider router={router} /></TestAppProviders>);
  await screen.findByTestId("chat-destination");
  await act(async () => router.load());
  await waitFor(() => expect(navigation).not.toHaveBeenCalled());
  expect(activations).not.toHaveBeenCalled();
});
