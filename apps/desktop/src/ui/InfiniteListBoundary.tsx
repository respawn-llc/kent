import { useCallback, useState } from "react";

import { Button } from "./Button";
import { Spinner } from "./Spinner";

function useInlineListBoundary(state: VirtualizedInfiniteListBoundaryState | undefined) {
  const [height, setHeight] = useState(0);
  const ref = useCallback((element: HTMLDivElement | null) => {
    if (element === null) return;
    const measure = () => {
      setHeight(element.offsetHeight);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, []);
  return { ref, height: state === undefined ? 0 : height };
}

export function useVirtualizedListBoundaries({
  anchored,
  horizontal,
  hasNextPage,
  previousBoundary,
  nextBoundary,
  paddingStart,
  paddingEnd,
}: Readonly<{
  anchored: boolean;
  horizontal: boolean;
  hasNextPage: boolean;
  previousBoundary: VirtualizedInfiniteListBoundaryState | undefined;
  nextBoundary: VirtualizedInfiniteListBoundaryState | undefined;
  paddingStart: number;
  paddingEnd: number;
}>) {
  // Transient pagination controls reserve space without becoming reading anchors.
  const inline = anchored && !horizontal;
  const { ref: leadingRef, height: leadingHeight } = useInlineListBoundary(
    inline ? previousBoundary : undefined,
  );
  const { ref: trailingRef, height: trailingHeight } = useInlineListBoundary(
    inline ? nextBoundary : undefined,
  );
  return {
    layout: {
      hasNextPage: inline ? false : hasNextPage,
      previousBoundary: inline ? undefined : previousBoundary,
      nextBoundary: inline ? undefined : nextBoundary,
    },
    paddingStart: paddingStart + leadingHeight,
    paddingEnd: paddingEnd + trailingHeight,
    leading:
      inline && previousBoundary !== undefined ? (
        <div className="absolute w-full" ref={leadingRef} style={{ top: paddingStart }}>
          <InfiniteListBoundary direction="previous" state={previousBoundary} />
        </div>
      ) : null,
    trailing:
      inline && nextBoundary !== undefined ? (
        <div className="absolute w-full" ref={trailingRef} style={{ bottom: paddingEnd }}>
          <InfiniteListBoundary direction="next" state={nextBoundary} />
        </div>
      ) : null,
  };
}

export type VirtualizedInfiniteListBoundaryState =
  | Readonly<{ state: "loading"; label: string }>
  | Readonly<{ state: "error"; message: string; retryLabel: string; onRetry: () => void }>;

export function directionalBoundary(
  input: Readonly<{
    message: string;
    failed: boolean;
    loading: boolean;
    loadingLabel: string;
    onRetry: () => void;
    retryLabel: string;
  }>,
): VirtualizedInfiniteListBoundaryState | undefined {
  if (input.loading) {
    return { state: "loading", label: input.loadingLabel };
  }
  if (input.failed) {
    return {
      state: "error",
      message: input.message,
      retryLabel: input.retryLabel,
      onRetry: input.onRetry,
    };
  }
  return undefined;
}

export function autoLoadAvailable(
  available: boolean,
  boundary: VirtualizedInfiniteListBoundaryState | undefined,
): boolean {
  return available && boundary?.state !== "error";
}

export function InfiniteListBoundary({
  direction,
  state,
}: Readonly<{
  direction: "initial" | "previous" | "next" | "replacement";
  state: VirtualizedInfiniteListBoundaryState;
}>) {
  return (
    <div
      className="grid min-h-12 place-items-center"
      data-testid={`virtual-boundary-${direction}`}
      data-virtual-boundary={direction}
    >
      {state.state === "loading" ? (
        <div aria-label={state.label} aria-live="polite" className="grid place-items-center" role="status">
          <Spinner size="sm" />
          <span className="sr-only">{state.label}</span>
        </div>
      ) : null}
      {state.state === "error" ? (
        <div className="flex flex-wrap items-center justify-center gap-[var(--space-2)]" role="alert">
          <span className="text-sm text-[var(--color-muted)]">{state.message}</span>
          <Button onClick={state.onRetry}>{state.retryLabel}</Button>
        </div>
      ) : null}
    </div>
  );
}
