import * as Cause from "effect/Cause";
import * as Effect from "effect/Effect";
import * as Queue from "effect/Queue";
import * as Stream from "effect/Stream";
import type { DescriptorRpcTransport } from "./transport";
import { workflowProjectEventRpcHandler, type WorkflowProjectEvent } from "./workflowProjectEvents";

export type ProjectObservation =
  | Readonly<{ kind: "open" }>
  | Readonly<{ kind: "event"; event: WorkflowProjectEvent }>
  | Readonly<{ kind: "complete"; code: number; message: string }>
  | Readonly<{ kind: "error"; error: Error }>;

export type ProjectOverflowReporter = (projectID: string, observation: ProjectObservation) => Promise<void>;

export function projectEvents(
  transport: DescriptorRpcTransport,
  projectID: string,
  reportOverflow: ProjectOverflowReporter,
): Stream.Stream<ProjectObservation> {
  return Stream.callback<ProjectObservation>(
    (queue) => {
      const offer = (observation: ProjectObservation) => {
        if (!Queue.offerUnsafe(queue, observation)) {
          void reportOverflow(projectID, observation).catch((error: unknown) => {
            Queue.failCauseUnsafe(queue, Cause.die(error));
          });
        }
      };
      return Effect.acquireRelease(
        Effect.sync(() =>
          transport.subscribe(
            "workflow.subscribeProject",
            { project_id: projectID },
            workflowProjectEventRpcHandler("workflow.project", {
              onOpen: () => {
                offer({ kind: "open" });
              },
              onEvent: (event) => {
                if (event.projectID === null || event.projectID === projectID)
                  offer({ kind: "event", event });
              },
              onComplete: (code, message) => {
                offer({ kind: "complete", code, message });
              },
              onError: (error) => {
                offer({ kind: "error", error });
              },
            }),
          ),
        ),
        (subscription) =>
          Effect.sync(() => {
            subscription.close();
          }),
      );
    },
    { bufferSize: 1000, strategy: "dropping" },
  );
}
