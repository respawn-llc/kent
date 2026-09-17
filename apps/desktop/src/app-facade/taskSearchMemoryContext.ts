import { createContext, useContext } from "react";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";

export type TaskSearchMemorySelection = Readonly<{
  key: string;
  projectID: string | null;
  query: string;
}>;

export type TaskSearchMemory = Readonly<{
  query: string;
  rememberSelection(selection: TaskSearchMemorySelection): void;
  selectionFor(projectID: string | null): TaskSearchMemorySelection | null;
  setQuery(query: string): void;
}>;

export function createTaskSearchMemory() {
  const query = Atom.make("");
  const selections = Atom.make<ReadonlyMap<string | null, TaskSearchMemorySelection>>(new Map());
  const state = Atom.make((get) => ({ query: get(query), selections: get(selections) }));
  const setQuery = Atom.fn<string>()((value, get) =>
    Effect.sync(() => {
      get.set(query, value);
    }),
  );
  const rememberSelection = Atom.fn<TaskSearchMemorySelection>()((value, get) =>
    Effect.sync(() => {
      const current = get(selections);
      const previous = current.get(value.projectID);
      if (previous?.key === value.key && previous.query === value.query) return;
      get.set(selections, new Map(current).set(value.projectID, value));
    }),
  );
  return { state, setQuery, rememberSelection } as const;
}

export const TaskSearchMemoryContext = createContext<ReturnType<typeof createTaskSearchMemory> | null>(null);

export function useTaskSearchMemory(): TaskSearchMemory {
  const memory = useContext(TaskSearchMemoryContext);
  if (memory === null) {
    throw new Error("Task Search memory requires TaskSearchMemoryProvider.");
  }
  const state = useAtomValue(memory.state);
  const setQuery = useAtomSet(memory.setQuery, { mode: "value" });
  const rememberSelection = useAtomSet(memory.rememberSelection, { mode: "value" });
  return {
    query: state.query,
    setQuery,
    rememberSelection,
    selectionFor: (projectID) => state.selections.get(projectID) ?? null,
  };
}
