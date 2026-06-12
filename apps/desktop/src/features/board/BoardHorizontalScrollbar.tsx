import { useCallback, useEffect, useState, type ChangeEvent, type RefObject } from "react";
import { useTranslation } from "react-i18next";

type HorizontalScrollMetrics = Readonly<{
  max: number;
  value: number;
}>;

export function BoardHorizontalScrollbar({
  scrollportRef,
}: Readonly<{ scrollportRef: RefObject<HTMLDivElement | null> }>) {
  const { t } = useTranslation();
  const [metrics, setMetrics] = useState<HorizontalScrollMetrics>({ max: 0, value: 0 });

  const syncMetrics = useCallback(() => {
    const scrollport = scrollportRef.current;
    if (scrollport === null) {
      return;
    }
    const max = Math.max(0, scrollport.scrollWidth - scrollport.clientWidth);
    const value = Math.min(scrollport.scrollLeft, max);
    setMetrics((current) => (current.max === max && current.value === value ? current : { max, value }));
  }, [scrollportRef]);

  useEffect(() => {
    const scrollport = scrollportRef.current;
    if (scrollport === null) {
      return undefined;
    }

    let frame = 0;
    const scheduleSync = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(syncMetrics);
    };
    const observer = typeof ResizeObserver === "undefined" ? null : new ResizeObserver(scheduleSync);
    if (observer !== null) {
      observer.observe(scrollport);
      if (scrollport.firstElementChild instanceof Element) {
        observer.observe(scrollport.firstElementChild);
      }
    }
    scrollport.addEventListener("scroll", scheduleSync, { passive: true });
    window.addEventListener("resize", scheduleSync);
    syncMetrics();

    return () => {
      cancelAnimationFrame(frame);
      observer?.disconnect();
      scrollport.removeEventListener("scroll", scheduleSync);
      window.removeEventListener("resize", scheduleSync);
    };
  }, [scrollportRef, syncMetrics]);

  if (metrics.max <= 0) {
    return null;
  }

  function scrollToValue(event: ChangeEvent<HTMLInputElement>): void {
    const scrollport = scrollportRef.current;
    if (scrollport === null) {
      return;
    }
    const value = Number(event.target.value);
    scrollport.scrollLeft = value;
    setMetrics((current) => ({ ...current, value }));
  }

  return (
    <input
      aria-label={t("board.horizontalScroll")}
      className="board-horizontal-scrollbar app-region-no-drag absolute inset-x-[var(--space-4)] bottom-[var(--space-2)] z-20"
      max={metrics.max}
      min={0}
      onChange={scrollToValue}
      type="range"
      value={metrics.value}
    />
  );
}
