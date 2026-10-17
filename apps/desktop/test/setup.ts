import { GlobalRegistrator } from "@happy-dom/global-registrator";
import { afterEach, expect, vi } from "bun:test";

GlobalRegistrator.register({ url: "http://localhost:1420/" });

const { cleanup } = await import("@testing-library/react");
const { default: matcherExports, ...matchers } = await import("@testing-library/jest-dom/matchers");
void matcherExports;
expect.extend(matchers);
afterEach(() => {
  cleanup();
  document.body.innerHTML = "";
});

Object.defineProperty(window, "scrollTo", { configurable: true, value: vi.fn() });

// Happy DOM has no layout engine. Supply a viewport and row geometry so the real
// virtualizer owns ranges in component tests too. Geometry-specific tests
// override these configurable getters with their own measurements.
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
    // The component tests only need the observer API to exist.
  }

  observe(): void {
    // The component tests only need the observer API to exist.
  }

  unobserve(): void {
    // The component tests only need the observer API to exist.
  }
}

Object.defineProperty(globalThis, "ResizeObserver", {
  configurable: true,
  value: TestResizeObserver,
  writable: true,
});
