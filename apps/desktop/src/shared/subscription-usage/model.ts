import { QueriesObserver, QueryObserver, type QueryClient, type QueryKey } from "@tanstack/react-query";
import * as Effect from "effect/Effect";
import * as Atom from "effect/reactivity/Atom";
import { SubscriptionReadError, type ApiService, type SubscriptionUsage } from "@/api";
import { queryAtom, queryKeys } from "@/app-facade";

const explicitReads = {
  retry: false,
  networkMode: "always",
  staleTime: 0,
  refetchOnWindowFocus: false,
  refetchOnReconnect: false,
  refetchOnMount: false,
} as const;

export function createSubscriptionUsageModel({
  api,
  client,
  sessionID,
}: Readonly<{
  api: ApiService;
  client: QueryClient;
  sessionID: string | null;
}>) {
  const catalogObserver = new QueryObserver(client, {
    queryKey: queryKeys.subscriptionCatalog(),
    queryFn: async () => api.getSubscriptionConnections(),
    ...explicitReads,
    enabled: sessionID === null,
  });
  const bindingObserver =
    sessionID === null
      ? null
      : new QueryObserver(client, {
          queryKey: queryKeys.subscriptionBinding(sessionID),
          queryFn: async () => api.getSubscriptionBinding(sessionID),
          ...explicitReads,
          enabled: false,
        });
  const catalog = queryAtom(catalogObserver);
  const binding = bindingObserver === null ? null : queryAtom(bindingObserver);
  const connections = Atom.make((get) => {
    if (binding !== null) {
      const result = get(binding);
      const value =
        result.error instanceof SubscriptionReadError && result.error.binding !== null
          ? result.error.binding
          : result.data;
      return value?.supported === true ? [value.connectionID] : [];
    }
    return (
      get(catalog)
        .data?.filter((item) => item.supported)
        .map((item) => item.id) ?? []
    );
  });
  function usageOptions(id: string) {
    return {
      queryKey: queryKeys.subscriptionUsage(id),
      queryFn: async () => api.getSubscriptionUsage(id),
      ...explicitReads,
    };
  }
  const observations = Atom.make((get) =>
    queryAtom<SubscriptionUsage | null>(
      new QueriesObserver(
        client,
        get(connections).map((id) => ({ ...usageOptions(id), enabled: false })),
      ),
    ),
  );
  const state = Atom.make((get) => {
    const prerequisite = binding === null ? get(catalog) : get(binding);
    const ids = get(connections);
    const results = get(get(observations));
    const knownFailure =
      binding !== null &&
      prerequisite.error instanceof SubscriptionReadError &&
      prerequisite.error.binding !== null;
    return {
      rows: ids.map((id, index) => {
        const result = results[index];
        return {
          id,
          data: result?.data,
          error: knownFailure ? prerequisite.error : (result?.error ?? null),
          fetching: result?.isFetching === true,
        };
      }),
      error: knownFailure ? null : prerequisite.error,
      loading: prerequisite.isFetching || results.some((result) => result.isFetching),
      failed: prerequisite.isError || results.some((result) => result.isError),
      hidden: prerequisite.isSuccess && ids.length === 0,
    };
  });
  const refresh = Effect.fnUntraced(function* (failedOnly: boolean) {
    let ids: readonly string[];
    let prerequisiteRetried = false;
    if (bindingObserver !== null) {
      const resolved = yield* resolvePrerequisite(bindingObserver, failedOnly);
      const result = resolved.result;
      prerequisiteRetried = resolved.retried;
      if (result.isError || result.data?.supported !== true) return;
      ids = [result.data.connectionID];
    } else {
      const resolved = yield* resolvePrerequisite(catalogObserver, failedOnly);
      const result = resolved.result;
      prerequisiteRetried = resolved.retried;
      if (result.isError || result.data === undefined) return;
      ids = result.data.filter((item) => item.supported).map((item) => item.id);
    }
    const selected = ids.filter((id) => {
      if (!failedOnly) return true;
      const result = client.getQueryState(queryKeys.subscriptionUsage(id));
      return result?.status === "error" || (prerequisiteRetried && result?.data === undefined);
    });
    yield* Effect.forEach(
      selected,
      (id) =>
        // Query owns the failure and cached data; each result remains independently observable.
        Effect.promise(async () => client.fetchQuery(usageOptions(id)).catch(() => undefined)),
      { concurrency: 4, discard: true },
    );
  });
  const open = Atom.fn(() => refresh(false), { concurrent: true });
  const retry = Atom.fn<undefined>()(
    (_, get) =>
      Effect.gen(function* () {
        const prerequisite = bindingObserver?.getCurrentResult() ?? catalogObserver.getCurrentResult();
        if (prerequisite.isFetching) return;
        const ids = get(connections);
        if (
          ids.some((id) => client.getQueryState(queryKeys.subscriptionUsage(id))?.fetchStatus === "fetching")
        )
          return;
        yield* refresh(true);
      }),
    { concurrent: true },
  );
  const busy = Atom.make((get) => get(open).waiting || get(retry).waiting || get(state).loading);
  const visible = Atom.make((get) => {
    const result = get(catalog);
    return !result.isSuccess || result.data.some((connection) => connection.supported);
  });
  return { state, open, retry, busy, visible } as const;
}

const resolvePrerequisite = Effect.fnUntraced(function* <T, K extends QueryKey>(
  observer: QueryObserver<T, Error, T, T, K>,
  failedOnly: boolean,
) {
  const current = observer.getCurrentResult();
  if (!failedOnly || current.isError || current.data === undefined) {
    const result = yield* Effect.promise(async () => observer.refetch({ cancelRefetch: false }));
    return { result, retried: true };
  }
  return { result: current, retried: false };
});
