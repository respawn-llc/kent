import { act, fireEvent, render, renderHook, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";

import { useBoardDragAutoScroll } from "./BoardDragAutoScroll";

afterEach(() => {
  vi.useRealTimers();
  vi.unstubAllGlobals();
});

it("scrolls both board axes only during a card drag inside the board", () => {
  vi.useFakeTimers();
  vi.stubGlobal("requestAnimationFrame", (callback: FrameRequestCallback) =>
    window.setTimeout(() => {
      callback(performance.now());
    }, 16),
  );
  vi.stubGlobal("cancelAnimationFrame", (id: number) => {
    window.clearTimeout(id);
  });
  render(
    <div data-testid="board">
      <div data-testid="column" />
    </div>,
  );
  const root = screen.getByTestId("board");
  const column = screen.getByTestId("column");
  vi.spyOn(root, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 300, 300));
  vi.spyOn(column, "getBoundingClientRect").mockReturnValue(new DOMRect(200, 0, 100, 300));
  Object.defineProperties(root, { scrollWidth: { value: 900 }, clientWidth: { value: 300 } });
  Object.defineProperties(column, { scrollHeight: { value: 900 }, clientHeight: { value: 300 } });
  const rootRef = { current: root };
  const { result, rerender } = renderHook(({ active }) => useBoardDragAutoScroll({ active, rootRef }), {
    initialProps: { active: false },
  });
  act(() => {
    result.current.registerColumnScrollport("column", column);
  });
  const move = (clientX: number, clientY: number) => {
    fireEvent(document, new MouseEvent("pointermove", { bubbles: true, clientX, clientY }));
  };
  move(290, 290);
  act(() => {
    vi.advanceTimersByTime(160);
  });
  expect(root.scrollLeft).toBe(0);
  expect(column.scrollTop).toBe(0);

  rerender({ active: true });
  move(290, 290);
  act(() => {
    vi.advanceTimersByTime(160);
  });
  expect(root.scrollLeft).toBeGreaterThan(0);
  expect(column.scrollTop).toBeGreaterThan(0);
  const scrollLeft = root.scrollLeft;
  const scrollTop = column.scrollTop;

  move(350, 290);
  act(() => {
    vi.advanceTimersByTime(160);
  });
  expect(root.scrollLeft).toBe(scrollLeft);
  expect(column.scrollTop).toBe(scrollTop);

  rerender({ active: false });
  move(290, 290);
  act(() => {
    vi.advanceTimersByTime(160);
  });
  expect(root.scrollLeft).toBe(scrollLeft);
  expect(column.scrollTop).toBe(scrollTop);
});
