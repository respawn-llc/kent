import { useState } from "react";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import {
  infiniteQueryOptions,
  keepPreviousData,
  InfiniteQueryObserver,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";

import type { AppServices } from "@/app-facade";
import { queryAtom, queryKeys } from "@/app-facade";
import { useAppServices } from "@/app-facade";

export function createHomeProjectPages(api: AppServices["api"], queryClient: QueryClient) {
  const observer = new InfiniteQueryObserver(queryClient, {
    queryKey: queryKeys.projects,
    queryFn: async ({ pageParam }: Readonly<{ pageParam: string | null }>) => api.listProjects(pageParam),
    initialPageParam: null,
    getNextPageParam: (lastPage) => lastPage.nextPageToken ?? undefined,
    placeholderData: keepPreviousData,
  });
  const lifetime = Atom.make((get) => {
    get.addFinalizer(() => {
      queryClient.removeQueries({ queryKey: queryKeys.projects, exact: true });
    });
  });
  return {
    request: queryAtom(observer),
    lifetime,
    nextPage: Atom.fn(
      () =>
        Effect.promise(async () => {
          const current = observer.getCurrentResult();
          if (current.isEnabled && !current.isFetching && current.hasNextPage) await observer.fetchNextPage();
        }),
      { concurrent: true },
    ),
    retry: Atom.fn(
      () =>
        Effect.promise(async () => {
          const current = observer.getCurrentResult();
          if (current.isEnabled && !current.isFetching) await observer.refetch();
        }),
      { concurrent: true },
    ),
  };
}

export function useProjectPages(model: ReturnType<typeof createHomeProjectPages>) {
  useAtomMount(model.lifetime);
  return {
    ...useAtomValue(model.request),
    fetchNextPage: useAtomSet(model.nextPage),
    refetch: useAtomSet(model.retry),
  };
}

export function useGlobalAttentionPages(model: ReturnType<typeof createHomeAttentionPages>) {
  return {
    ...useAtomValue(model.request),
    fetchNextPage: useAtomSet(model.nextPage),
    refetch: useAtomSet(model.retry),
  };
}

export function useSidebarGlobalAttentionPages() {
  const { api } = useAppServices();
  const client = useQueryClient();
  const [model] = useState(() => createHomeAttentionPages(api, client, true));
  return useGlobalAttentionPages(model);
}

export function createHomeAttentionPages(api: AppServices["api"], client: QueryClient, sidebar: boolean) {
  const observer = new InfiniteQueryObserver(client, {
    ...globalAttentionQueryOptions(api),
    refetchOnMount: (query) =>
      !sidebar || query.observers.filter((current) => current.getCurrentResult().isEnabled).length <= 1,
  });
  return {
    request: queryAtom(observer),
    nextPage: Atom.fn(
      () =>
        Effect.promise(async () => {
          const current = observer.getCurrentResult();
          if (current.isEnabled && !current.isFetching && current.hasNextPage) await observer.fetchNextPage();
        }),
      { concurrent: true },
    ),
    retry: Atom.fn(
      () =>
        Effect.promise(async () => {
          const current = observer.getCurrentResult();
          if (current.isEnabled && !current.isFetching) await observer.refetch();
        }),
      { concurrent: true },
    ),
  };
}

function globalAttentionQueryOptions(api: AppServices["api"]) {
  return infiniteQueryOptions({
    queryKey: queryKeys.attention,
    queryFn: async ({ pageParam }) => api.listAttention(pageParam),
    initialPageParam: "",
    getNextPageParam: (lastPage) => (lastPage.nextPageToken.length > 0 ? lastPage.nextPageToken : undefined),
    placeholderData: keepPreviousData,
  });
}
