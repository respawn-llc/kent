import { useLayoutEffect, useRef, useState } from "react";

export function useTextOverflow(text: string) {
  const viewportRef = useRef<HTMLSpanElement>(null);
  const contentRef = useRef<HTMLElement>(null);
  const [overflowing, setOverflowing] = useState(false);
  useLayoutEffect(() => {
    const viewport = viewportRef.current;
    const content = contentRef.current;
    if (viewport === null || content === null) return;
    const measure = () => {
      setOverflowing(viewport.scrollWidth > viewport.clientWidth);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(viewport);
    observer.observe(content);
    return () => {
      observer.disconnect();
    };
  }, [text]);
  return { viewportRef, contentRef, overflowing };
}
