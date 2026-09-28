import type { QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import { errorMessage, type TaskDetail } from "@/api";
import { reportNonCancelledError, type AppServices } from "@/app-facade";
import { createProjectLabelEffects } from "@/shared/labels";
import {
  dependencyRelatedTaskIDs,
  workflowProjectEventAffectsDependencyDetail,
} from "@/shared/task-dependencies";
import { refreshTaskDetailReads } from "./taskDetailQueries";

export function createTaskDetailObservation({
  services,
  client,
  taskID,
  active,
  detail,
  currentDetail,
  reportLabelError,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  taskID: string;
  active: Atom.Atom<boolean>;
  detail: Atom.Atom<{ readonly data: TaskDetail | undefined }>;
  currentDetail(): TaskDetail | undefined;
  reportLabelError(error: unknown): void;
}>) {
  const project = Atom.make((get) => get(detail).data?.projectID ?? null);
  return Atom.make(
    (get) => {
      const enabled = get(active);
      const projectID = get(project);
      if (!enabled || projectID === null) return Stream.succeed(null);
      const labels = createProjectLabelEffects({
        projectID,
        queryClient: client,
        onBackgroundError: reportLabelError,
      });
      const refreshOrReport = async (operation: Promise<unknown>) => {
        await operation.catch((error: unknown) => {
          reportNonCancelledError(error, (failure) => {
            void services.logger.append("warn", "Task detail live refresh failed.", {
              error: errorMessage(failure),
            });
          });
        });
      };
      return services.api.subscribeProject(projectID).pipe(
        Stream.mapEffect((observation) =>
          Effect.promise(async () => {
            switch (observation.kind) {
              case "open":
                await refreshOrReport(
                  Promise.all([
                    refreshTaskDetailReads(client, taskID),
                    labels.refreshAfterSubscriptionBoundary(),
                  ]),
                );
                break;
              case "event": {
                await refreshOrReport(labels.consumeProjectEvent(observation.event));
                const task = currentDetail();
                if (
                  task !== undefined &&
                  workflowProjectEventAffectsDependencyDetail(
                    observation.event,
                    taskID,
                    dependencyRelatedTaskIDs(task.dependencies),
                  )
                ) {
                  await refreshOrReport(refreshTaskDetailReads(client, taskID));
                }
                break;
              }
              case "complete":
                break;
              case "error":
                await services.logger.append("warn", "Task detail subscription failed.", {
                  error: errorMessage(observation.error),
                });
                return observation.error;
            }
            return null;
          }),
        ),
        Stream.scan<Error | null, Error | null>(null, (previous, error) => previous ?? error),
        Stream.prepend([null]),
      );
    },
    { initialValue: null },
  );
}
