import { expect, it, vi } from "bun:test";
import { renderHook } from "@testing-library/react";
import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterContextProvider,
} from "@tanstack/react-router";
import type { ReactNode } from "react";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { useAppNavigation } from "./navigation";

it("preserves New Chat navigation failures for the initiating action", async () => {
  const services = createTestServices([]);
  const router = createRouter({
    routeTree: createRootRoute(),
    history: createMemoryHistory({ initialEntries: ["/"] }),
  });
  const failure = new Error("navigation failed");
  vi.spyOn(router, "navigate").mockRejectedValue(failure);
  const { result } = renderHook(() => useAppNavigation(), {
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <TestAppProviders services={services}>
        <RouterContextProvider router={router}>{children}</RouterContextProvider>
      </TestAppProviders>
    ),
  });
  const error = await result.current.openNewChat("project-1").catch((cause: unknown) => cause);
  expect(error).toBe(failure);
});
