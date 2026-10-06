import { defaultRangeExtractor, type Range, type Virtualizer } from "@tanstack/react-virtual";

export function virtualizedVisibleIndexes(
  virtualizer: Virtualizer<HTMLDivElement, Element>,
): readonly number[] {
  const range = virtualizer.range;
  return range === null
    ? []
    : defaultRangeExtractor({
        ...range,
        count: virtualizer.options.count,
        overscan: virtualizer.options.overscan,
      });
}

export function pinnedVirtualRangeExtractor(range: Range, pinnedIndexes: ReadonlySet<number>): number[] {
  const indexes = new Set(defaultRangeExtractor(range));
  for (const index of pinnedIndexes) {
    if (index >= 0 && index < range.count) {
      indexes.add(index);
    }
  }
  return [...indexes].sort((left, right) => left - right);
}
