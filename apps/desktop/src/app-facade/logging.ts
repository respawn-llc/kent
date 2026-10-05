import type { NativeLogEntry } from "@app/native-bridge";

import { recoverOrThrowDebugFailure } from "./debugFailure";

export type AppLogLevel = NativeLogEntry["level"];

export type AppLogger = Readonly<{
  append(level: AppLogLevel, message: string, context?: Readonly<Record<string, string>>): Promise<void>;
}>;

export type AppObservationLogger = AppLogger &
  Readonly<{
    reportObservationOverflow(observation: string): Promise<void>;
  }>;

export function createAppLogger(append: AppLogger["append"]): AppObservationLogger {
  const logger = { append };
  return {
    append,
    async reportObservationOverflow(observation) {
      return recoverOrThrowDebugFailure({
        context: { observation },
        error: new Error("Shell observation exceeded its 1,000-event capacity."),
        logger,
        message: "Shell observation buffer overflow.",
        recover: () => undefined,
      });
    },
  };
}
