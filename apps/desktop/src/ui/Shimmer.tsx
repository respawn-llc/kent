// Adapted from Vercel AI Elements Shimmer (Apache-2.0).
// https://github.com/vercel/ai-elements
import { motion } from "motion/react";

import { motionDurationFromCSSVar, shimmerMotion, useReducedMotion } from "./motion";

export function Shimmer({ children, className }: Readonly<{ children?: string; className?: string }>) {
  const reducedMotion = useReducedMotion();
  return (
    <motion.span
      className={children === undefined ? className : "inline-block bg-clip-text text-transparent"}
      initial={{ backgroundPosition: "100% center" }}
      animate={{ backgroundPosition: reducedMotion ? "100% center" : "0% center" }}
      transition={{
        duration: reducedMotion
          ? 0
          : motionDurationFromCSSVar(
              shimmerMotion.durationVarName,
              shimmerMotion.fallbackDurationMs,
              reducedMotion,
            ) / 1000,
        ease: shimmerMotion.ease,
        repeat: reducedMotion ? 0 : Infinity,
      }}
      style={{
        backgroundSize: "250% 100%, auto",
        backgroundRepeat: "no-repeat",
        backgroundImage: reducedMotion
          ? "linear-gradient(var(--color-muted), var(--color-muted))"
          : children === undefined
            ? "linear-gradient(90deg, transparent, color-mix(in srgb, var(--color-on-background) 8%, transparent), transparent), linear-gradient(color-mix(in srgb, var(--color-muted) 12%, transparent), color-mix(in srgb, var(--color-muted) 12%, transparent))"
            : `linear-gradient(90deg, transparent calc(50% - ${String(children.length * 2)}px), var(--color-on-background), transparent calc(50% + ${String(children.length * 2)}px)), linear-gradient(var(--color-muted), var(--color-muted))`,
      }}
    >
      {children}
    </motion.span>
  );
}
