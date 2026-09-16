import { useCallback, useRef } from "react";
import {
  observeElementOffset,
  observeElementRect,
  type Virtualizer,
  type VirtualizerOptions,
} from "@tanstack/react-virtual";

import { prefersReducedMotion } from "./motion";
import { useStableCallback } from "./useStableCallback";

export type VirtualizedEndAnchoring = Readonly<{
  followEnabled: boolean;
  threshold: number;
  onEndChange(atEnd: boolean): void;
  scrollRequest: symbol | null;
}>;

type ListVirtualizer = Virtualizer<HTMLDivElement, Element>;
type ListOptions = VirtualizerOptions<HTMLDivElement, Element>;

export function scrollToNativeEnd(instance: ListVirtualizer): void {
  instance.scrollToEnd({ behavior: prefersReducedMotion() ? "auto" : "smooth" });
}

export function useVirtualizedEndAnchoring(mode: VirtualizedEndAnchoring | undefined) {
  const end = useRef<boolean | null>(null);
  const publish = useStableCallback((instance: ListVirtualizer) => {
    if (mode === undefined) return;
    end.current = instance.isAtEnd();
    mode.onEndChange(end.current);
  });
  const canFollow = useStableCallback(() => mode?.followEnabled === true && end.current === true);
  const observeOffset = useCallback<ListOptions["observeElementOffset"]>(
    (instance, callback) =>
      observeElementOffset(instance, (offset, scrolling) => {
        callback(offset, scrolling);
        publish(instance);
      }),
    [publish],
  );
  const observeRect = useCallback<ListOptions["observeElementRect"]>(
    (instance, callback) =>
      observeElementRect(instance, (rect) => {
        const following = canFollow();
        callback(rect);
        if (following) scrollToNativeEnd(instance);
        publish(instance);
      }),
    [canFollow, publish],
  );
  return mode === undefined
    ? {}
    : {
        anchorTo: "end" as const,
        followOnAppend: false as const,
        scrollEndThreshold: mode.threshold,
        observeElementOffset: observeOffset,
        observeElementRect: observeRect,
        onChange: publish,
      };
}
