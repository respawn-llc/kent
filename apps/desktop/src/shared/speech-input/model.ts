import { QueryObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import * as Exit from "effect/Exit";
import { useAtomSet } from "@effect/atom-react";
import { SpeechOperationError, SpeechTransportError, assertSpeechSize, type SpeechResult } from "@/api";
import { queryAtom, queryKeys, speechSourceBytes, type AppServices, type SpeechSource } from "@/app-facade";
import {
  captureMicrophone,
  speechAttempt,
  speechSync,
  SpeechActionError,
  type CaptureSession,
  type CompletedClip,
} from "./capture";
import { isDiscardableClip } from "./clip";
import type { createSpeechRecovery } from "./RecoveryModel";
import type { SpeechTarget } from "./insertion";

type Phase =
  | Readonly<{ kind: "idle" }>
  | Readonly<{ kind: "preparing"; field: string | symbol }>
  | Readonly<{ kind: "recording" | "transcribing"; field: string | symbol; session: CaptureSession }>;

export function createSpeechDestination({
  services,
  client,
  deliver,
  recover,
  report,
  onLimit,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  deliver(
    source: SpeechSource,
    result: SpeechResult,
    target: SpeechTarget,
  ): ReturnType<ReturnType<typeof createSpeechRecovery>["deliver"]>;
  recover(source: SpeechSource, error: Error, target: SpeechTarget): void;
  report(error: Error): void;
  onLimit(): void;
}>) {
  const phase = Atom.make<Phase>({ kind: "idle" });
  const availability = queryAtom(
    new QueryObserver(client, {
      queryKey: queryKeys.speechAvailability,
      queryFn: async () => services.api.speechAvailability(),
      retry: false,
    }),
  );
  const state = Atom.make((get) => {
    const current = get(phase);
    return {
      phase: current.kind,
      available: get(availability).data ?? null,
      availability: get(availability),
      busy: current.kind !== "idle",
      origin: current.kind === "idle" ? null : current.field,
      recording:
        current.kind === "recording"
          ? {
              analyser: current.session.analyser,
              startedAt: current.session.startedAt,
            }
          : null,
    };
  });
  const discardable = (clip: CompletedClip) =>
    isDiscardableClip({
      bytes: speechSourceBytes(clip.source),
      durationMs: clip.source.durationMs,
      hasSignal: clip.hasSignal,
    });
  const start = Atom.fn<Readonly<{ target: SpeechTarget; field: string | symbol }>>()(
    ({ target, field }, get) =>
      Effect.gen(function* () {
        if ((yield* Atom.get(phase)).kind !== "idle") return;
        const available = (yield* Atom.get(availability)).data;
        if (available == null) return;
        yield* Atom.set(phase, { kind: "preparing", field });
        yield* Effect.scoped(
          Effect.gen(function* () {
            const session = yield* captureMicrophone(services, available, {
              onStop: () => {
                const current = get(phase);
                if (current.kind === "recording") get.set(phase, { ...current, kind: "transcribing" });
              },
              onLimit,
              report,
            });
            yield* Effect.addFinalizer((exit) =>
              speechAttempt(async () => {
                if (Exit.isFailure(exit)) {
                  session.stop();
                  const clip = await session.finished;
                  await session.transfer.cancel().catch((cause: unknown) => {
                    report(new SpeechActionError(cause));
                  });
                  if (discardable(clip)) await services.speech.discard(clip.source);
                  else recover(clip.source, clip.error ?? new SpeechOperationError("closed"), target);
                }
              }).pipe(
                Effect.catch((error) =>
                  Effect.sync(() => {
                    report(error);
                  }),
                ),
              ),
            );
            yield* Atom.set(phase, { kind: "recording", field, session });
            const clip = yield* speechAttempt(async () => session.finished);
            yield* Atom.set(phase, { kind: "transcribing", field, session });
            if (discardable(clip)) {
              yield* speechAttempt(session.transfer.cancel);
              yield* speechAttempt(async () => services.speech.discard(clip.source));
              return;
            }
            yield* Effect.gen(function* () {
              if (clip.error !== null) return yield* Effect.fail(new SpeechActionError(clip.error));
              yield* speechSync(() => {
                assertSpeechSize(speechSourceBytes(clip.source), available);
              });
              const result = yield* speechAttempt(session.transfer.result).pipe(
                Effect.catch((error) =>
                  Effect.gen(function* () {
                    if (!(error.cause instanceof SpeechTransportError) || clip.source.kind !== "native")
                      return yield* Effect.fail(error);
                    const fresh = yield* speechAttempt(async () => services.api.speechAvailability());
                    if (fresh === null)
                      return yield* Effect.fail(
                        new SpeechActionError(new SpeechOperationError("unavailable")),
                      );
                    const request = yield* Effect.acquireRelease(
                      speechAttempt(async () => services.speech.retry(clip.source, fresh)),
                      (request) =>
                        speechAttempt(request.cancel).pipe(
                          Effect.catch((error) =>
                            Effect.sync(() => {
                              report(error);
                            }),
                          ),
                        ),
                    );
                    return yield* speechAttempt(request.result);
                  }),
                ),
              );
              yield* deliver(clip.source, result, target);
            }).pipe(
              Effect.catch((error) =>
                Effect.sync(() => {
                  recover(clip.source, error, target);
                }),
              ),
            );
          }),
        ).pipe(
          Effect.catch((error) =>
            Effect.sync(() => {
              report(error);
            }),
          ),
          Effect.ensuring(Atom.set(phase, { kind: "idle" })),
        );
      }),
    { concurrent: true },
  );
  const stop = Atom.fn(() =>
    Effect.gen(function* () {
      const current = yield* Atom.get(phase);
      if (current.kind !== "recording") return;
      current.session.stop();
      yield* Atom.set(phase, { ...current, kind: "transcribing" });
    }),
  );
  const refresh = Atom.fn(() =>
    speechAttempt(async () => client.refetchQueries({ queryKey: queryKeys.speechAvailability })),
  );
  return { state, start, stop, refresh } as const;
}

export type SpeechDestination = ReturnType<typeof createSpeechDestination>;

export function useSpeechActions(
  model: SpeechDestination,
  field: string | symbol,
  target: () => SpeechTarget,
) {
  const start = useAtomSet(model.start);
  return {
    start: () => {
      start({ target: target(), field });
    },
    stop: useAtomSet(model.stop),
  };
}
