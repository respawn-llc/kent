import { act, fireEvent, render, screen } from "@testing-library/react";
import { vi } from "vitest";

import { BoardDependencyProgressChip } from "./BoardDependencyProgressChip";

describe("BoardDependencyProgressChip", () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  it("isolates chip activation from its card and exposes server progress", async () => {
    const onActivate = vi.fn();
    const onCardActivate = vi.fn();

    render(
      <div onClick={onCardActivate}>
        <BoardDependencyProgressChip
          onActivate={onActivate}
          progress={{ satisfiedCount: 1, totalCount: 2 }}
        />
      </div>,
    );

    const chip = screen.getByRole("button");
    chip.focus();
    expect(chip).toHaveFocus();
    expect(screen.queryByRole("tooltip")).not.toBeInTheDocument();
    fireEvent.mouseEnter(chip);
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    const tooltip = screen.getByRole("tooltip");
    expect(tooltip).not.toBeEmptyDOMElement();

    fireEvent.click(chip);

    expect(onActivate).toHaveBeenCalledOnce();
    expect(onCardActivate).not.toHaveBeenCalled();
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuenow", "1");
    expect(screen.getByRole("progressbar")).toHaveAttribute("aria-valuemax", "2");
  });
});
