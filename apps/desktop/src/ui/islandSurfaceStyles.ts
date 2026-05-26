import { cx } from "./classes";

export type IslandLevel = 0 | 1 | 2 | 3 | 4;

const islandSurfaceLevelClassNames: Record<IslandLevel, string> = {
  0: "island-surface-0",
  1: "island-surface-1",
  2: "island-surface-2",
  3: "island-surface-3",
  4: "island-surface-4",
};

export function islandSurfaceClassName(level: IslandLevel = 0): string {
  return cx("island-surface", islandSurfaceLevelClassNames[level]);
}
