import { useLayoutEffect, useState, type ReactNode } from "react";
import { useAtomSet } from "@effect/atom-react";

import { type TaskLabelFilter } from "@/api";
import { BoardQueryContext } from "./BoardQueryRuntime";
import { createBoardQueryModel } from "./BoardQueryModel";

export function BoardQueryProvider({
  children,
  labelFilter,
  queriesEnabled = true,
}: Readonly<{
  children: ReactNode;
  labelFilter: TaskLabelFilter;
  queriesEnabled?: boolean;
}>) {
  const [model] = useState(() => createBoardQueryModel({ labelFilter, queriesEnabled }));
  const setInputs = useAtomSet(model.inputs);
  useLayoutEffect(() => {
    setInputs({ labelFilter, queriesEnabled });
  }, [labelFilter, queriesEnabled, setInputs]);
  return <BoardQueryContext.Provider value={model}>{children}</BoardQueryContext.Provider>;
}
