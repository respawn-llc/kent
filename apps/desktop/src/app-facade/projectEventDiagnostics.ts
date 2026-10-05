import type { ProjectObservation } from "@/api";
import { recoverOrThrowDebugFailure } from "./debugFailure";
import type { AppLogger } from "./logging";

export function projectEventDiagnostics(logger: AppLogger) {
  return async (projectID: string, observation: ProjectObservation): Promise<void> =>
    recoverOrThrowDebugFailure({
      logger,
      message: "Project observation buffer overflow.",
      error: new Error("Project observation exceeded its 1,000-event capacity."),
      context: {
        projectID,
        kind: observation.kind,
        ...(observation.kind === "event"
          ? {
              primaryEntityID: observation.event.primaryEntityID,
              resource: observation.event.resource,
              action: observation.event.action,
            }
          : {}),
      },
      recover: () => undefined,
    });
}
