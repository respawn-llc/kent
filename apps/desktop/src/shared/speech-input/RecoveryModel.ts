import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import * as Fiber from "effect/Fiber";
import { useAtomMount, useAtomSet } from "@effect/atom-react";
import type { TFunction } from "i18next";
import { errorMessage, SpeechFailure, type SpeechResult } from "@/api";
import {
  recoverOrThrowDebugFailure,
  type AppServices,
  type SpeechSource,
  type StatusController,
} from "@/app-facade";
import { writeClipboardText } from "@/shared/native-clipboard";
import { speechAttempt, SpeechActionError } from "./capture";
import { deliverSpeech, type Delivery } from "./recovery";
import type { SpeechTarget } from "./insertion";

export type RecoveryEntry = Readonly<{
  source: SpeechSource;
  target: SpeechTarget;
  error: Error;
  delivery?: Delivery;
}>;
type Work =
  Readonly<{ kind: "idle" | "discarding" }> | Readonly<{ kind: "retrying"; fiber: Fiber.Fiber<void> }>;

export function createSpeechRecovery({
  services,
  t,
  push,
}: Readonly<{
  services: AppServices;
  t: TFunction;
  push: StatusController["push"];
}>) {
  const entry = Atom.make<RecoveryEntry | null>(null);
  const work = Atom.make<Work>({ kind: "idle" });
  const lifetime = Atom.make(Effect.scope);
  const state = Atom.make((get) => ({ entry: get(entry), work: get(work).kind }));
  const report = (error: unknown) => {
    push({
      id: "speech-recovery-action",
      tone: "danger",
      title: t("speech.failed"),
      body: errorMessage(error),
    });
  };
  const offer = Atom.fnSync<RecoveryEntry>()((incoming, get) => {
    if (get(entry) === null) {
      get.set(entry, incoming);
      return;
    }
    push({
      id: incoming.source.kind === "native" ? incoming.source.original.path : crypto.randomUUID(),
      tone: "danger",
      title: t("speech.additionalRecording"),
      body: incoming.source.kind === "native" ? incoming.source.original.path : t("speech.browserTemporary"),
      durationMs: Infinity,
      actionLabel: t("speech.saveCopy"),
      onAction: () => {
        void services.speech.saveCopy(incoming.source, t("speech.saveCopy")).catch(report);
      },
    });
  });
  const deliverResult = Effect.fn("deliverSpeechResult")(function* (
    input: Readonly<{ source: SpeechSource; target: SpeechTarget; result: SpeechResult }>,
  ) {
    const delivery = yield* speechAttempt(async () =>
      deliverSpeech(input.source, input.target, input.result, {
        discard: services.speech.discard,
        copy: async (text) => writeClipboardText(text, services.nativeBridge),
      }),
    );
    if (delivery.kind === "inserted" || delivery.kind === "noop") {
      if ((yield* Atom.get(entry))?.source === input.source) yield* Atom.set(entry, null);
      return;
    }
    const retained = {
      source: input.source,
      target: input.target,
      delivery,
      error: new Error(t("speech.copied")),
    };
    if ((yield* Atom.get(entry))?.source === input.source) yield* Atom.set(entry, retained);
    else yield* Atom.set(offer, retained);
    if (delivery.kind === "copied") {
      push({ id: "speech-copied", tone: "info", title: t("speech.copied"), durationMs: Infinity });
    } else {
      yield* Effect.promise(async () =>
        recoverOrThrowDebugFailure({
          context: { feature: "speech" },
          error: delivery.error,
          logger: services.logger,
          message: t("speech.copyFailed"),
          recover: () => undefined,
        }),
      );
    }
  });
  const retry = Atom.fn(
    () =>
      Effect.gen(function* () {
        if ((yield* Atom.get(work)).kind !== "idle") return;
        const current = yield* Atom.get(entry);
        if (current === null) return;
        const scope = yield* Atom.getResult(lifetime);
        const attempt = Effect.scoped(
          Effect.gen(function* () {
            const available = yield* speechAttempt(async () => services.api.speechAvailability());
            if (available === null)
              return yield* Effect.fail(
                new SpeechActionError(new SpeechFailure("unavailable", t("speech.unavailable"))),
              );
            const request = yield* Effect.acquireRelease(
              speechAttempt(async () => services.speech.retry(current.source, available)),
              (request) =>
                speechAttempt(request.cancel).pipe(
                  Effect.catch((error) =>
                    Effect.sync(() => {
                      report(error);
                    }),
                  ),
                ),
            );
            const result = yield* speechAttempt(request.result);
            yield* deliverResult({ source: current.source, target: current.target, result });
          }),
        ).pipe(
          Effect.catch((error) =>
            Effect.gen(function* () {
              if ((yield* Atom.get(entry))?.source === current.source)
                yield* Atom.set(entry, { ...current, error });
            }),
          ),
          Effect.ensuring(
            Effect.gen(function* () {
              if ((yield* Atom.get(work)).kind === "retrying") yield* Atom.set(work, { kind: "idle" });
            }),
          ),
        );
        const fiber = yield* Effect.forkIn(attempt, scope);
        yield* Atom.set(work, { kind: "retrying", fiber });
      }),
    { concurrent: true },
  );
  const discard = Atom.fn(
    () =>
      Effect.gen(function* () {
        const currentWork = yield* Atom.get(work);
        if (currentWork.kind === "discarding") return;
        const current = yield* Atom.get(entry);
        if (current === null) return;
        yield* Atom.set(work, { kind: "discarding" });
        yield* Effect.gen(function* () {
          if (currentWork.kind === "retrying") yield* Fiber.interrupt(currentWork.fiber);
          yield* speechAttempt(async () => services.speech.discard(current.source));
          yield* Atom.set(entry, null);
        }).pipe(
          Effect.catch((error) =>
            Effect.sync(() => {
              report(error);
            }),
          ),
          Effect.ensuring(Atom.set(work, { kind: "idle" })),
        );
      }),
    { concurrent: true },
  );
  const fileAction = Atom.fn<"reveal" | "save">()((action) =>
    Effect.gen(function* () {
      const current = yield* Atom.get(entry);
      if (current === null) return;
      yield* speechAttempt(async () =>
        action === "reveal"
          ? services.speech.reveal(current.source)
          : services.speech.saveCopy(current.source, t("speech.saveCopy")),
      ).pipe(
        Effect.catch((error) =>
          Effect.sync(() => {
            report(error);
          }),
        ),
      );
    }),
  );
  return { state, lifetime, offer, deliver: deliverResult, retry, discard, fileAction } as const;
}

export function useRecoveryActions(model: ReturnType<typeof createSpeechRecovery>) {
  useAtomMount(model.lifetime);
  return {
    offer: useAtomSet(model.offer),
    retry: useAtomSet(model.retry),
    discard: useAtomSet(model.discard),
    fileAction: useAtomSet(model.fileAction),
  };
}
