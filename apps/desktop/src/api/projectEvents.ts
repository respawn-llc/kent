import type { StreamFailureCode } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import type * as Stream from "effect/Stream";
import { subscriptionStream } from "./subscriptionStream";
import type { RpcTransport } from "./transport";
import { subscribeWorkflowProject, type WorkflowProjectEvent } from "./workflowProjectEvents";

export type ProjectObservation =
  | Readonly<{ kind: "open" }>
  | Readonly<{ kind: "event"; event: WorkflowProjectEvent }>
  | Readonly<{
      kind: "complete";
      code: StreamFailureCode | null;
      message: string | null;
    }>
  | Readonly<{ kind: "error"; error: Error }>;

export type ProjectOverflowReporter = (projectID: string, observation: ProjectObservation) => Promise<void>;

export function projectEvents(
  transport: RpcTransport,
  projectID: string,
  reportOverflow: ProjectOverflowReporter,
): Stream.Stream<ProjectObservation> {
  return subscriptionStream<ProjectObservation>(
    (offer) =>
      subscribeWorkflowProject(transport, projectID, {
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
    async (observation) => reportOverflow(projectID, observation),
  );
}
