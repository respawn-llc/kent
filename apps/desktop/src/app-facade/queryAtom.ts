import {
  type InfiniteQueryObserver,
  type MutationObserver,
  type QueryObserver,
  type InfiniteQueryObserverResult,
  type QueryKey,
  type QueryObserverResult,
  type MutationObserverResult,
} from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";

export type QuerySnapshot<R> = R extends unknown
  ? Readonly<Omit<R, "refetch" | "fetchNextPage" | "fetchPreviousPage" | "mutate" | "reset" | "promise">>
  : never;

export function queryAtom<Q, E, A, K extends QueryKey, P>(
  observer: InfiniteQueryObserver<Q, E, A, K, P>,
): Atom.Atom<QuerySnapshot<InfiniteQueryObserverResult<A, E>>>;
export function queryAtom<Q, E, A, D, K extends QueryKey>(
  observer: QueryObserver<Q, E, A, D, K>,
): Atom.Atom<QuerySnapshot<QueryObserverResult<A, E>>>;
export function queryAtom<A, E, V>(
  observer: MutationObserver<A, E, V>,
): Atom.Atom<QuerySnapshot<MutationObserverResult<A, E, V>>>;
export function queryAtom<Q, E, A, D, K extends QueryKey, V, P>(
  observer: QueryObserver<Q, E, A, D, K> | MutationObserver<A, E, V> | InfiniteQueryObserver<Q, E, A, K, P>,
): Atom.Atom<
  QuerySnapshot<
    QueryObserverResult<A, E> | InfiniteQueryObserverResult<A, E> | MutationObserverResult<A, E, V>
  >
> {
  return Atom.make((get) => {
    // Query owns the result object. Atom owns only this subscription, not a second cache.
    get.addFinalizer(
      observer.subscribe((result) => {
        get.setSelf(result);
      }),
    );
    return observer.getCurrentResult();
  });
}
