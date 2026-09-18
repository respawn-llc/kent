import { InfiniteQueryObserver, type InfiniteData, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import { boardNodeCardsPageSize, type ApiService, type BoardNodeCardsPage } from "@/api";
import { queryAtom, queryKeys, retainQueryData, type RetainedQueryData } from "@/app-facade";
import type { BoardQueryState } from "./BoardQueryRuntime";

type Inputs = Scope &
  Pick<BoardQueryState, "filter" | "sort" | "queriesEnabled"> &
  Readonly<{ enabled: boolean }>;
type Cards = InfiniteData<BoardNodeCardsPage, number>;
type Scope = Readonly<{ projectID: string; workflowID: string; nodeID: string }>;

export function createBoardColumnQueryModel(api: ApiService, client: QueryClient, initial: Inputs) {
  const inputs = Atom.make(initial);
  const options = ({ filter, sort, queriesEnabled, enabled, ...scope }: Inputs) => ({
    queryKey: queryKeys.boardNodeCards({ ...scope, filter, sort }),
    queryFn: async ({ pageParam }: { pageParam: number }) =>
      api.listBoardNodeCards({ ...scope, filter, sort, offset: pageParam }),
    initialPageParam: 0,
    enabled:
      queriesEnabled &&
      enabled &&
      scope.projectID.length > 0 &&
      scope.workflowID.length > 0 &&
      scope.nodeID.length > 0,
    getPreviousPageParam: (_first: BoardNodeCardsPage, _pages: BoardNodeCardsPage[], first: number) =>
      first === 0 ? undefined : Math.max(0, first - boardNodeCardsPageSize),
    getNextPageParam: (last: BoardNodeCardsPage) => last.nextOffset ?? undefined,
    maxPages: 3,
    gcTime: 0,
    placeholderData: (previous: Cards | undefined) => previous,
  });
  const observer = new InfiniteQueryObserver<BoardNodeCardsPage, Error, Cards, readonly unknown[], number>(
    client,
    options(initial),
  );
  const configuration = Atom.make((get) => {
    observer.setOptions(options(get(inputs)));
  });
  const request = queryAtom(observer);
  const retained = Atom.make<RetainedQueryData<Cards, Scope> | null>(null);
  const state = Atom.make((get) => {
    get.mount(retained);
    get(configuration);
    const current = get(request);
    const previous = get.once(retained);
    const next = retainQueryData(
      previous,
      { scope: get.once(inputs), data: current.data, retain: true },
      (a, b) => a.projectID === b.projectID && a.workflowID === b.workflowID && a.nodeID === b.nodeID,
    );
    if (previous !== next.retained) get.set(retained, next.retained);
    return {
      ...current,
      data: next.data,
      isPlaceholderData: current.isPlaceholderData || (next.data !== undefined && next.data !== current.data),
    };
  });
  const page = Atom.fn<"previous" | "next">()(
    (direction, get) =>
      Effect.promise(async () => {
        const current = observer.getCurrentResult();
        const input = get(inputs);
        if (
          !input.enabled ||
          !input.queriesEnabled ||
          get(state).isPlaceholderData ||
          current.data === undefined ||
          current.isFetching
        )
          return;
        if (direction === "next" && current.hasNextPage) await observer.fetchNextPage();
        if (direction === "previous" && current.hasPreviousPage) await observer.fetchPreviousPage();
      }),
    { concurrent: true },
  );
  const retry = Atom.fn(() => Effect.promise(async () => observer.refetch()), { concurrent: true });
  return { inputs, state, page, retry } as const;
}
