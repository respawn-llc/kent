import type { NativeBridge } from "@app/native-bridge";
import { z } from "zod";
import * as Effect from "effect/Effect";

import type { AppLogger } from "@/app-facade";

export type DesktopUpdateAvailability =
  Readonly<{ available: false }> | Readonly<{ available: true; version: string }>;

// Self-update is gated off on Homebrew installs (brew owns updates), on installs the
// platform updater cannot service (Linux deb/plain-binary, where only AppImage
// self-updates), and on shells without the updater capability; transient check
// failures stay silent so the next launch retries instead of surfacing an error chip.
export const checkForDesktopUpdate: (
  nativeBridge: NativeBridge,
  logger: AppLogger,
) => Effect.Effect<DesktopUpdateAvailability> = Effect.fn("checkForDesktopUpdate")(
  function* (nativeBridge: NativeBridge, logger: AppLogger) {
    if (!nativeBridge.capabilities.updater) {
      return { available: false } as const;
    }
    if (!(yield* Effect.tryPromise(async () => nativeBridge.updates.supported()))) {
      return { available: false } as const;
    }
    if (yield* isSelfUpdateDisabled(nativeBridge, logger)) {
      return { available: false } as const;
    }
    const result = yield* Effect.tryPromise(async () => nativeBridge.updates.check());
    return result.available
      ? ({ available: true, version: result.version } as const)
      : ({ available: false } as const);
  },
  (effect, _bridge, logger) =>
    effect.pipe(
      Effect.catch((error) =>
        Effect.promise(async () =>
          logger.append("warn", "Desktop update check failed.", { error: errorText(error.cause) }),
        ).pipe(Effect.as({ available: false } as const)),
      ),
    ),
);

// Reads the install-local self-update gate. A read failure is treated as "not
// disabled" so a missing/corrupt settings file never silently suppresses updates
// for direct-download installs; the cask explicitly writes "disabled" for brew.
const isSelfUpdateDisabled = Effect.fn("isSelfUpdateDisabled")(function* (
  nativeBridge: NativeBridge,
  logger: AppLogger,
) {
  return yield* Effect.tryPromise(async () => nativeBridge.settings.read()).pipe(
    Effect.map((settings) => settings.selfUpdate === "disabled"),
    Effect.catch((error) =>
      Effect.promise(async () =>
        logger.append("warn", "Desktop settings read failed.", { error: errorText(error.cause) }),
      ).pipe(Effect.as(false)),
    ),
  );
});

function errorText(error: unknown): string {
  if (error instanceof Error) {
    return error.message;
  }
  const message = z.string().safeParse(error);
  if (message.success) {
    return message.data;
  }
  return "Unknown desktop update error.";
}
