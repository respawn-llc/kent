import { recoverOrThrowDebugFailure } from "./debugFailure";
import type { AppLogger } from "./logging";

export function shellObservationDiagnostics(logger: AppLogger, observation: string) {
  return async (): Promise<void> =>
    recoverOrThrowDebugFailure({
      logger,
      message: "Shell observation buffer overflow.",
      error: new Error("Shell observation exceeded its 1,000-event capacity."),
      context: { observation },
      recover: () => undefined,
    });
}
