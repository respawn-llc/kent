import type { VirtualItem } from "@tanstack/react-virtual";
import { resolveVirtualizedFlowBlock } from "./virtualizedPinnedRange";

const row = (index: number): VirtualItem => ({
  index,
  key: String(index),
  start: index * 80,
  end: (index + 1) * 80,
  size: 80,
  lane: 0,
});

it("keeps distant retained rows out of the visible block's flow", () => {
  const items = [0, 1, 40, 41, 42, 99].map(row);
  const block = resolveVirtualizedFlowBlock(items, { startIndex: 41 }, true);
  expect(block?.first.index).toBe(40);
  expect(block?.last.index).toBe(42);
});

it("joins retained rows to the flow when scrolling closes the gap", () => {
  const items = [0, 1, 97, 98, 99].map(row);
  const block = resolveVirtualizedFlowBlock(items, { startIndex: 98 }, true);
  expect(block?.first.index).toBe(97);
  expect(block?.last.index).toBe(99);
});

it("does not choose a retained row as the visible block when the viewport is unavailable", () => {
  expect(resolveVirtualizedFlowBlock([row(99)], null, true)).toBeUndefined();
  expect(resolveVirtualizedFlowBlock([], { startIndex: 0 }, true)).toBeUndefined();
  expect(resolveVirtualizedFlowBlock([row(0)], { startIndex: 0 }, false)).toBeUndefined();
});
