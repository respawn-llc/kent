import { afterEach, expect, it, mock, vi } from "bun:test";
import { fireEvent, render, screen } from "@testing-library/react";
import { createRootRoute, createRouter, createMemoryHistory, RouterProvider } from "@tanstack/react-router";

import { useSidebarRoots } from "@/app-facade";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";

const actualChat = await import("@/features/chat");
await mock.module("@/features/chat", () => ({
  ...actualChat,
  ChatDestination: () => {
    const roots = useSidebarRoots();
    return (
      <button
        onClick={() =>
          roots.open({
            kind: "custom",
            title: "Context",
            content: () => null,
            sizing: { desiredWidthPx: 500, minWidthPx: 400 },
          })
        }
      >
        Open context
      </button>
    );
  },
}));
const { NativeChatRoute } = await import("./NativeChatRoute");

afterEach(() => {
  vi.restoreAllMocks();
});

it.each([
  [960, "shift"],
  [800, "overlay"],
] as const)("adapts popped-out Chat sidebar to its %ipx content pane", async (width, expected) => {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, width, 700));
  const router = createRouter({
    routeTree: createRootRoute({
      component: () => <NativeChatRoute projectID="project-1" sessionID="session-1" />,
    }),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  render(
    <TestAppProviders services={createTestServices([])} skipAnimations>
      <RouterProvider router={router} />
    </TestAppProviders>,
  );
  fireEvent.click(await screen.findByRole("button", { name: "Open context" }));
  expect(screen.getByRole("complementary").getAttribute("data-mode")).toBe(expected);
});
