import { act, render, screen } from "@testing-library/react";
import { CircleOff } from "lucide-react";
import { afterEach, vi } from "vitest";

import { Button } from "./Button";
import { EmptyState, ErrorState, LoadingState } from "./StateViews";

describe("EmptyState", () => {
  it("fills the available page space and centers default empty content", () => {
    render(<EmptyState body="Nothing to do yet." title="No items yet" />);

    expect(screen.getByTestId("empty-state")).toHaveClass(
      "h-full",
      "min-h-0",
      "place-items-center",
    );
    expect(screen.getByTestId("empty-state")).not.toHaveClass("p-[var(--space-2)]");
    expect(screen.getByTestId("empty-state-island")).toHaveClass(
      "h-full",
      "min-h-0",
      "p-[var(--space-4)]",
      "place-items-center",
    );
    expect(screen.getByTestId("empty-state")).not.toHaveClass("animate-[surface-reveal_var(--motion-normal)]");
    expect(screen.getByTestId("empty-state-content")).toHaveClass("justify-items-center", "text-center");
    expect(screen.getByTestId("empty-state-icon")).not.toBeEmptyDOMElement();
    expect(screen.getByRole("heading", { name: "No items yet" })).toHaveClass("font-bold");
    expect(screen.getByText("Nothing to do yet.")).toHaveClass("text-[var(--color-muted)]");
  });

  it("can own chrome padding for edge-to-edge shell fallback states", () => {
    render(<EmptyState body="Nothing to do yet." chromePadding title="No items yet" />);

    expect(screen.getByTestId("empty-state")).toHaveClass("p-[var(--space-2)]");
    expect(screen.getByTestId("empty-state-island")).toHaveClass("p-[var(--space-4)]");
  });

  it("can stretch content across full-screen islands", () => {
    render(
      <EmptyState
        body="Nothing to do yet."
        chromePadding
        contentWidth="full"
        title="No items yet"
      />,
    );

    expect(screen.getByTestId("empty-state-island")).toHaveClass("h-full", "w-full");
    expect(screen.getByTestId("empty-state-content")).toHaveClass("w-full", "max-w-none");
    expect(screen.getByTestId("empty-state-content")).not.toHaveClass("max-w-[560px]");
  });

  it("renders custom icon and action flow row", () => {
    render(
      <EmptyState
        actions={
          <>
            <Button variant="primary">Create</Button>
            <Button>Import</Button>
          </>
        }
        body="Set up a workflow before creating tasks."
        icon={<CircleOff data-testid="custom-empty-icon" size={24} />}
        title="No workflows yet"
      />,
    );

    expect(screen.getByTestId("custom-empty-icon")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Import" })).toBeInTheDocument();
    expect(screen.getByTestId("empty-state-actions")).toHaveClass(
      "flex",
      "flex-wrap",
      "justify-center",
    );
  });

  it("can render compact showcase states and legacy action content", () => {
    render(
      <EmptyState
        action={<Button variant="primary">Legacy create</Button>}
        body="Embedded empty state."
        fullPage={false}
        title="Empty card"
      />,
    );

    expect(screen.getByTestId("empty-state")).not.toHaveClass("h-full");
    expect(screen.getByTestId("empty-state")).not.toHaveClass("min-h-0");
    expect(screen.getByTestId("empty-state")).not.toHaveClass("overflow-hidden");
    expect(screen.getByTestId("empty-state")).toHaveClass("animate-[surface-reveal_var(--motion-normal)]");
    expect(screen.getByRole("button", { name: "Legacy create" })).toBeInTheDocument();
    expect(screen.getByTestId("empty-state-actions")).toHaveClass("flex", "flex-wrap", "justify-center");
  });
});

describe("LoadingState", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders centered spinner, optional title, and optional description", () => {
    render(<LoadingState appearanceDelayMs={0} body="Booting." reveal={false} title="Loading" />);

    expect(screen.getByTestId("loading-state")).not.toHaveClass("animate-[surface-reveal_var(--motion-normal)]");
    expect(screen.getByTestId("loading-state")).toHaveClass(
      "h-full",
      "min-h-0",
      "place-items-center",
    );
    expect(screen.getByTestId("loading-state")).not.toHaveClass("p-[var(--space-2)]");
    expect(screen.getByTestId("loading-state-island")).toHaveClass(
      "h-full",
      "min-h-0",
      "p-[var(--space-4)]",
      "place-items-center",
    );
    expect(screen.getByTestId("loading-state-content")).toHaveClass("justify-items-center", "text-center");
    expect(screen.getByTestId("loading-state-spinner")).toHaveClass("motion-safe:animate-spin");
    expect(screen.getByRole("heading", { name: "Loading" })).toHaveClass("font-bold");
    expect(screen.getByText("Booting.")).toHaveClass("text-[var(--color-muted)]");
  });

  it("omits loading title and description by default", () => {
    render(<LoadingState appearanceDelayMs={0} />);

    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
    expect(screen.getByTestId("loading-state-spinner")).toBeInTheDocument();
  });

  it("can stretch loading content across full-screen islands", () => {
    render(<LoadingState appearanceDelayMs={0} contentWidth="full" title="Loading" />);

    expect(screen.getByTestId("loading-state-island")).toHaveClass("h-full", "w-full");
    expect(screen.getByTestId("loading-state-island")).not.toHaveClass("max-w-[760px]", "m-auto");
    expect(screen.getByTestId("loading-state-content")).toHaveClass("w-full", "max-w-none");
    expect(screen.getByTestId("loading-state-content")).not.toHaveClass("max-w-[560px]");
  });

  it("can render compact loading states for embedded panels", () => {
    render(<LoadingState appearanceDelayMs={0} body="Embedded loading." fullPage={false} title="Loading" />);

    expect(screen.getByTestId("loading-state")).not.toHaveClass("h-full");
    expect(screen.getByTestId("loading-state")).not.toHaveClass("min-h-0");
    expect(screen.getByTestId("loading-state")).not.toHaveClass("overflow-hidden");
    expect(screen.getByTestId("loading-state-content")).toHaveClass("justify-items-center", "text-center");
  });

  it("delays loading layout appearance to avoid fast-loading flicker", () => {
    vi.useFakeTimers();
    render(<LoadingState appearanceDelayKey="state-view-delay-test" body="Booting." title="Loading" />);

    expect(screen.getByTestId("loading-state-placeholder")).toHaveClass("h-full", "min-h-0");
    expect(screen.queryByTestId("loading-state")).not.toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(499);
    });

    expect(screen.queryByTestId("loading-state")).not.toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(1);
    });

    expect(screen.getByTestId("loading-state")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Loading" })).toBeInTheDocument();
  });

  it("keeps delayed loading placeholder aligned with chrome-padded loading layout", () => {
    vi.useFakeTimers();
    render(
      <LoadingState
        appearanceDelayKey="state-view-chrome-delay-test"
        body="Booting."
        chromePadding
        title="Loading"
      />,
    );

    expect(screen.getByTestId("loading-state-placeholder")).toHaveClass(
      "h-full",
      "min-h-0",
      "p-[var(--space-2)]",
    );
    expect(screen.queryByTestId("loading-state")).not.toBeInTheDocument();
  });

  it("does not delay later loading remounts for the same state key", () => {
    vi.useFakeTimers();
    const key = "state-view-remount-test";
    const view = render(<LoadingState appearanceDelayKey={key} title="Loading" />);

    act(() => {
      vi.advanceTimersByTime(500);
    });

    expect(screen.getByTestId("loading-state")).toBeInTheDocument();
    view.unmount();
    render(<LoadingState appearanceDelayKey={key} title="Loading again" />);

    expect(screen.getByTestId("loading-state")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Loading again" })).toBeInTheDocument();
  });
});

