import { errorMessage } from "@/api";

import type { AppLogger } from "./logging";

export type RecoverableFailureContext = Readonly<Record<string, string>>;

export async function recoverOrThrowDebugFailure({
  context,
  error,
  logger,
  message,
  recover,
}: Readonly<{
  context: RecoverableFailureContext;
  error: unknown;
  logger: AppLogger;
  message: string;
  recover: () => void;
}>): Promise<void> {
  const debug = kentDebugModeEnabled();
  const evidence =
    debug && error instanceof Error && error.cause !== undefined ? JSON.stringify(error.cause) : undefined;
  const evidenceID = evidence === undefined ? undefined : crypto.randomUUID();
  const diagnostic = {
    ...context,
    error: errorMessage(error),
    ...(error instanceof Error && error.stack !== undefined ? { stack: error.stack } : {}),
    ...(evidenceID === undefined ? {} : { evidenceID }),
  };
  const logged = logger.append("warn", message, diagnostic);
  if (debug) {
    return logged.then(async () => {
      if (evidence !== undefined && evidenceID !== undefined) {
        // JSON escaping can expand each character sixfold. These chunks stay
        // below the native logger's 64 KiB entry limit even for control characters.
        const chunkLength = 8192;
        const count = Math.ceil(evidence.length / chunkLength);
        for (let index = 0; index < count; index++) {
          await logger.append("warn", message, {
            evidenceID,
            evidenceIndex: String(index),
            evidenceCount: String(count),
            evidenceChunk: evidence.slice(index * chunkLength, (index + 1) * chunkLength),
          });
        }
      }
      throw new Error(`${message} ${JSON.stringify(diagnostic)}`, { cause: error });
    });
  }
  recover();
  return logged;
}

export function kentDebugModeEnabled(): boolean {
  return debugEnvEnabled(import.meta.env.KENT_DEBUG) || debugQueryEnabled();
}

function debugEnvEnabled(value: unknown): boolean {
  return value === true || value === "true" || value === "1";
}

function debugQueryEnabled(): boolean {
  if (typeof window === "undefined") {
    return false;
  }
  return new URLSearchParams(window.location.search).get("debug") === "true";
}
