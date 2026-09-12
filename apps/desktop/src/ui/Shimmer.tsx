// Adapted from Vercel AI Elements Shimmer (Apache-2.0).
// https://github.com/vercel/ai-elements
import { motion, useReducedMotion } from "motion/react";

import { motionDurationFromCSSVar, shimmerMotion } from "./motion";

export function Shimmer({ children }: Readonly<{ children: string }>) {
  const reducedMotion = useReducedMotion();
  return (
    <motion.span
      className="inline-block bg-clip-text text-transparent"
      initial={{ backgroundPosition: "100% center" }}
      animate={{ backgroundPosition: reducedMotion ? "100% center" : "0% center" }}
      transition={{
        duration:
          motionDurationFromCSSVar(shimmerMotion.durationVarName, shimmerMotion.fallbackDurationMs) / 1000,
        ease: shimmerMotion.ease,
        repeat: reducedMotion ? 0 : Infinity,
      }}
      style={{
        backgroundSize: "250% 100%, auto",
        backgroundRepeat: "no-repeat",
        backgroundImage: reducedMotion
          ? "linear-gradient(var(--color-muted), var(--color-muted))"
          : `linear-gradient(90deg, transparent calc(50% - ${String(children.length * 2)}px), var(--color-on-background), transparent calc(50% + ${String(children.length * 2)}px)), linear-gradient(var(--color-muted), var(--color-muted))`,
      }}
    >
      {children}
    </motion.span>
  );
}
