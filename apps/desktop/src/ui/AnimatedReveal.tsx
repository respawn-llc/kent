import { motion, useIsPresent, useReducedMotion } from "motion/react";
import type { ReactNode } from "react";

import { motionDurationFromCSSVar } from "./motion";

export function AnimatedReveal({
  children,
  enter = true,
  className,
}: Readonly<{ children: ReactNode; enter?: boolean; className?: string }>) {
  const reducedMotion = useReducedMotion();
  const present = useIsPresent();
  return (
    <motion.div
      className={className}
      inert={present ? undefined : true}
      style={{ overflow: "hidden" }}
      initial={enter && !reducedMotion ? { height: 0, opacity: 0, y: "var(--space-3)" } : false}
      animate={{ height: "auto", opacity: 1, y: 0 }}
      exit={{ height: 0, opacity: 0, y: "var(--space-3)" }}
      transition={{
        duration: reducedMotion ? 0 : motionDurationFromCSSVar("--motion-reveal", 280) / 1000,
        ease: "easeOut",
      }}
    >
      {children}
    </motion.div>
  );
}
