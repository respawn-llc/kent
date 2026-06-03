import { act, render, screen } from "@testing-library/react";
import { CircleOff } from "lucide-react";
import { afterEach, vi } from "vitest";

import { Button } from "./Button";
import { EmptyState, ErrorState, LoadingState } from "./StateViews";

describe("EmptyState", () => {
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

    expect(screen.getByRole("button", { name: "Legacy create" })).toBeInTheDocument();
  });
});

describe("LoadingState", () => {
  afterEach(() => {
    vi.useRealTimers();
  });

  it("renders centered spinner, optional title, and optional description", () => {
    render(<LoadingState appearanceDelayMs={0} body="Booting." reveal={false} title="Loading" />);

    expect(screen.getByRole("heading", { name: "Loading" })).toBeInTheDocument();
    expect(screen.getByText("Booting.")).toBeInTheDocument();
    expect(screen.getByTestId("loading-state-spinner")).toBeInTheDocument();
  });

  it("omits loading title and description by default", () => {
    render(<LoadingState appearanceDelayMs={0} />);

    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
    expect(screen.getByTestId("loading-state-spinner")).toBeInTheDocument();
  });

  it("delays loading state reveal to avoid fast-loading flicker", () => {
    vi.useFakeTimers();
    render(<LoadingState appearanceDelayKey="state-view-delay-test" body="Booting." title="Loading" />);

    expect(screen.queryByTestId("loading-state")).not.toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(499);
    });

    expect(screen.queryByTestId("loading-state")).not.toBeInTheDocument();

    act(() => {
      vi.advanceTimersByTime(1);
    });

    expect(screen.getByTestId("loading-state")).toBeInTheDocument();
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
  });
});

describe("ErrorState", () => {
  it("renders the error icon, title, and description", () => {
    render(<ErrorState body="Failed." reveal={false} title="Error" />);

    expect(screen.getByRole("heading", { name: "Error" })).toBeInTheDocument();
    expect(screen.getByText("Failed.")).toBeInTheDocument();
  });

  it("omits error title and description by default", () => {
    render(<ErrorState />);

    expect(screen.queryByRole("heading")).not.toBeInTheDocument();
  });

  it("preserves multiline details for stacktraces", () => {
    render(<ErrorState body="Failed." details={"line 1\nline 2"} title="Error" />);

    expect(screen.getByTestId("error-state-details").textContent).toBe("line 1\nline 2");
  });
});
