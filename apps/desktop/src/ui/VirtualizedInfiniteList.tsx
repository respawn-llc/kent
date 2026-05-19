import { useEffect, useRef, type ReactNode } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";

import { cx } from "./classes";
import { Spinner } from "./Spinner";

export type VirtualizedInfiniteListProps<TItem> = Readonly<{
  items: readonly TItem[];
  getItemKey: (item: TItem) => string;
  renderItem: (item: TItem) => ReactNode;
  header?: ReactNode | undefined;
  empty?: ReactNode | undefined;
  hasNextPage: boolean;
  isFetchingNextPage: boolean;
  loadingLabel: string;
  onLoadMore: () => void;
  estimateSize: () => number;
  paddingEnd?: number | undefined;
  paddingStart?: number | undefined;
  className?: string | undefined;
}>;

export function VirtualizedInfiniteList<TItem>({
  items,
  getItemKey,
  renderItem,
  header,
  empty,
  hasNextPage,
  isFetchingNextPage,
  loadingLabel,
  onLoadMore,
  estimateSize,
  paddingEnd = 0,
  paddingStart = 0,
  className,
}: VirtualizedInfiniteListProps<TItem>) {
  const scrollRef = useRef<HTMLDivElement | null>(null);
  const lastLoadMoreItemsLengthRef = useRef(-1);
  const headerCount = header === undefined ? 0 : 1;
  const emptyCount = items.length === 0 && empty !== undefined ? 1 : 0;
  const placeholderCount = hasNextPage ? 1 : 0;
  const count = headerCount + Math.max(items.length, emptyCount) + placeholderCount;
  // TanStack Virtual is the intended windowing boundary; returned instance methods are not passed to memoized children.
  // eslint-disable-next-line react-hooks/incompatible-library
  const virtualizer = useVirtualizer({
    count,
    getScrollElement: () => scrollRef.current,
    estimateSize,
    initialRect: { width: 800, height: 600 },
    paddingEnd,
    paddingStart,
    getItemKey: (index) => {
      if (header !== undefined && index === 0) {
        return "header";
      }
      if (items.length === 0 && empty !== undefined && index === headerCount) {
        return "empty";
      }
      const item = items[index - headerCount];
      return item === undefined ? `placeholder-${index.toString()}` : getItemKey(item);
    },
    overscan: 6,
  });
  const virtualItems = virtualizer.getVirtualItems();
  const renderRow = (virtualIndex: number): ReactNode =>
    renderVirtualRow({
      empty,
      emptyCount,
      header,
      headerCount,
      isFetchingNextPage,
      item: items[virtualIndex - headerCount],
      loadingLabel,
      renderItem,
      virtualIndex,
    });

  useEffect(() => {
    const lastItem = virtualItems.at(-1);
    const lastDataIndex = headerCount + items.length - 1;
    if (
      lastItem !== undefined &&
      lastItem.index >= lastDataIndex &&
      hasNextPage &&
      !isFetchingNextPage &&
      lastLoadMoreItemsLengthRef.current !== items.length
    ) {
      lastLoadMoreItemsLengthRef.current = items.length;
      onLoadMore();
    }
  }, [hasNextPage, headerCount, isFetchingNextPage, items.length, onLoadMore, virtualItems]);

  if (count > 0 && virtualItems.length === 0) {
    return (
      <div className={className} ref={scrollRef}>
        {Array.from({ length: count }, (_value, index) => (
          <div
            className="py-[var(--space-2)] first:pt-0 last:pb-0"
            key={fallbackRowKey({ emptyCount, getItemKey, headerCount, index, items })}
            style={fallbackRowStyle({ count, index, paddingEnd, paddingStart })}
          >
            {renderRow(index)}
          </div>
        ))}
      </div>
    );
  }

  return (
    <div className={className} ref={scrollRef}>
      <div className="relative w-full" style={{ height: `${virtualizer.getTotalSize().toString()}px` }}>
        {virtualItems.map((virtualItem) => {
          return (
            <div
              className={cx(
                "absolute top-0 left-0 w-full py-[var(--space-2)]",
                virtualItem.index === 0 && "pt-0",
                virtualItem.index === count - 1 && "pb-0",
              )}
              data-index={virtualItem.index}
              key={virtualItem.key}
              ref={virtualizer.measureElement}
              style={{ transform: `translateY(${virtualItem.start.toString()}px)` }}
            >
              {renderRow(virtualItem.index)}
            </div>
          );
        })}
      </div>
    </div>
  );
}

function fallbackRowStyle({
  count,
  index,
  paddingEnd,
  paddingStart,
}: Readonly<{
  count: number;
  index: number;
  paddingEnd: number;
  paddingStart: number;
}>): React.CSSProperties | undefined {
  if (count === 0) {
    return undefined;
  }
  return {
    paddingBottom: index === count - 1 ? paddingEnd : undefined,
    paddingTop: index === 0 ? paddingStart : undefined,
  };
}

function fallbackRowKey<TItem>({
  emptyCount,
  getItemKey,
  headerCount,
  index,
  items,
}: Readonly<{
  emptyCount: number;
  getItemKey: (item: TItem) => string;
  headerCount: number;
  index: number;
  items: readonly TItem[];
}>): string {
  if (headerCount > 0 && index === 0) {
    return "header";
  }
  if (emptyCount > 0 && index === headerCount) {
    return "empty";
  }
  const item = items[index - headerCount];
  return item === undefined ? `placeholder-${index.toString()}` : getItemKey(item);
}

function renderVirtualRow<TItem>({
  empty,
  emptyCount,
  header,
  headerCount,
  isFetchingNextPage,
  item,
  loadingLabel,
  renderItem,
  virtualIndex,
}: Readonly<{
  empty: ReactNode | undefined;
  emptyCount: number;
  header: ReactNode | undefined;
  headerCount: number;
  isFetchingNextPage: boolean;
  item: TItem | undefined;
  loadingLabel: string;
  renderItem: (item: TItem) => ReactNode;
  virtualIndex: number;
}>): ReactNode {
  if (header !== undefined && virtualIndex === 0) {
    return header;
  }
  if (emptyCount > 0 && virtualIndex === headerCount) {
    return empty;
  }
  if (item === undefined) {
    return <VirtualizedPlaceholder loading={isFetchingNextPage} loadingLabel={loadingLabel} />;
  }
  return renderItem(item);
}

function VirtualizedPlaceholder({
  loading,
  loadingLabel,
}: Readonly<{ loading: boolean; loadingLabel: string }>) {
  return (
    <div
      aria-label={loading ? loadingLabel : undefined}
      aria-live="polite"
      className="grid min-h-12 place-items-center"
      role={loading ? "status" : undefined}
    >
      {loading ? (
        <>
          <Spinner size="sm" />
          <span className="sr-only">{loadingLabel}</span>
        </>
      ) : null}
    </div>
  );
}
