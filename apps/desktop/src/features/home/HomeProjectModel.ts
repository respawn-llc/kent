import { useMemo, useState } from "react";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import {
  InfiniteQueryObserver,
  QueryObserver,
  useQueryClient,
  type QueryClient,
} from "@tanstack/react-query";
import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import { isProjectMissingError, type ProjectObservation } from "@/api";
import {
  clearLastProjectRoute,
  mainSessionCatalogInfiniteQueryOptions,
  subagentSessionCatalogInfiniteQueryOptions,
  queryAtom,
  infiniteQueryReadActions,
  queryKeys,
  useAppServices,
  useProjectObservation,
  ProjectObservationFailure,
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
  const consume = useMemo(
    () => (observation: ProjectObservation) => {
      if (
        observation.kind !== "open" &&
        !(observation.kind === "event" && observation.event.resource === "session_catalog")
      )
        return Effect.void;
      return Effect.tryPromise({
        try: async () => {
          await client.invalidateQueries({ queryKey: queryKeys.projectSessionCatalogs(projectID) });
        },
        catch: (error) => new ProjectObservationFailure(error),
      });
    },
    [client, projectID],
  );
  const observation = useProjectObservation(enabled ? `${projectID}-${category}` : null, projectID, consume);
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
  const query = useAtomValue(model.request);
  const retry = useAtomSet(model.retry);
  return {
    ...query,
    error: query.error ?? observation.error,
    isError: query.isError || observation.error !== null,
    fetchNextPage: useAtomSet(model.nextPage),
    refetch: () => {
      retry();
      observation.retry();
    },
  };
}
