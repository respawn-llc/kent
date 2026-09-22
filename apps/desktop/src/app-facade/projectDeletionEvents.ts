import type { NativeBridge, NativeProjectDeleted } from "@app/native-bridge";
import type { QueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { useAtomSuspense } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/unstable/reactivity/Atom";

import { errorMessage } from "@/api";
import { useStableCallback } from "@/ui";
import { clearLastProjectRoute } from "./projectRoutePersistence";
import { queryKeys } from "./queryKeys";
import { removeProjectTaskSearches } from "./taskSearchQueries";
import { useAppServices } from "./useAppServices";
import { shellObservationDiagnostics } from "./shellObservationDiagnostics";

export function useProjectDeletedEvents(
  nativeBridge: NativeBridge,
  handler: (event: NativeProjectDeleted) => Promise<void>,
) {
  const { logger } = useAppServices();
  const handle = useStableCallback(handler);
  const model = useMemo(() => {
    const action = Atom.fn<NativeProjectDeleted>()(
      (event) =>
        Effect.tryPromise(async () => handle(event)).pipe(
          Effect.catch((error) =>
            Effect.promise(async () =>
              logger.append("warn", "Project deletion event handling failed.", {
                error: errorMessage(error.cause),
              }),
            ),
          ),
          Effect.as(null),
        ),
      { concurrent: true, initialValue: null },
    );
    const deleted = Atom.make(
      (get) =>
        nativeBridge.projectDeletion.deleted(shellObservationDiagnostics(logger, "project-deletion")).pipe(
          Stream.runForEach((event) =>
            Effect.sync(() => {
              get.set(action, event);
            }),
          ),
          Effect.catch((error) =>
            Effect.promise(async () =>
              logger.append("warn", "Project deletion event listener failed.", {
                error: errorMessage(error),
              }),
            ),
          ),
          Effect.as(null),
        ),
      { initialValue: null },
    );
    return { action, deleted };
  }, [handle, logger, nativeBridge.projectDeletion]);
  useAtomSuspense(model.action);
  useAtomSuspense(model.deleted);
}

export async function completeProjectDeletion({
  navigateHome,
  projectID,
  pushDeletedToast,
  queryClient,
}: Readonly<{
  navigateHome?: (() => Promise<void>) | undefined;
  projectID: string;
  pushDeletedToast: () => void;
  queryClient: QueryClient;
}>): Promise<void> {
  await invalidateProjectDeleteQueries(queryClient, projectID);
  clearLastProjectRoute(projectID);
  if (navigateHome !== undefined) await navigateHome();
  pushDeletedToast();
}

export async function invalidateProjectDeleteQueries(
  queryClient: QueryClient,
  projectID: string,
): Promise<void> {
  await removeProjectTaskSearches(queryClient, projectID);
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: queryKeys.projects }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allProjectEdits }),
    queryClient.invalidateQueries({ queryKey: queryKeys.projectCatalog(projectID) }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allBoards }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allAttention }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allTasks }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allProjectWorkflowLinks, refetchType: "active" }),
  ]);
}
