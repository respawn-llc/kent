import { afterEach, vi } from "vitest";

import { startProjectToBoardTransition } from "./navigationTransitions";

describe("navigationTransitions", () => {
  const originalMatchMedia = globalThis.matchMedia;
  const originalStartViewTransitionDescriptor = Object.getOwnPropertyDescriptor(
    document,
    "startViewTransition",
  );

  afterEach(() => {
    Object.defineProperty(globalThis, "matchMedia", {
      configurable: true,
      value: originalMatchMedia,
    });
    restoreStartViewTransition(originalStartViewTransitionDescriptor);
  });

  it("uses the View Transition API for project to board navigation without a shared element", () => {
    installMatchMedia(false);
    const source = document.createElement("article");
    const startViewTransition = vi.fn((update: () => void): ViewTransitionTestHandle => {
      expect(source.style.viewTransitionName).toBe("");
      update();
      return { finished: Promise.resolve() };
    });
    installStartViewTransition(startViewTransition);

    const update = vi.fn();
    startProjectToBoardTransition(source, update);

    expect(startViewTransition).toHaveBeenCalledOnce();
    expect(update).toHaveBeenCalledOnce();
    expect(source.style.viewTransitionName).toBe("");
  });

  it("falls back to immediate navigation when reduced motion is enabled", () => {
    installMatchMedia(true);
    const source = document.createElement("article");
    const startViewTransition = vi.fn();
    installStartViewTransition(startViewTransition);
    const update = vi.fn();

    startProjectToBoardTransition(source, update);

    expect(startViewTransition).not.toHaveBeenCalled();
    expect(update).toHaveBeenCalledOnce();
    expect(source.style.viewTransitionName).toBe("");
  });
});

type ViewTransitionTestHandle = Readonly<{
  finished: Promise<void>;
}>;

function installMatchMedia(matches: boolean): void {
  Object.defineProperty(globalThis, "matchMedia", {
    configurable: true,
    value: vi.fn(() => ({
      matches,
      media: "(prefers-reduced-motion: reduce)",
      onchange: null,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      addListener: vi.fn(),
      removeListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })),
  });
}

function installStartViewTransition(startViewTransition: (update: () => void) => ViewTransitionTestHandle): void {
  Object.defineProperty(document, "startViewTransition", {
    configurable: true,
    value: startViewTransition,
  });
}

function restoreStartViewTransition(descriptor: PropertyDescriptor | undefined): void {
  if (descriptor === undefined) {
    Reflect.deleteProperty(document, "startViewTransition");
    return;
  }
  Object.defineProperty(document, "startViewTransition", descriptor);
}
