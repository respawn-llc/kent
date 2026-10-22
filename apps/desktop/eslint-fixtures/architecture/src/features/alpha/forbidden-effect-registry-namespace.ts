import * as Reactivity from "effect/reactivity";

const { AtomRegistry: Registry } = Reactivity;
export const registry = Registry.make();
