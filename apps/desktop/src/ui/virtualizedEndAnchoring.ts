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
  scrollRequest: symbol | null;
}>;

type ListVirtualizer = Virtualizer<HTMLDivElement, Element>;
type ListOptions = VirtualizerOptions<HTMLDivElement, Element>;

function scrollToNativeEnd(instance: ListVirtualizer): void {
  instance.scrollToEnd({ behavior: prefersReducedMotion() ? "auto" : "smooth" });
}

export function useVirtualizedEndAnchoring(mode: VirtualizedEndAnchoring | undefined) {
  const end = useRef<boolean | null>(null);
  const resizeRequest = useRef<symbol | null>(null);
  const lastExplicitRequest = useRef<symbol | null>(null);
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
    const previousSize = instance.itemSizeCache.get(instance.options.getItemKey(index));
    const size = measureElement(element, entry, instance);
    if (entry !== undefined && previousSize !== undefined && previousSize !== size && end.current === true) {
      resizeRequest.current = Symbol("resize at native end");
    }
    return size;
  }, []);
  const afterCommit = useStableCallback((instance: ListVirtualizer) => {
    if (mode === undefined) return;
    const explicit = mode.scrollRequest !== null && mode.scrollRequest !== lastExplicitRequest.current;
    lastExplicitRequest.current = mode.scrollRequest;
    const resize = resizeRequest.current;
    resizeRequest.current = null;
    if (explicit) scrollToNativeEnd(instance);
    else if (resize !== null) instance.scrollToEnd();
  });
  return {
    afterCommit,
    options:
      mode === undefined
        ? {}
        : {
            anchorTo: "end" as const,
            followOnAppend: false as const,
            scrollEndThreshold: mode.threshold,
            observeElementOffset: observeOffset,
            observeElementRect: observeRect,
            onChange: publish,
            // Core 3.17.7 adjusts scroll before React has grown the size container.
            // The browser can clamp that write; batched resizes then lose the local
            // bottom. Restoring useFlushSync was tried and still left content clipped.
            // Finish the resize with native scrollToEnd after commit. Reuse the library's
            // measured-size cache and observer entry so mounting/admitting a page
            // is not an expansion. The preceding native end result lets historical
            // bottom resizing follow without revealing expansions elsewhere.
            // Do not replace this with anchorTo/followOnAppend alone or manual
            // pixel compensation.
            measureElement: measure,
          },
  };
}
