import { useMemo } from "react";
import { useAtomSet, useAtomSuspense, useAtomValue } from "@effect/atom-react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/unstable/reactivity/Atom";
import type { NativeBridge } from "@app/native-bridge";
import { errorMessage } from "@/api";
import { shellObservationDiagnostics, type AppLogger } from "@/app-facade";
import { checkForDesktopUpdate } from "./desktopUpdate";

type UpdateState =
  | Readonly<{ phase: "none" }>
  | Readonly<{ phase: "available" | "error"; version: string }>
  | Readonly<{ phase: "installing"; version: string; progressRatio: number | null }>;

export type DesktopUpdateState = UpdateState & Readonly<{ install(): void; dismiss(): void }>;

export function useDesktopUpdate(nativeBridge: NativeBridge, logger: AppLogger): DesktopUpdateState {
  const description = useMemo(
    () =>
      Atom.make((get) => {
        const state = Atom.make<UpdateState>({ phase: "none" });
        get.mount(state);
        const check = Atom.make(
          Effect.gen(function* () {
            const result = yield* checkForDesktopUpdate(nativeBridge, logger);
            if (result.available) get.set(state, { phase: "available", version: result.version });
            return null;
          }),
          { initialValue: null },
        );
        const install = Atom.fn(
          () =>
            Effect.gen(function* () {
              const current = get.once(state);
              if (current.phase !== "available" && current.phase !== "error") return null;
              const version = current.version;
              get.set(state, { phase: "installing", version, progressRatio: null });
              const installed = yield* nativeBridge.updates
                .downloadAndInstall(shellObservationDiagnostics(logger, "update-progress"))
                .pipe(
                  Stream.runForEach((progress) =>
                    Effect.sync(() => {
                      get.set(state, {
                        phase: "installing",
                        version,
                        progressRatio:
                          progress.totalBytes !== null && progress.totalBytes > 0
                            ? progress.downloadedBytes / progress.totalBytes
                            : null,
                      });
                    }),
                  ),
                  Effect.as(true),
                  Effect.catch((error) =>
                    Effect.promise(async () =>
                      logger.append("error", "Desktop update download/install failed.", {
                        error: errorMessage(error),
                      }),
                    ).pipe(Effect.as(false)),
                  ),
                );
              if (!installed) {
                get.set(state, { phase: "error", version });
                return null;
              }
              yield* Effect.tryPromise(async () => nativeBridge.updates.relaunch()).pipe(
                Effect.catch((error) =>
                  Effect.gen(function* () {
                    yield* Effect.promise(async () =>
                      logger.append("error", "Desktop update relaunch failed.", {
                        error: errorMessage(error.cause),
                      }),
                    );
                    get.set(state, { phase: "error", version });
                  }),
                ),
              );
              return null;
            }),
          { concurrent: true, initialValue: null },
        );
        const dismiss = Atom.fn(
          () =>
            Effect.sync(() => {
              if (get.once(state).phase !== "installing") get.set(state, { phase: "none" });
              return null;
            }),
          { concurrent: true, initialValue: null },
        );
        return { state: Atom.make((read) => read(state)), check, install, dismiss };
      }),
    [nativeBridge, logger],
  );
  const model = useAtomValue(description);
  useAtomSuspense(model.check);
  useAtomSuspense(model.install);
  const state = useAtomValue(model.state);
  const install = useAtomSet(model.install);
  const dismiss = useAtomSet(model.dismiss);
  return {
    ...state,
    install: () => {
      install();
    },
    dismiss: () => {
      dismiss();
    },
  };
}
