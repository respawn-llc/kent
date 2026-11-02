import {
  animate,
  motion,
  useMotionValue,
  type AnimationPlaybackControls,
  type Transition,
} from "motion/react";
import { useEffect, useEffectEvent, useLayoutEffect, useRef, type ReactNode, type RefObject } from "react";

import { cn } from "./classes";
import { motionDurationFromCSSVar, useReducedMotion } from "./motion";

export function AnimatedSize({
  children,
  className,
  contentClassName,
  reflowRef,
  transition,
}: Readonly<{
  children: ReactNode;
  className?: string;
  contentClassName?: string;
  reflowRef?: RefObject<HTMLElement | null>;
  transition?: Transition;
}>) {
  const content = useRef<HTMLDivElement>(null);
  const height = useMotionValue<number | string>("auto");
  const animation = useRef<AnimationPlaybackControls | null>(null);
  const reducedMotion = useReducedMotion();
  useEffect(() => {
    if (!reducedMotion) return;
    animation.current?.stop();
    if (content.current !== null) height.set(content.current.offsetHeight);
  }, [reducedMotion, height]);
  const resize = useEffectEvent((next: number, widthChanged: boolean) => {
    animation.current?.stop();
    if (widthChanged) {
      height.set(next);
      return;
    }
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
  }, [height]);
  useEffect(() => {
    const element = content.current;
    if (element === null) return;
    // Ancestor refs attach after descendant layout effects.
    const reflowElement = reflowRef?.current ?? element;
    let measuredHeight = element.offsetHeight;
    let measuredWidth = reflowElement.offsetWidth;
    const observer = new ResizeObserver(() => {
      const nextHeight = element.offsetHeight;
      const nextWidth = reflowElement.offsetWidth;
      if (nextHeight === measuredHeight && nextWidth === measuredWidth) return;
      // Fit-content bubbles grow horizontally as text arrives. Only changes to
      // their available layout width should bypass the content-height animation.
      resize(nextHeight, nextWidth !== measuredWidth);
      measuredHeight = nextHeight;
      measuredWidth = nextWidth;
    });
    observer.observe(element);
    if (reflowElement !== element) observer.observe(reflowElement);
    return () => {
      observer.disconnect();
      animation.current?.stop();
    };
  }, [height, reflowRef]);
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
