import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, vi } from "vitest";

import { FloatingNoticeIsland } from "./FloatingNoticeIsland";

describe("FloatingNoticeIsland", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("keeps expanded content mounted while collapsed", () => {
    const onCollapsedChange = vi.fn();

    render(
      <FloatingNoticeIsland
        collapsed
        collapseLabel="Collapse"
        expandLabel="Expand"
        onCollapsedChange={onCollapsedChange}
        title="Notice"
      >
        <p>Persistent notice body</p>
      </FloatingNoticeIsland>,
    );

    const content = screen.getByTestId("floating-notice-content");
    expect(content).toHaveAttribute("aria-hidden", "true");
    expect(content).toHaveAttribute("inert");
    expect(screen.getByText("Persistent notice body")).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "Expand" }));

    expect(onCollapsedChange).toHaveBeenCalledWith(false);
  });

  it("keeps the collapsed affordance mounted while expanded", () => {
    render(
      <FloatingNoticeIsland
        collapsed={false}
        collapseLabel="Collapse"
        expandLabel="Expand"
        onCollapsedChange={vi.fn()}
        title="Notice"
      >
        <p>Visible notice body</p>
      </FloatingNoticeIsland>,
    );

    const collapsedButton = screen.getByTestId("floating-notice-collapsed-button");
    expect(collapsedButton).toHaveAttribute("inert");
    expect(screen.getByTestId("floating-notice-content")).not.toHaveAttribute("aria-hidden", "true");
  });

  it("keeps custom-sized expanded content hidden until the shell finishes expanding", () => {
    vi.useFakeTimers();
    const { rerender } = render(
      <FloatingNoticeIsland
        collapsed
        collapseLabel="Collapse"
        expandedClassName="floating-notice-expanded grid h-[204px] w-[min(300px,calc(100vw-var(--space-2)*2))] gap-[6px] rounded-[var(--radius-xl)] p-[var(--space-2)]"
        expandLabel="Expand"
        onCollapsedChange={vi.fn()}
        title="Legend"
        tone="neutral"
      >
        <p>Custom notice body</p>
      </FloatingNoticeIsland>,
    );
    const collapsedContent = screen.getByTestId("floating-notice-content");
    expect(collapsedContent).toHaveAttribute("aria-hidden", "true");
    expect(collapsedContent).toHaveAttribute("inert");

    rerender(
      <FloatingNoticeIsland
        collapsed={false}
        collapseLabel="Collapse"
        expandedClassName="floating-notice-expanded grid h-[204px] w-[min(300px,calc(100vw-var(--space-2)*2))] gap-[6px] rounded-[var(--radius-xl)] p-[var(--space-2)]"
        expandLabel="Expand"
        onCollapsedChange={vi.fn()}
        title="Legend"
        tone="neutral"
      >
        <p>Custom notice body</p>
      </FloatingNoticeIsland>,
    );

    expect(screen.getByTestId("floating-notice-content")).toBe(collapsedContent);
    expect(screen.getByTestId("floating-notice-shell")).toHaveAttribute("data-state", "expanding");
    expect(collapsedContent).toHaveAttribute("aria-hidden", "true");
    expect(collapsedContent).toHaveAttribute("inert");

    act(() => {
      vi.advanceTimersByTime(1_000);
    });

    expect(screen.getByTestId("floating-notice-shell")).toHaveAttribute("data-state", "expanded");
    expect(collapsedContent).not.toHaveAttribute("aria-hidden", "true");
    expect(collapsedContent).not.toHaveAttribute("inert");
    expect(screen.getByText("Custom notice body")).toBeInTheDocument();
  });

  it("hides expanded content before the first collapse frame", () => {
    vi.useFakeTimers();
    const { rerender } = render(
      <FloatingNoticeIsland
        collapsed={false}
        collapseLabel="Collapse"
        expandLabel="Expand"
        onCollapsedChange={vi.fn()}
        title="Notice"
      >
        <p>Visible notice body</p>
      </FloatingNoticeIsland>,
    );
    const content = screen.getByTestId("floating-notice-content");
    expect(content).not.toHaveAttribute("aria-hidden", "true");
    expect(content).not.toHaveAttribute("inert");

    rerender(
      <FloatingNoticeIsland
        collapsed
        collapseLabel="Collapse"
        expandLabel="Expand"
        onCollapsedChange={vi.fn()}
        title="Notice"
      >
        <p>Visible notice body</p>
      </FloatingNoticeIsland>,
    );

    expect(screen.getByTestId("floating-notice-shell")).toHaveAttribute("data-state", "collapsing");
    expect(content).toHaveAttribute("aria-hidden", "true");
    expect(content).toHaveAttribute("inert");

    act(() => {
      vi.advanceTimersByTime(1_000);
    });

    expect(screen.getByTestId("floating-notice-shell")).toHaveAttribute("data-state", "collapsed");
  });
});
