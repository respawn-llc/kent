import * as Reactivity from "effect/unstable/reactivity";

const { AtomRegistry: Registry } = Reactivity;
export const registry = Registry.make();
