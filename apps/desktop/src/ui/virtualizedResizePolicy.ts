import type { Virtualizer } from "@tanstack/react-virtual";

export function resolveVirtualizedResizePolicy(
  nonAdjustingItemKey: string | undefined,
  endAnchored: boolean,
): Virtualizer<HTMLDivElement, Element>["shouldAdjustScrollPositionOnItemSizeChange"] {
  if (nonAdjustingItemKey === undefined && !endAnchored) return undefined;
  return (item, delta, instance) =>
    nonAdjustingItemKey !== item.key &&
    // First measurements replace estimates, including the estimated row that
    // spans the fold. Later resizes must not drag a reader inside a growing row.
    (!endAnchored ||
      (instance.scrollOffset !== null &&
        (instance.itemSizeCache.has(item.key) ? item.end + delta : item.start) <=
          instance.scrollOffset + instance.scrollAdjustments));
}
