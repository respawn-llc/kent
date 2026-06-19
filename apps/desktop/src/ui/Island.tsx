import type { HTMLAttributes, ReactNode } from "react";

import { cx } from "./classes";
import { islandSurfaceClassName, type IslandLevel } from "./islandSurfaceStyles";

// Island corner radius. `xl` is the standalone/container radius; nested cards
// that sit inside another island use `l` so their corners read one level below
// the surface that contains them.
export type IslandRadius = "l" | "xl";

const islandRadiusClassNames: Record<IslandRadius, string> = {
  l: "rounded-[var(--radius-l)]",
  xl: "rounded-[var(--radius-xl)]",
};

export type IslandProps = Readonly<{
  children: ReactNode;
  floatingWidth?: "default" | "full";
  level?: IslandLevel;
  radius?: IslandRadius;
  tone?: "primary" | "secondary" | "floating";
  unpadded?: boolean;
}> &
  HTMLAttributes<HTMLElement>;

export function Island({
  children,
  className,
  floatingWidth = "default",
  level,
  radius = "xl",
  tone = "primary",
  unpadded = false,
  ...props
}: IslandProps) {
  const surfaceLevel = level ?? (tone === "secondary" ? 1 : 0);
  return (
    <section
      className={cx(
        "app-region-no-drag",
        islandRadiusClassNames[radius],
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
