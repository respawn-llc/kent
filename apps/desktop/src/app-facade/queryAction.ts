import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import type {
  InfiniteQueryObserver,
  MutationObserver,
  QueryObserver,
  QueryObserverResult,
  QueryKey,
} from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/unstable/reactivity/Atom";
import { queryAtom } from "./queryAtom";

export function queryReadActions<Q, E, A, D, K extends QueryKey>(observer: QueryObserver<Q, E, A, D, K>) {
  return {
    retry: guardedReadAction(observer, async () => observer.refetch()),
  } as const;
}

export function infiniteQueryReadActions<Q, E, A, K extends QueryKey, P>(
  observer: InfiniteQueryObserver<Q, E, A, K, P>,
) {
  return {
    ...queryReadActions(observer),
    nextPage: guardedReadAction(
      observer,
      async () => observer.fetchNextPage(),
      (result) => result.hasNextPage,
    ),
    previousPage: guardedReadAction(
      observer,
      async () => observer.fetchPreviousPage(),
      (result) => result.hasPreviousPage,
    ),
  } as const;
}

function guardedReadAction<R extends Pick<QueryObserverResult, "isEnabled" | "isFetching">>(
  observer: Readonly<{ getCurrentResult(): R }>,
  execute: () => Promise<unknown>,
  available: (result: R) => boolean = () => true,
) {
  return Atom.fn(
    () =>
      Effect.promise(async () => {
        const current = observer.getCurrentResult();
        if (current.isEnabled && !current.isFetching && available(current)) await execute();
      }),
    { concurrent: true },
  );
}

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
