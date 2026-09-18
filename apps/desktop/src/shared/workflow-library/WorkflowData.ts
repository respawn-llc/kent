import { InfiniteQueryObserver, useQueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";

import { workflowPageSize } from "@/api";
import { queryAtom, queryKeys, useAppServices } from "@/app-facade";

export function useWorkflowPages(query = "", enabled = true) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const model = useMemo(() => {
    const observer = new InfiniteQueryObserver(client, {
      queryKey: queryKeys.workflows(query),
      queryFn: async ({ pageParam }) =>
        api.listWorkflows({ offset: pageParam, limit: workflowPageSize, query }),
      initialPageParam: 0,
      getNextPageParam: (lastPage) => lastPage.nextOffset ?? undefined,
      enabled,
    });
    const nextPage = Atom.fn(
      () =>
        Effect.promise(async () => {
          const current = observer.getCurrentResult();
          if (enabled && current.hasNextPage && !current.isFetching) await observer.fetchNextPage();
        }),
      { concurrent: true },
    );
    const retry = Atom.fn(
      () =>
        Effect.promise(async () => {
          if (enabled && !observer.getCurrentResult().isFetching) await observer.refetch();
        }),
      { concurrent: true },
    );
    return { request: queryAtom(observer), nextPage, retry } as const;
  }, [api, client, query, enabled]);
  const nextPage = useAtomSet(model.nextPage, { mode: "value" });
  const retry = useAtomSet(model.retry, { mode: "value" });
  return {
    ...useAtomValue(model.request),
    fetchNextPage: () => {
      nextPage(undefined);
    },
    refetch: () => {
      retry(undefined);
    },
  };
}
