import { afterEach, vi } from "vitest";

import { runNavigationTransition } from "./navigationTransitions";

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

  it("uses the View Transition API for default navigation", async () => {
    installMatchMedia(false);
    const startViewTransition = vi.fn((update: () => void | Promise<void>): ViewTransitionTestHandle => {
      const updateCallbackDone = Promise.resolve(update());
      return {
        finished: updateCallbackDone,
        ready: Promise.resolve(),
        updateCallbackDone,
      };
    });
    installStartViewTransition(startViewTransition);

    const update = vi.fn();
    await runNavigationTransition(update);

    expect(startViewTransition).toHaveBeenCalledOnce();
    expect(update).toHaveBeenCalledOnce();
  });

  it("falls back to immediate navigation when reduced motion is enabled", async () => {
    installMatchMedia(true);
    const startViewTransition = vi.fn();
    installStartViewTransition(startViewTransition);
    const update = vi.fn();

    await runNavigationTransition(update);

    expect(startViewTransition).not.toHaveBeenCalled();
    expect(update).toHaveBeenCalledOnce();
  });
});

type ViewTransitionTestHandle = Readonly<{
  finished: Promise<void>;
  ready: Promise<void>;
  updateCallbackDone: Promise<void>;
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

function installStartViewTransition(
  startViewTransition: (update: () => void | Promise<void>) => ViewTransitionTestHandle,
): void {
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
