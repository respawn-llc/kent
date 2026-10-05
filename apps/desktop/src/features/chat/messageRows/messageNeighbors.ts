import type { TranscriptRenderItem } from "@/app-facade";

export type MessageNeighbors = Readonly<{ previous: boolean; next: boolean }>;

/** Only immediate neighbors in the loaded, bounded window participate. */
export function messageNeighbors(
  items: readonly TranscriptRenderItem[],
): ReadonlyMap<string, MessageNeighbors> {
  const neighbors = new Map<string, MessageNeighbors>();
  items.forEach((item, index) => {
    if (item.kind !== "user" && item.kind !== "assistant") return;
    neighbors.set(item.key, {
      previous: items[index - 1]?.kind === item.kind,
      next: items[index + 1]?.kind === item.kind,
    });
  });
  return neighbors;
}
