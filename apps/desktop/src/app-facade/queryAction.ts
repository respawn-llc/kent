import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import type { MutationObserver } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import { queryAtom } from "./queryAtom";

export function queryAction<A, E, V, C>(observer: MutationObserver<A, E, V, C>) {
  const request = queryAtom(observer);
  const action = Atom.fn<V>()(
    (input) =>
      Effect.gen(function* () {
        if (observer.getCurrentResult().isPending) return;
        yield* Effect.tryPromise(async () => observer.mutate(input)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const reset = Atom.fn(() =>
    Effect.sync(() => {
      if (!observer.getCurrentResult().isPending) observer.reset();
    }),
  );
  return { request, submit: action, reset } as const;
}

export function useQueryAction<A, E, V, C>(model: ReturnType<typeof queryAction<A, E, V, C>>) {
  useAtomMount(model.request);
  return {
    ...useAtomValue(model.request),
    submit: useAtomSet(model.submit, { mode: "value" }),
    reset: useAtomSet(model.reset, { mode: "value" }),
  };
}
