import "@testing-library/jest-dom/vitest";
import { vi } from "vitest";

if (typeof window !== "undefined") {
  Object.defineProperty(window, "scrollTo", { configurable: true, value: vi.fn() });
}

// jsdom has no layout engine. Supply a viewport and row geometry so the real
// virtualizer owns ranges in component tests too. Geometry-specific tests
// override these configurable getters with their own measurements.
if (typeof HTMLElement !== "undefined")
  Object.defineProperties(HTMLElement.prototype, {
    offsetHeight: {
      configurable: true,
      get(this: HTMLElement) {
        return this.hasAttribute("data-index") ? 40 : 600;
      },
    },
    offsetWidth: {
      configurable: true,
      get(this: HTMLElement) {
        return this.hasAttribute("data-index") ? 160 : 800;
      },
    },
  });

class TestResizeObserver implements ResizeObserver {
  disconnect(): void {
    // jsdom has no layout engine; component tests only need the observer API to exist.
  }

  observe(): void {
    // jsdom has no layout engine; component tests only need the observer API to exist.
  }

  unobserve(): void {
    // jsdom has no layout engine; component tests only need the observer API to exist.
  }
}

Object.defineProperty(globalThis, "ResizeObserver", {
  configurable: true,
  value: TestResizeObserver,
  writable: true,
});
