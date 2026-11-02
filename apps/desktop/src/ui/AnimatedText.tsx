import { AnimatePresence, motion } from "motion/react";
import { cn } from "./classes";
import { motionDurationFromCSSVar, useReducedMotion } from "./motion";

export function AnimatedText({ text, className }: Readonly<{ text: string; className?: string }>) {
  const reducedMotion = useReducedMotion();
  if (reducedMotion) return <span className={className}>{text}</span>;
  return (
    <span className="grid min-w-0">
      <AnimatePresence initial={false}>
        <motion.span
          key={text}
          className={cn("min-w-0 [grid-area:1/1]", className)}
          initial={{ opacity: 0, filter: "blur(var(--space-1))" }}
          animate={{ opacity: 1, filter: "blur(0px)" }}
          exit={{ opacity: 0, filter: "blur(var(--space-1))" }}
          transition={{ duration: motionDurationFromCSSVar("--motion-fast", 140) / 1000, ease: "easeOut" }}
        >
          {text}
        </motion.span>
      </AnimatePresence>
    </span>
  );
}
