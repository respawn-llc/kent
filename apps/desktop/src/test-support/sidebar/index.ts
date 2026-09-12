import type { SidebarDestination, SidebarPageNavigator, SidebarRootController } from "@/app-facade";
import { SidebarRootOwner } from "@/app-facade";
import { SidebarHost } from "@/app/sidebar";
import { sidebarDestinationPolicy } from "@/app/sidebarDestinationPolicy";
import { SidebarProvider } from "@/app/sidebarProvider";
import { createElement, type ReactNode } from "react";

export function TestSidebar({ children }: Readonly<{ children: ReactNode }>) {
  return createElement(SidebarProvider, {
    policy: sidebarDestinationPolicy,
    children: [createElement(SidebarRootOwner, { children }), createElement(SidebarHost)],
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
