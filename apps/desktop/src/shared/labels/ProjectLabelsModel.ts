import { QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import { queryAtom, queryKeys, type AppServices } from "@/app-facade";
import { createProjectLabelFilter } from "./projectLabelFilter";
import { createProjectLabelEffects } from "./labelEventEffects";

export function createProjectLabelsModel({
  services,
  client,
  projectID,
  enabled,
  report,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  projectID: string;
  enabled: boolean;
  report: (error: unknown) => void;
}>) {
  const observer = new QueryObserver(client, {
    queryKey: queryKeys.projectLabels(projectID),
    queryFn: async () => services.api.listProjectLabels(projectID),
    enabled,
    retry: false,
  });
  const catalog = queryAtom(observer);
  const filter = createProjectLabelFilter({
    catalog,
    projectID,
    namespace: services.storageNamespace,
    report,
  });
  const effects = createProjectLabelEffects({ projectID, queryClient: client, onBackgroundError: report });
  const refresh = Atom.fn(() =>
    Effect.promise(async () => {
      await observer.refetch();
    }),
  );
  return { projectID, catalog, filter, effects, refresh } as const;
}

export type ProjectLabelsModel = ReturnType<typeof createProjectLabelsModel>;
