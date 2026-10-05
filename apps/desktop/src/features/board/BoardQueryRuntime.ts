import { createContext, useContext } from "react";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import type { createBoardQueryModel } from "./BoardQueryModel";

import type { BoardFilter, BoardNodeCardsSort } from "@/api";

export type BoardQueryState = Readonly<{
  filter: BoardFilter;
  queriesEnabled: boolean;
  setDependencyFilter: (filter: boolean | null) => void;
  setSort: (sort: BoardNodeCardsSort) => void;
  sort: BoardNodeCardsSort;
}>;

export const BoardQueryContext = createContext<ReturnType<typeof createBoardQueryModel> | null>(null);

export function useBoardQueryModel() {
  const value = useContext(BoardQueryContext);
  if (value === null) {
    throw new Error("BoardQueryProvider is required");
  }
  return value;
}

export function useBoardQuery(): BoardQueryState {
  const model = useBoardQueryModel();
  return {
    ...useAtomValue(model.state),
    setDependencyFilter: useAtomSet(model.setDependencyFilter, { mode: "value" }),
    setSort: useAtomSet(model.setSort, { mode: "value" }),
  };
}
