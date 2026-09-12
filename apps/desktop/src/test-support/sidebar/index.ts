import type { SidebarDestination, SidebarPageNavigator, SidebarRootController } from "@/app-facade";
import { SidebarComposition } from "@/app";
import { createElement, type ReactNode } from "react";

export function TestSidebar({ children }: Readonly<{ children: ReactNode }>) {
  return createElement(SidebarComposition, { children });
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
