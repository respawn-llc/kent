import { motion, useReducedMotion } from "motion/react";
import { useLayoutEffect, useRef, useState, type ReactNode } from "react";

import { motionDurationFromCSSVar } from "./motion";

export function AnimatedSize({ children }: Readonly<{ children: ReactNode }>) {
  const content = useRef<HTMLDivElement>(null);
  const [height, setHeight] = useState<number | null>(null);
  const reducedMotion = useReducedMotion();
  useLayoutEffect(() => {
    const element = content.current;
    if (element === null) return;
    const measure = () => {
      setHeight(element.getBoundingClientRect().height);
    };
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(element);
    return () => {
      observer.disconnect();
    };
  }, []);
  return (
    <motion.div
      className="flex min-h-0 flex-col justify-end overflow-hidden"
      initial={false}
      animate={{ height: height ?? "auto" }}
      transition={{
        duration: reducedMotion ? 0 : motionDurationFromCSSVar("--motion-reveal", 280) / 1000,
        ease: "easeOut",
      }}
    >
      <div className="shrink-0" ref={content}>
        {children}
      </div>
    </motion.div>
  );
}