describe("ErrorState", () => {
  it("renders centered error icon, error title, and normal-color description", () => {
    render(<ErrorState body="Failed." reveal={false} title="Error" />);

    expect(screen.getByTestId("error-state")).not.toHaveClass("animate-[surface-reveal_var(--motion-normal)]");
    expect(screen.getByTestId("error-state")).toHaveClass(
      "h-full",
      "min-h-0",
      "place-items-center",
    );
    expect(screen.getByTestId("error-state")).not.toHaveClass("p-[var(--space-2)]");
    expect(screen.getByTestId("error-state-island")).toHaveClass(
      "h-full",
      "min-h-0",
      "p-[var(--space-4)]",
      "place-items-center",
    );
    expect(screen.getByTestId("error-state-icon")).toHaveClass("text-[var(--color-error)]");
    expect(screen.getByRole("heading", { name: "Error" })).toHaveClass(
      "font-bold",
      "text-[var(--color-error)]",
    );
    expect(screen.getByText("Failed.")).toHaveClass("text-[var(--color-on-island)]");
  });

  it("omits error title and description by default", () => {
    render(<ErrorState />);

    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
    expect(screen.getByTestId("error-state-icon")).toHaveClass("text-[var(--color-error)]");
  });

  it("can render compact error states for embedded panels", () => {
    render(<ErrorState body="Embedded error." fullPage={false} title="Error" />);

    expect(screen.getByTestId("error-state")).not.toHaveClass("h-full");
    expect(screen.getByTestId("error-state")).not.toHaveClass("min-h-0");
    expect(screen.getByTestId("error-state")).not.toHaveClass("overflow-hidden");
    expect(screen.getByTestId("error-state-content")).toHaveClass("justify-items-center", "text-center");
  });

  it("preserves multiline details for stacktraces", () => {
    render(<ErrorState body="Failed." details={"line 1\nline 2"} title="Error" />);

    expect(screen.getByTestId("error-state-details")).toHaveTextContent("line 1 line 2");
    expect(screen.getByTestId("error-state-details").textContent).toBe("line 1\nline 2");
    expect(screen.getByTestId("error-state-details")).toHaveClass("whitespace-pre-wrap", "font-mono");
  });
});
