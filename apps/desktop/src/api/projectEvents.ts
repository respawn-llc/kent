import type * as Stream from "effect/Stream";
import { subscriptionStream } from "./subscriptionStream";
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
  return subscriptionStream<ProjectObservation>(
    (offer) =>
      transport.subscribe(
        "workflow.subscribeProject",
        { project_id: projectID },
        workflowProjectEventRpcHandler("workflow.project", {
          onOpen: () => {
            offer({ kind: "open" });
          },
          onEvent: (event) => {
            if (event.projectID === null || event.projectID === projectID) offer({ kind: "event", event });
          },
          onComplete: (code, message) => {
            offer({ kind: "complete", code, message });
          },
          onError: (error) => {
            offer({ kind: "error", error });
          },
        }),
      ),
    async (observation) => reportOverflow(projectID, observation),
  );
}
