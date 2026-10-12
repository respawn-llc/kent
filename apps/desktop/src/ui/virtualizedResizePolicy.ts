import type { Virtualizer } from "@tanstack/react-virtual";

export function resolveVirtualizedResizePolicy(
  nonAdjustingItemKey: string | undefined,
  endAnchored: boolean,
): Virtualizer<HTMLDivElement, Element>["shouldAdjustScrollPositionOnItemSizeChange"] {
  if (nonAdjustingItemKey === undefined && !endAnchored) return undefined;
  return (item, delta, instance) =>
    nonAdjustingItemKey !== item.key &&
    // A shrinking first measurement may put the entire row above the fold
    // even though its estimate crossed it. Compensate that newly exposed
    // space, but do not shift a row that still spans the viewport's top.
    (!endAnchored ||
      (instance.scrollOffset !== null &&
        item.end + delta <= instance.scrollOffset + instance.scrollAdjustments));
}
