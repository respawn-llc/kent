import { useEffect } from "react";

import type { KanbanCardVM } from "./BoardColumnViewModel";

export function useObservedInterruptedRuns(
  cards: readonly KanbanCardVM[],
  onObserved: (input: Readonly<{ runID: string; taskID: string }>) => void,
): void {
  useEffect(() => {
    for (const card of cards) {
      if (card.statusKind !== "interrupted") {
        continue;
      }
      for (const runID of card.statusRunIDs) {
        onObserved({ runID, taskID: card.id });
      }
    }
  }, [cards, onObserved]);
}
