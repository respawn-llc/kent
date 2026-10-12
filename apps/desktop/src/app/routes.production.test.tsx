import { render, screen } from "@testing-library/react";
import { RouterProvider } from "@tanstack/react-router";
import { afterAll, expect, it, vi } from "vitest";

import { newChatRoutePath, sessionChatRoutePath } from "@/app-facade";
import { createAppRouter } from "./routes";

vi.hoisted(() => {
  vi.stubEnv("DEV", false);
});

afterAll(() => {
  vi.unstubAllEnvs();
});
vi.mock("./routeComponents", async () => {
  const { Outlet } = await import("@tanstack/react-router");
  return {
    ChatRoute: () => <div data-testid="chat-route" />,
    NewChatRoute: () => <div data-testid="new-chat-route" />,
    HomeShellRoute: () => null,
    ProjectRoute: () => null,
    ProjectTasksRoute: () => null,
    RootRoute: () => <Outlet />,
    TaskRoute: () => null,
    WorkflowEditorShellRoute: () => null,
    WorkflowLibraryShellRoute: () => null,
  };
});

it("opens Session Chat in production", async () => {
  const router = createAppRouter();

  await router.navigate({
    to: sessionChatRoutePath,
    params: { projectId: "project-1", sessionId: "session-1" },
  });
  render(<RouterProvider router={router} />);

  expect(await screen.findByTestId("chat-route")).toBeInTheDocument();
});

it("opens New Chat in production", async () => {
  const router = createAppRouter();
  await router.navigate({ to: newChatRoutePath, params: { projectId: "project-1" } });
  render(<RouterProvider router={router} />);
  expect(await screen.findByTestId("new-chat-route")).toBeInTheDocument();
});
