import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { I18nextProvider } from "react-i18next";
import { afterEach, beforeAll, vi } from "vitest";

import { appI18n, initializeI18n } from "../i18n/setup";
import { SidebarHost } from "./sidebar";
import { useSidebar } from "./sidebarContext";
import { SidebarProvider } from "./sidebarProvider";

describe("SidebarHost", () => {
  beforeAll(async () => {
    await initializeI18n();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("uses 35 percent of the window width for the initial width capped at 840px", async () => {
    const {
      restoreWindowWidth: restoreUncappedWindowWidth,
      sidebar: uncappedSidebar,
      unmount: unmountUncapped,
    } = await renderOpenSidebarAtWindowWidth(1600);
    try {
      expect(sidebarWidthStyle(uncappedSidebar)).toBe(560);
    } finally {
      unmountUncapped();
      restoreUncappedWindowWidth();
    }

    const {
      restoreWindowWidth: restoreCappedWindowWidth,
      sidebar: cappedSidebar,
      unmount: unmountCapped,
    } = await renderOpenSidebarAtWindowWidth(3000);
    try {
      expect(sidebarWidthStyle(cappedSidebar)).toBe(840);
    } finally {
      unmountCapped();
      restoreCappedWindowWidth();
    }
  });

  it("uses shift layout as the default destination mode", async () => {
    render(
      <I18nextProvider i18n={appI18n}>
        <SidebarProvider>
          <div className="relative flex min-h-0" data-testid="app-shell-content">
            <div className="min-w-0 flex-1">
              <OpenCustomSidebar />
            </div>
            <SidebarHost />
          </div>
        </SidebarProvider>
      </I18nextProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Open sidebar" }));

    const sidebar = await screen.findByRole("complementary", { name: "Settings" });
    expect(sidebar).toHaveAttribute("data-mode", "shift");
    expect(sidebar).toHaveClass(
      "app-sidebar-panel-shift",
      "h-[calc(100%-(var(--app-sidebar-inset)*2))]",
      "mr-[var(--app-sidebar-inset)]",
      "mt-[var(--app-sidebar-inset)]",
      "shrink-0",
    );
    expect(sidebar.style.getPropertyValue("--app-sidebar-inset")).toBe("var(--space-2)");
    expect(sidebar).not.toHaveClass("absolute", "app-sidebar-panel-overlay");
    expect(screen.getByRole("button", { name: "Close" })).toHaveClass("h-9", "w-9");
    expect(screen.getByRole("heading", { name: "Settings" })).toHaveClass("whitespace-nowrap");
    expect(screen.getByTestId("default-shift-content")).toBeInTheDocument();
  });

  it("resizes from the leading edge without requiring pointer-capture APIs", async () => {
    mockSidebarLayout();
    render(
      <I18nextProvider i18n={appI18n}>
        <SidebarProvider>
          <div className="relative flex min-h-0" data-testid="app-shell-content">
            <div className="min-w-0 flex-1">
              <OpenCustomSidebar />
            </div>
            <SidebarHost />
          </div>
        </SidebarProvider>
      </I18nextProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Open sidebar" }));

    const sidebar = await screen.findByRole("complementary", { name: "Settings" });
    const resizeHandle = screen.getByRole("separator", { name: "Resize sidebar" });
    const initialWidth = sidebarWidthStyle(sidebar);
    fireEvent.pointerDown(resizeHandle, { button: 0, clientX: 700, pointerId: 1 });
    fireEvent.pointerMove(resizeHandle, { clientX: 620, pointerId: 1 });
    fireEvent.pointerUp(resizeHandle, { clientX: 620, pointerId: 1 });

    expect(sidebarWidthStyle(sidebar)).toBe(initialWidth + 80);
    expect(resizeHandle).toHaveAttribute("aria-valuenow", String(initialWidth + 80));

    fireEvent.keyDown(resizeHandle, { key: "ArrowRight" });

    expect(sidebarWidthStyle(sidebar)).toBe(initialWidth + 48);
  });

  it("clamps the current width when the app shell narrows", async () => {
    let shellWidth = 1200;
    mockSidebarLayout(() => shellWidth);
    render(
      <I18nextProvider i18n={appI18n}>
        <SidebarProvider>
          <div className="relative flex min-h-0" data-testid="app-shell-content">
            <div className="min-w-0 flex-1">
              <OpenCustomSidebar />
            </div>
            <SidebarHost />
          </div>
        </SidebarProvider>
      </I18nextProvider>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Open sidebar" }));
    const sidebar = await screen.findByRole("complementary", { name: "Settings" });
    const resizeHandle = screen.getByRole("separator", { name: "Resize sidebar" });
    fireEvent.keyDown(resizeHandle, { key: "End" });
    await waitFor(() => {
      expect(sidebarWidthStyle(sidebar)).toBe(1020);
    });

    shellWidth = 760;
    act(() => {
      window.dispatchEvent(new Event("resize"));
    });

    await waitFor(() => {
      expect(sidebarWidthStyle(sidebar)).toBe(646);
    });
    expect(resizeHandle).toHaveAttribute("aria-valuemax", "646");
  });
});

function OpenCustomSidebar() {
  const { openSidebar } = useSidebar();

  return (
    <button
      onClick={() => {
        void openSidebar({
          content: <p data-testid="default-shift-content">Default shift content</p>,
          kind: "custom",
          title: "Settings",
        });
      }}
      type="button"
    >
      Open sidebar
    </button>
  );
}

function sidebarWidthStyle(sidebar: HTMLElement): number {
  return Number.parseInt(sidebar.style.getPropertyValue("--app-sidebar-width"), 10);
}

async function renderOpenSidebarAtWindowWidth(width: number): Promise<
  Readonly<{
    restoreWindowWidth(): void;
    sidebar: HTMLElement;
    unmount(): void;
  }>
> {
  const restoreWindowWidth = mockWindowWidth(width);
  const { unmount } = render(
    <I18nextProvider i18n={appI18n}>
      <SidebarProvider>
        <div className="relative flex min-h-0" data-testid="app-shell-content">
          <div className="min-w-0 flex-1">
            <OpenCustomSidebar />
          </div>
          <SidebarHost />
        </div>
      </SidebarProvider>
    </I18nextProvider>,
  );

  fireEvent.click(screen.getByRole("button", { name: "Open sidebar" }));

  return {
    restoreWindowWidth,
    sidebar: await screen.findByRole("complementary", { name: "Settings" }),
    unmount,
  };
}

function mockWindowWidth(width: number): () => void {
  const descriptor = Object.getOwnPropertyDescriptor(window, "innerWidth");
  Object.defineProperty(window, "innerWidth", { configurable: true, value: width });
  return () => {
    if (descriptor === undefined) {
      Reflect.deleteProperty(window, "innerWidth");
      return;
    }
    Object.defineProperty(window, "innerWidth", descriptor);
  };
}

function mockSidebarLayout(shellWidth: () => number = () => 1200): void {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function getBoundingClientRect(
    this: HTMLElement,
  ) {
    if (this instanceof HTMLElement && this.dataset.testid === "app-shell-content") {
      return domRect({ height: 720, width: shellWidth() });
    }
    return domRect({ height: 720, width: 560 });
  });
}

function domRect({ height, width }: Readonly<{ height: number; width: number }>): DOMRect {
  return {
    bottom: height,
    height,
    left: 0,
    right: width,
    top: 0,
    width,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  };
}
