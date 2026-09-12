import { ChevronDown } from "lucide-react";
import { useEffect, useRef, useState, type ReactNode } from "react";

import { cx } from "./classes";
import { useOpacityExit } from "./motion";

export type MarkdownHeightClamp = Readonly<{
  maximumLines: number;
  minimumLines: number;
  viewportPercent: number;
}>;

export function CollapsibleMarkdownViewport({
  children,
  collapsedHeightClamp,
  expanded,
  expandLabel,
  onExpand,
}: Readonly<{
  children: ReactNode;
  collapsedHeightClamp?: MarkdownHeightClamp;
  expanded: boolean;
  expandLabel?: string;
  onExpand?: () => void;
}>) {
  const viewportRef = useRef<HTMLDivElement | null>(null);
  const contentRef = useRef<HTMLDivElement | null>(null);
  const collapsed = collapsedHeightClamp !== undefined && !expanded;
  const [overflows, setOverflows] = useState(false);
  useEffect(() => {
    if (!collapsed) return;
    const measureOverflow = () => {
      const viewport = viewportRef.current;
      if (viewport !== null) setOverflows(viewport.scrollHeight > viewport.clientHeight);
    };
    const frame = window.requestAnimationFrame(measureOverflow);
    window.addEventListener("resize", measureOverflow);
    const observer = typeof ResizeObserver === "undefined" ? undefined : new ResizeObserver(measureOverflow);
    if (viewportRef.current !== null) observer?.observe(viewportRef.current);
    if (contentRef.current !== null) observer?.observe(contentRef.current);
    return () => {
      observer?.disconnect();
      window.cancelAnimationFrame(frame);
      window.removeEventListener("resize", measureOverflow);
    };
  }, [collapsed]);
  const phase = useOpacityExit(collapsed && overflows);
  const showAffordance = phase !== "hidden" && onExpand !== undefined && expandLabel !== undefined;
  return (
    <div
      className={cx("relative min-w-0 max-w-full", collapsed && "overflow-hidden")}
      data-slot="markdown-field-read-content-viewport"
      data-testid="markdown-field-read-content-viewport"
      ref={viewportRef}
      style={
        collapsed
          ? {
              maxHeight: `clamp(${collapsedHeightClamp.minimumLines.toString()}lh,${collapsedHeightClamp.viewportPercent.toString()}dvh,${collapsedHeightClamp.maximumLines.toString()}lh)`,
            }
          : undefined
      }
    >
      <div className="min-w-0 max-w-full" ref={contentRef}>
        {children}
      </div>
      {showAffordance && (
        <>
          <div
            aria-hidden="true"
            className={cx(
              "pointer-events-none absolute inset-x-0 bottom-0 h-12 bg-gradient-to-b from-transparent to-[var(--color-island-1)] transition-opacity motion-reduce:transition-none",
              phase === "visible" ? "opacity-100" : "opacity-0",
            )}
            data-state={phase}
          />
          <button
            aria-label={expandLabel}
            aria-hidden={phase === "visible" ? undefined : true}
            className={cx(
              "app-region-no-drag absolute inset-x-0 bottom-0 grid h-10 place-items-center text-[var(--color-on-island)] transition-opacity motion-reduce:transition-none",
              phase === "visible" ? "pointer-events-auto opacity-100" : "pointer-events-none opacity-0",
            )}
            data-state={phase}
            onClick={onExpand}
            tabIndex={phase === "visible" ? undefined : -1}
            type="button"
          >
            <ChevronDown aria-hidden="true" size={20} strokeWidth={1.5} />
          </button>
        </>
      )}
    </div>
  );
}
