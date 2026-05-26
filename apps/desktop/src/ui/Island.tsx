import type { HTMLAttributes, ReactNode } from "react";

import { cx } from "./classes";
import { islandSurfaceClassName, type IslandLevel } from "./islandSurfaceStyles";

export type IslandProps = Readonly<{
  children: ReactNode;
  floatingWidth?: "default" | "full";
  level?: IslandLevel;
  tone?: "primary" | "secondary" | "floating";
  unpadded?: boolean;
}> &
  HTMLAttributes<HTMLElement>;

export function Island({
  children,
  className,
  floatingWidth = "default",
  level,
  tone = "primary",
  unpadded = false,
  ...props
}: IslandProps) {
  const surfaceLevel = level ?? (tone === "secondary" ? 1 : 0);
  return (
    <section
      className={cx(
        "app-region-no-drag rounded-[var(--radius-xl)]",
        islandSurfaceClassName(surfaceLevel),
        !unpadded && "p-[var(--space-4)]",
        tone === "secondary" && "shadow-none",
        tone === "floating" && floatingWidth === "default" && "m-auto max-w-[760px]",
        className,
      )}
      {...props}
    >
      {children}
    </section>
  );
}
