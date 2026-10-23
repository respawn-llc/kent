import { motion, useIsPresent } from "motion/react";
import type { CSSProperties, ReactNode } from "react";

import { motionDurationFromCSSVar, useReducedMotion } from "./motion";

const revealStyles = {
  expand: { overflow: "hidden" },
  fade: {},
  "collapse-center": {
    overflow: "hidden",
    display: "flex",
    flexDirection: "column",
    justifyContent: "center",
  },
} satisfies Record<string, CSSProperties>;

export function AnimatedReveal({
  children,
  enter = true,
  className,
  variant = "expand",
}: Readonly<{
  children: ReactNode;
  enter?: boolean;
  className?: string;
  variant?: "expand" | "fade" | "collapse-center";
}>) {
  const reducedMotion = useReducedMotion();
  const present = useIsPresent();
  return (
    <motion.div
      className={className}
      inert={present ? undefined : true}
      style={revealStyles[variant]}
      initial={
        enter && !reducedMotion
          ? variant === "fade"
            ? { opacity: 0 }
            : { height: 0, opacity: 0, y: "var(--space-3)" }
          : false
      }
      animate={variant === "fade" ? { opacity: 1 } : { height: "auto", opacity: 1, y: 0 }}
      exit={
        variant === "fade"
          ? { opacity: 0 }
          : variant === "collapse-center"
            ? { height: 0, opacity: 0, y: 0 }
            : { height: 0, opacity: 0, y: "var(--space-3)" }
      }
      transition={{
        duration: reducedMotion ? 0 : motionDurationFromCSSVar("--motion-reveal", 280) / 1000,
        ease: "easeOut",
      }}
    >
      {variant === "collapse-center" ? <div className="shrink-0">{children}</div> : children}
    </motion.div>
  );
}
