import { motion, usePresence } from "motion/react";
import { useEffect, type CSSProperties, type ReactNode } from "react";

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

function useRevealPresence(reducedMotion: boolean) {
  const [present, safeToRemove] = usePresence(reducedMotion);
  useEffect(() => {
    if (reducedMotion && !present) safeToRemove();
  }, [present, reducedMotion, safeToRemove]);
  return { present, shouldRender: present || !reducedMotion };
}

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
  const { present, shouldRender } = useRevealPresence(reducedMotion);
  if (!shouldRender) return null;
  return (
    <motion.div
      className={className}
      inert={!present}
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
