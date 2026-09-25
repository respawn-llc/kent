import { useMemo, useState } from "react";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import {
  InfiniteQueryObserver,
  QueryObserver,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import { isProjectMissingError } from "@/api";
import {
  clearLastProjectRoute,
  mainSessionCatalogInfiniteQueryOptions,
  subagentSessionCatalogInfiniteQueryOptions,
  queryAtom,
  infiniteQueryReadActions,
  queryKeys,
  useAppServices,
  type AppServices,
} from "@/app-facade";
import { useStableCallback } from "@/ui";

export function createHomeProjectModel(services: AppServices, client: QueryClient, projectID: string) {
  const metadata = queryAtom(
    new QueryObserver(client, {
      queryKey: queryKeys.projectEdit(projectID),
      queryFn: async () => services.api.getProjectEdit(projectID),
    }),
  );
  return { metadata } as const;
}

export function useHomeProjectModel(projectID: string, returnHome: () => Promise<void>) {
  const services = useAppServices();
  const client = useQueryClient();
  const [model] = useState(() => createHomeProjectModel(services, client, projectID));
  const leave = useStableCallback(returnHome);
  const missing = useMemo(
    () =>
      Atom.make((get) => {
        const error = get(model.metadata).error;
        return Effect.gen(function* () {
          if (!isProjectMissingError(error)) return;
          clearLastProjectRoute(projectID);
          yield* Effect.promise(leave);
        });
      }),
    [leave, model, projectID],
  );
  useAtomMount(missing);
}

export function useHomeSessionPages(projectID: string, category: "main" | "subagent", enabled: boolean) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const model = useMemo(() => {
    const observer = new InfiniteQueryObserver(client, {
      ...(category === "main"
        ? mainSessionCatalogInfiniteQueryOptions(api, projectID)
        : subagentSessionCatalogInfiniteQueryOptions(api, projectID)),
      enabled,
    });
    return {
      request: queryAtom(observer),
      ...infiniteQueryReadActions(observer),
    };
  }, [api, client, projectID, category, enabled]);
  return {
    ...useAtomValue(model.request),
    fetchNextPage: useAtomSet(model.nextPage),
    refetch: useAtomSet(model.retry),
  };
}
