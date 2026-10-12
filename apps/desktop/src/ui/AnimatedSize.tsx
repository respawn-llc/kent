import {
  animate,
  motion,
  useMotionValue,
  useReducedMotion,
  type AnimationPlaybackControls,
  type Transition,
} from "motion/react";
import { useEffectEvent, useLayoutEffect, useRef, type ReactNode } from "react";

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
  const height = useMotionValue<number | string>("auto");
  const animation = useRef<AnimationPlaybackControls | null>(null);
  const reducedMotion = useReducedMotion();
  const resize = useEffectEvent((next: number) => {
    animation.current?.stop();
    animation.current = animate(height, next, {
      ease: "easeOut",
      ...transition,
      duration: reducedMotion
        ? 0
        : (transition?.duration ?? motionDurationFromCSSVar("--motion-reveal", 280) / 1000),
    });
  });
  useLayoutEffect(() => {
    const element = content.current;
    if (element === null) return;
    // Initialize the unscaled layout height without animating from "auto".
    // The popup owns entrance motion; only later content resizes animate here.
    height.set(element.offsetHeight);
    const observer = new ResizeObserver(() => {
      resize(element.offsetHeight);
    });
    observer.observe(element);
    return () => {
      observer.disconnect();
      animation.current?.stop();
    };
  }, [height]);
  return (
    <motion.div
      className={cn("flex min-h-0 flex-col justify-end overflow-hidden", className)}
      style={{ height }}
    >
      <div className={cn("shrink-0", contentClassName)} ref={content}>
        {children}
      </div>
    </motion.div>
  );
}
