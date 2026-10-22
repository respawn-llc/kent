import type { RootOptions } from "react-dom/client";

import { errorMessage } from "@/api";
import type { AppLogger } from "@/app-facade";

export function createDevelopmentReactErrorHandlers(logger: AppLogger): RootOptions {
  function report(kind: "caught" | "uncaught" | "recoverable"): NonNullable<RootOptions["onCaughtError"]> {
    return (error, info) => {
      reportError(error);
      void logger.append("error", errorMessage(error), {
        kind,
        ...(error instanceof Error && error.stack !== undefined ? { stack: error.stack } : {}),
        ...(info.componentStack == null ? {} : { componentStack: info.componentStack }),
      });
    };
  }
  return {
    onCaughtError: report("caught"),
    onUncaughtError: report("uncaught"),
    onRecoverableError: report("recoverable"),
  };
}
