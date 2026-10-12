import { motion, useReducedMotion, type Transition } from "motion/react";
import { useLayoutEffect, useRef, useState, type ReactNode } from "react";

import { cn } from "./classes";
import { motionDurationFromCSSVar } from "./motion";

export function AnimatedSize({
  children,
  className,
  contentClassName,
  transition,
}: Readonly<{
  children: ReactNode;
  className?: string;
  contentClassName?: string;
  transition?: Transition;
}>) {
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
      className={cn("flex min-h-0 flex-col justify-end overflow-hidden", className)}
      initial={false}
      animate={{ height: height ?? "auto" }}
      transition={{
        ease: "easeOut",
        ...transition,
        duration: reducedMotion
          ? 0
          : (transition?.duration ?? motionDurationFromCSSVar("--motion-reveal", 280) / 1000),
      }}
    >
      <div className={cn("shrink-0", contentClassName)} ref={content}>
        {children}
      </div>
    </motion.div>
  );
}
