import * as Atom from "effect/unstable/reactivity/Atom";

export const state = Atom.make(0);
const listeners = new Set<(value: number) => void>();
export function subscribe(listener: (value: number) => void): () => void {
  listeners.add(listener);
  return () => {
    listeners.delete(listener);
  };
}
export interface Observation {
  subscribe(listener: (value: number) => void): Promise<() => void>;
}
