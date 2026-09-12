import * as Atom from "effect/unstable/reactivity/Atom";

export const state = Atom.make(0);
export declare const subscribe:
  ((listener: (value: number) => void) => () => void) | ((listener: (value: string) => void) => () => void);
