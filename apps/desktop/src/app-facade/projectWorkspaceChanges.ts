import * as Effect from "effect/Effect";
import * as Queue from "effect/Queue";
import * as Stream from "effect/Stream";
import type { AppServices } from "./services";

type WorkspaceBridge = AppServices["nativeBridge"]["projectWorkspace"];
type WorkspaceChanged = Parameters<Parameters<WorkspaceBridge["onChanged"]>[0]>[0];
type WorkspaceObservationError = Readonly<{ _tag: "WorkspaceObservationError"; cause: unknown }>;

export function projectWorkspaceChanges(
  bridge: WorkspaceBridge,
  projectID: string,
): Stream.Stream<WorkspaceChanged, WorkspaceObservationError> {
  return Stream.callback<WorkspaceChanged, WorkspaceObservationError>(
    (queue) =>
      Effect.acquireRelease(
        Effect.tryPromise({
          try: async () =>
            bridge.onChanged((event) => {
              if (event.projectID === projectID) Queue.offerUnsafe(queue, event);
            }),
          catch: (cause): WorkspaceObservationError => ({ _tag: "WorkspaceObservationError", cause }),
        }),
        (unlisten) => Effect.sync(unlisten),
        // Stream.callback runs registration in a child fiber; fail its supplied queue too.
      ).pipe(Effect.catch((error) => Queue.fail(queue, error))),
    { bufferSize: 1, strategy: "sliding" },
  );
}
