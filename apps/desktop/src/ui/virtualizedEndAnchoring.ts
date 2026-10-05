import { useCallback, useRef } from "react";
import {
  measureElement,
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
  scrollRequest: Readonly<{ behavior: "auto" | "smooth" }> | null;
}>;

type ListVirtualizer = Virtualizer<HTMLDivElement, Element>;
type ListOptions = VirtualizerOptions<HTMLDivElement, Element>;

function scrollToNativeEnd(instance: ListVirtualizer): void {
  instance.scrollToEnd({ behavior: prefersReducedMotion() ? "auto" : "smooth" });
}

export function useVirtualizedEndAnchoring(mode: VirtualizedEndAnchoring | undefined) {
  const end = useRef<boolean | null>(null);
  const resizeRequest = useRef<symbol | null>(null);
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
  const measure = useCallback<NonNullable<ListOptions["measureElement"]>>((element, entry, instance) => {
    const index = instance.indexFromElement(element);
    const previousSize = instance.measurementsCache[index]?.size;
    const size = measureElement(element, entry, instance);
    if (entry !== undefined && previousSize !== size && end.current === true) {
      resizeRequest.current = Symbol("resize at native end");
    }
    return size;
  }, []);
  const applyScrollRequest = useStableCallback((instance: ListVirtualizer) => {
    if (mode?.scrollRequest == null) return;
    resizeRequest.current = null;
    instance.scrollToEnd({
      behavior: prefersReducedMotion() ? "auto" : mode.scrollRequest.behavior,
    });
  });
  const onChange = useStableCallback((instance: ListVirtualizer) => {
    if (mode === undefined) return;
    const resize = resizeRequest.current;
    resizeRequest.current = null;
    if (resize !== null) instance.scrollToEnd();
    publish(instance);
  });
  return {
    applyScrollRequest,
    options:
      mode === undefined
        ? {}
        : {
            anchorTo: "end" as const,
            followOnAppend: false as const,
            scrollEndThreshold: mode.threshold,
            observeElementOffset: observeOffset,
            observeElementRect: observeRect,
            // Direct positioning updates the size container before this callback.
            // Finish any end resize here, in the same observer delivery, because
            // the earlier scroll write can be clamped against the old DOM extent.
            onChange,
            measureElement: measure,
          },
  };
}
