import type { Virtualizer } from "@tanstack/react-virtual";

export function resolveVirtualizedResizePolicy(
  nonAdjustingItemKey: string | undefined,
  endAnchored: boolean,
): Virtualizer<HTMLDivElement, Element>["shouldAdjustScrollPositionOnItemSizeChange"] {
  if (nonAdjustingItemKey === undefined && !endAnchored) return undefined;
  return (item, _delta, instance) =>
    nonAdjustingItemKey !== item.key &&
    // A first measurement can grow a message across the viewport's top.
    // Only rows entirely above it should move the reader's position.
    (!endAnchored ||
      (instance.scrollOffset !== null && item.end <= instance.scrollOffset + instance.scrollAdjustments));
}
