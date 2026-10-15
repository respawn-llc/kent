import {
  defaultRangeExtractor,
  type Range,
  type Virtualizer,
  type VirtualItem,
} from "@tanstack/react-virtual";

export type VirtualizedFlowBlock = Readonly<{
  first: VirtualItem;
  last: VirtualItem;
}>;

export function resolveVirtualizedFlowBlock(
  items: readonly VirtualItem[],
  range: Pick<Range, "startIndex"> | null,
  enabled: boolean,
): VirtualizedFlowBlock | undefined {
  if (!enabled || range === null) return undefined;
  const center = items.findIndex((item) => item.index === range.startIndex);
  let first = items[center];
  if (first === undefined) return undefined;
  let last = first;
  for (let index = center - 1; index >= 0; index--) {
    const previous = items[index];
    if (previous === undefined || previous.index + 1 !== first.index) break;
    first = previous;
  }
  for (let index = center + 1; index < items.length; index++) {
    const next = items[index];
    if (last.index + 1 !== next?.index) break;
    last = next;
  }
  return { first, last };
}

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
