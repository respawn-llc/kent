import { useAtomSet } from "@effect/atom-react";
import { Effect } from "effect";
import { Atom } from "effect/unstable/reactivity";
const Fx = Effect;

const action = Atom.fn(
  () =>
    Effect.gen(function* () {
      yield* Effect.forkChild(Effect.void);
      yield* Fx.scoped(Fx.forkScoped(Fx.void));
    }),
  { concurrent: true },
);

export function Action() {
  const run = useAtomSet(action);
  return <button onClick={() => run()}>Run</button>;
}
