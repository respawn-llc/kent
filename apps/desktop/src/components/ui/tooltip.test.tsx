import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach } from "vitest";

import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from "./tooltip";

describe("Tooltip", () => {
  const originalResizeObserver = globalThis.ResizeObserver;

  beforeEach(() => {
    globalThis.ResizeObserver = MockResizeObserver;
  });

  afterEach(() => {
    globalThis.ResizeObserver = originalResizeObserver;
  });

  it("uses Builder island glass instead of the default shadcn foreground panel", () => {
    render(
      <TooltipProvider>
        <Tooltip open>
          <TooltipTrigger>Node metadata</TooltipTrigger>
          <TooltipContent data-testid="tooltip-content">Backlog metadata</TooltipContent>
        </Tooltip>
      </TooltipProvider>,
    );

    const tooltipContent = screen.getByTestId("tooltip-content");
    expect(tooltipContent).toHaveClass("island-surface", "island-surface-0");
    expect(tooltipContent).toHaveClass("text-[var(--color-on-island)]");
    expect(tooltipContent).not.toHaveClass("bg-foreground");
    expect(screen.getByTestId("tooltip-arrow")).toHaveClass("fill-[var(--color-island-0)]");
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
