import { createContext, createElement, useCallback, useContext, useMemo, type ReactNode } from "react";
import { useAtomSuspense } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/reactivity/Atom";

import { useAppServices } from "./useAppServices";
import type { AppServices } from "./services";

const WindowFocusContext = createContext<boolean | null | undefined>(undefined);

export function useOpenExternalLink(): (url: string) => void {
  const { nativeBridge, logger } = useAppServices();
  return useCallback(
    (url: string) => {
      void nativeBridge.links.openExternal(url).catch((error: unknown) => {
        void logger.append("warn", "Open external link failed.", {
          error: error instanceof Error ? error.message : "unknown",
        });
      });
    },
    [logger, nativeBridge],
  );
}

export function WindowFocusProvider({ children }: Readonly<{ children: ReactNode }>) {
  const focused = useWindowFocusSource();
  return createElement(WindowFocusContext.Provider, { value: focused }, children);
}

export function useWindowFocus(): boolean | null {
  const focused = useContext(WindowFocusContext);
  if (focused === undefined) {
    throw new Error("WindowFocusProvider is required.");
  }
  return focused;
}

function useWindowFocusSource(): boolean | null {
  const { logger, nativeBridge } = useAppServices();
  const focused = useMemo(
    () => Atom.make(windowFocusObservation(nativeBridge, logger), { initialValue: null }),
    [logger, nativeBridge.window],
  );
  return useAtomSuspense(focused).value;
}

function windowFocusObservation(
  nativeBridge: AppServices["nativeBridge"],
  logger: AppServices["logger"],
): Stream.Stream<boolean | null> {
  return Stream.suspend(() => {
    let observed = false;
    const changes = nativeBridge.window
      .focusChanges(async () => logger.reportObservationOverflow("focus"))
      .pipe(
        Stream.catch((error) => Stream.fromEffect(reportFocusRegistrationFailure(logger, error))),
        Stream.tap(() =>
          Effect.sync(() => {
            observed = true;
          }),
        ),
      );
    const initial = Stream.fromEffect(readInitialFocus(nativeBridge, logger)).pipe(
      Stream.filter(() => !observed),
    );
    return Stream.merge(changes, initial);
  });
}

const reportFocusRegistrationFailure = Effect.fn("reportFocusRegistrationFailure")(function* (
  logger: AppServices["logger"],
  error: Error,
) {
  yield* Effect.promise(async () =>
    logger.append("warn", "Listening for native window focus changes failed.", {
      error: error.message,
    }),
  );
  return false;
});

const reportInitialFocusReadFailure = Effect.fn("reportInitialFocusReadFailure")(function* (
  logger: AppServices["logger"],
  cause: unknown,
) {
  yield* Effect.promise(async () =>
    logger.append("warn", "Reading native window focus state failed.", {
      error: String(cause),
    }),
  );
  return false;
});

const readInitialFocus = Effect.fn("readInitialFocus")(function* (
  nativeBridge: AppServices["nativeBridge"],
  logger: AppServices["logger"],
) {
  return yield* Effect.tryPromise(async () => nativeBridge.window.isFocused()).pipe(
    Effect.catch((error) => reportInitialFocusReadFailure(logger, error.cause)),
  );
});
