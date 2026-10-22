import { render, screen } from "@testing-library/react";
import { RouterProvider } from "@tanstack/react-router";
import { expect, it, mock } from "bun:test";

import { newChatRoutePath, sessionChatRoutePath } from "@/app-facade";

globalThis.__KENT_TEST_ENV__.DEV = false;

await mock.module("./routeComponents", async () => {
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

const { createAppRouter } = await import("./routes");

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
