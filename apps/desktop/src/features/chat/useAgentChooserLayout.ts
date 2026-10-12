import { useLayoutEffect, useRef, useState } from "react";

export function useAgentChooserLayout(choosing: boolean) {
  const overviewRef = useRef<HTMLDivElement>(null);
  const scrollRef = useRef<HTMLDivElement>(null);
  const selectedRef = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number | undefined>(undefined);
  useLayoutEffect(() => {
    if (!choosing) return;
    const scroll = scrollRef.current;
    const selected = selectedRef.current;
    if (scroll === null || selected === null) return;
    const bottom = selected.offsetTop + selected.offsetHeight;
    if (bottom > scroll.scrollTop + scroll.clientHeight) {
      scroll.scrollTop = bottom - scroll.clientHeight;
    } else if (selected.offsetTop < scroll.scrollTop) {
      scroll.scrollTop = selected.offsetTop;
    }
  }, [choosing]);
  return {
    overviewRef,
    scrollRef,
    selectedRef,
    overviewOpacity: choosing ? 0 : 1,
    maxHeight: chooserMaxHeight(choosing ? height : undefined),
    capture: () => {
      const overview = overviewRef.current;
      if (overview === null) return;
      setHeight(overview.offsetHeight * 2);
    },
  };
}

function chooserMaxHeight(height: number | undefined): string {
  const available = "calc(var(--radix-popover-content-available-height) - 2 * var(--space-1))";
  return height === undefined ? available : `min(${height.toString()}px, ${available})`;
}
