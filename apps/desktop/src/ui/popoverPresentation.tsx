import type { ReactNode } from "react";
import { AnimatedSize } from "./AnimatedSize";
import { motionDurationFromCSSVar } from "./motion";

export const popoverLayoutClassName =
  "grid w-64 gap-[var(--space-3)] rounded-[var(--radius-l)] p-[var(--space-3)] text-[var(--color-on-island)]";

export function PopoverBody({ children }: Readonly<{ children: ReactNode }>) {
  return (
    <AnimatedSize
      className="justify-start gap-[inherit]"
      contentClassName="grid min-h-0 gap-[inherit]"
      transition={{
        duration: motionDurationFromCSSVar("--motion-fast", 140) / 1000,
        ease: "easeInOut",
      }}
    >
      {children}
    </AnimatedSize>
  );
}
