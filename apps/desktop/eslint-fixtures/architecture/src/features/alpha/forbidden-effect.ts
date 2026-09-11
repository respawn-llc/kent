import { Effect as Fx, Scope } from "effect";
import { runPromise as launch } from "effect/Effect";
import * as E from "effect/Effect";
import { RegistryContext } from "@effect/atom-react";
import * as Registry from "effect/unstable/reactivity/AtomRegistry";

const alias = Fx;
const forward = alias.runSync;
const { runFork: fork } = E;
const dynamic = E[Math.random() ? "runSync" : "runPromise"];
export { runSync as execute } from "effect/Effect";
export const detached = Fx.forkDetach(Fx.void);
export const root = Scope.make();
export const registry = Registry.make();
export const global = RegistryContext;
export const executions = [launch, forward, fork, dynamic];
