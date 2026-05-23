import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { createElement, useEffect, useState } from "react";

import { AppProviders } from "./AppProviders";
import { nextReachableHistoryIndex } from "./navigation";
import { useAppNavigation, type AppNavigation } from "./navigation";
import { createTestServices, startupRoutes } from "../testSupport/appServices";

describe("navigation stack state", () => {
  it("preserves forward availability after back and truncates it on push", () => {
    const afterPushes = nextReachableHistoryIndex(nextReachableHistoryIndex(0, "PUSH", 1), "PUSH", 2);
    const afterBack = nextReachableHistoryIndex(afterPushes, "BACK", 1);
    const canGoForwardAfterBack = 1 < afterBack;
    const afterPushFromBack = nextReachableHistoryIndex(afterBack, "PUSH", 2);
    const canGoForwardAfterPush = 2 < afterPushFromBack;

    expect(canGoForwardAfterBack).toBe(true);
    expect(canGoForwardAfterPush).toBe(false);
  });

  it("keeps the navigation API stable across component rerenders", async () => {
    const observed: AppNavigation[] = [];
    const rootRoute = createRootRoute({ component: Outlet });
    const indexRoute = createRoute({
      getParentRoute: () => rootRoute,
      path: "/",
      component: () => createElement(NavigationIdentityProbe, { observed }),
    });
    const router = createRouter({
      history: createMemoryHistory({ initialEntries: ["/"] }),
      routeTree: rootRoute.addChildren([indexRoute]),
    });

    render(
      createElement(AppProviders, {
        children: createElement(RouterProvider, { router }),
        services: createTestServices(startupRoutes),
      }),
    );

    await waitFor(() => {
      expect(observed).toHaveLength(1);
    });
    fireEvent.click(screen.getByRole("button", { name: /rerender/u }));

    await waitFor(() => {
      expect(observed).toHaveLength(2);
    });
    expect(observed[1]).toBe(observed[0]);
    expect(observed[1]?.openProjectTask).toBe(observed[0]?.openProjectTask);
  });
});

function NavigationIdentityProbe({ observed }: Readonly<{ observed: AppNavigation[] }>) {
  const [renderCount, setRenderCount] = useState(0);
  const navigation = useAppNavigation();

  useEffect(() => {
    observed.push(navigation);
  });

  return createElement(
    "button",
    {
      onClick: () => {
        setRenderCount((current) => current + 1);
      },
      type: "button",
    },
    `rerender ${renderCount.toString()}`,
  );
}
