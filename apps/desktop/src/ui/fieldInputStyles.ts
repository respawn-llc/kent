import { cx } from "./classes";
import { islandSurfaceClassName, type IslandLevel } from "./islandSurfaceStyles";

export const fieldInputClassName =
  "app-region-no-drag w-full rounded-[var(--radius-m)] border border-[var(--color-outline)] bg-[var(--color-island-1)] px-[14px] py-3 text-[var(--color-on-island)] outline-none transition-[height,border-color,box-shadow,background-color] focus-visible:border-[var(--color-primary)] disabled:cursor-not-allowed disabled:opacity-55";

export function fieldIslandInputClassName(level: IslandLevel = 0): string {
  // Inputs keep the standard control radius (--radius-m); only the surface elevation changes with
  // the level. Container islands (cards/panes) use larger radii, but controls should not.
  return cx(fieldInputClassName, islandSurfaceClassName(level));
}
