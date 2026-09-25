import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Queue from "effect/Queue";
import * as Cause from "effect/Cause";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { QueryClient } from "@tanstack/react-query";
import type { TFunction } from "i18next";
import { errorMessage } from "@/api";
import { queryKeys, type AppServices, type StatusController } from "@/app-facade";

class ProjectCreationObservationFailure extends Error {
  readonly _tag = "ProjectCreationObservationFailure";
  constructor(cause: unknown) {
    super(errorMessage(cause), { cause });
  }
}

export function createHomeCreationObservation({
  services,
  client,
  openProject,
  push,
  t,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  openProject: (projectID: string) => Promise<void>;
  push: StatusController["push"];
  t: TFunction;
}>) {
  const events = services.nativeBridge.capabilities.projectCreationWindow
    ? Stream.callback<Readonly<{ projectID: string }>, ProjectCreationObservationFailure>(
        (queue) =>
          Effect.acquireRelease(
            Effect.tryPromise({
              try: async () =>
                services.nativeBridge.projectCreation.onCreated((binding) => {
                  if (!Queue.offerUnsafe(queue, binding)) {
                    Queue.failCauseUnsafe(
                      queue,
                      Cause.fail(
                        new ProjectCreationObservationFailure(
                          new Error(t("home.projectCreationObservationOverflow")),
                        ),
                      ),
                    );
                  }
                }),
              catch: (cause) => new ProjectCreationObservationFailure(cause),
            }),
            (unlisten) => Effect.sync(unlisten),
          ).pipe(Effect.catch((error) => Queue.fail(queue, error))),
        { bufferSize: 1000 },
      )
    : Stream.empty;
  return Atom.make(
    events.pipe(
      Stream.mapEffect(
        Effect.fn("Home.projectCreated")(function* (binding) {
          yield* Effect.promise(async () => client.invalidateQueries({ queryKey: queryKeys.projects }));
          yield* Effect.tryPromise({
            try: async () => openProject(binding.projectID),
            catch: (cause) => new ProjectCreationObservationFailure(cause),
          });
        }),
      ),
      Stream.runDrain,
      Effect.catch((error) =>
        Effect.sync(() => {
          push({
            id: "project-create-observation-error",
            tone: "danger",
            title: t("home.projectCreateWindowError"),
            body: errorMessage(error),
          });
        }),
      ),
    ),
  );
}
