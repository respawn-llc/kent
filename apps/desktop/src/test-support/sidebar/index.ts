import {
  createMemoryHistory,
  createRootRoute,
  createRouter,
  RouterContextProvider,
} from "@tanstack/react-router";
import type {
  SidebarDestination,
  SidebarPageNavigator,
  SidebarRootController,
  SidebarShellController,
} from "@/app-facade";
import { SidebarRootOwner } from "@/app-facade";
import { AppChrome } from "@/app";
import { createElement, type ReactNode } from "react";
import { useState } from "react";

export function TestSidebar({ children }: Readonly<{ children: ReactNode }>) {
  const [router] = useState(() =>
    createRouter({
      history: createMemoryHistory({ initialEntries: ["/"] }),
      routeTree: createRootRoute(),
    }),
  );
  return createElement(RouterContextProvider, {
    router,
    children: createElement(AppChrome, {
      children: createElement(SidebarRootOwner, { children }),
    }),
  });
}

export function createTestSidebarNavigator(
  overrides: Partial<SidebarPageNavigator> = {},
): SidebarPageNavigator {
  return {
    back: vi.fn(() => "accepted" as const),
    close: vi.fn(() => "accepted" as const),
    push: vi.fn(() => "accepted" as const),
    registerAvailability: vi.fn(() => () => undefined),
    registerCapture: vi.fn(() => () => undefined),
    replace: vi.fn(() => "accepted" as const),
    ...overrides,
  };
}

export function createTestSidebarController(
  onOpen: (destination: SidebarDestination) => void = () => {
    return;
  },
): SidebarRootController {
  return {
    open(destination) {
      onOpen(destination);
      return { lifecycle: Promise.resolve("closed"), release: () => undefined };
    },
  };
}

export function createTestSidebarShell(): SidebarShellController {
  return {
    currentSurface: () => null,
    activeDestination: null,
    back: () => "unavailable",
    backAvailable: false,
    canGoBack: false,
    close: () => "unavailable",
    closeAvailable: false,
    phase: "open",
    resize: vi.fn(),
    sidebarWidthPx: 400,
    transitionDirection: null,
  };
}
