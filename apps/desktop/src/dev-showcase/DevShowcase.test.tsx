import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach } from "vitest";

import { DevShowcaseApp } from "./DevShowcase";

describe("DevShowcaseApp", () => {
  const originalResizeObserver = globalThis.ResizeObserver;

  beforeEach(() => {
    globalThis.ResizeObserver = MockResizeObserver;
  });

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver;
    document.documentElement.removeAttribute("data-theme");
  });

  it("renders single-page UI inventory with mock data", async () => {
    render(<DevShowcaseApp />);

    expect(await screen.findByTestId("dev-showcase-scroll-root")).toBeInTheDocument();
    expect(screen.getAllByTestId(/^showcase-section-/u).length).toBeGreaterThan(3);
  });

  it("does not render the removed handrolled toast stack in the showcase", async () => {
    render(<DevShowcaseApp />);

    expect(await screen.findByTestId("dev-showcase-scroll-root")).toBeInTheDocument();
    expect(screen.queryAllByTestId("dev-showcase-toast-example")).toHaveLength(0);
  });
});

class MockResizeObserver implements ResizeObserver {
  observe(): void {
    return;
  }

  unobserve(): void {
    return;
  }

  disconnect(): void {
    return;
  }
}
