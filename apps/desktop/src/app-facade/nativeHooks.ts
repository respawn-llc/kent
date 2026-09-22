import { createContext, createElement, useCallback, useContext, useMemo, type ReactNode } from "react";
import { useAtomSuspense } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/unstable/reactivity/Atom";

import { useAppServices } from "./useAppServices";
import { shellObservationDiagnostics } from "./shellObservationDiagnostics";

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
    () =>
      Atom.make(
        Stream.suspend((): Stream.Stream<boolean | null> => {
          let observed = false;
          const changes = nativeBridge.window.focusChanges(shellObservationDiagnostics(logger, "focus")).pipe(
            Stream.catch((error) =>
              Stream.fromEffect(
                Effect.promise(async () =>
                  logger.append("warn", "Listening for native window focus changes failed.", {
                    error: error.message,
                  }),
                ).pipe(Effect.as(false)),
              ),
            ),
            Stream.tap(() =>
              Effect.sync(() => {
                observed = true;
              }),
            ),
          );
          const initial = Stream.fromEffect(
            Effect.tryPromise(async () => nativeBridge.window.isFocused()).pipe(
              Effect.catch((error) =>
                Effect.promise(async () =>
                  logger.append("warn", "Reading native window focus state failed.", {
                    error: String(error.cause),
                  }),
                ).pipe(Effect.as(false)),
              ),
            ),
          ).pipe(Stream.filter(() => !observed));
          return Stream.merge(changes, initial);
        }),
        { initialValue: null },
      ),
    [logger, nativeBridge.window],
  );
  return useAtomSuspense(focused).value;
}
