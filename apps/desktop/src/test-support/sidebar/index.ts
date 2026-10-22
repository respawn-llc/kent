import { vi } from "bun:test";
import type {
  SidebarDestination,
  SidebarPageNavigator,
  SidebarRootController,
  SidebarShellController,
} from "@/app-facade";
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
  const open: SidebarRootController["open"] = (destination) => {
    onOpen(destination);
    return { lifecycle: Promise.resolve("closed"), release: () => undefined };
  };
  return { open, toggle: open };
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
