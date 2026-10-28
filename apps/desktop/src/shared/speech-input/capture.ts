import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import { errorMessage, SpeechOperationError, type SpeechAvailable } from "@/api";
import { nativeObservation, type AppServices, type SpeechSource } from "@/app-facade";

export type CompletedClip = Readonly<{ source: SpeechSource; hasSignal: boolean; error: Error | null }>;
type RecorderEvent =
  | Readonly<{ kind: "chunk"; chunk: Blob }>
  | Readonly<{ kind: "stop" }>
  | Readonly<{ kind: "error"; error: Error }>;

export class SpeechActionError extends Error {
  readonly _tag = "SpeechActionError";
  constructor(cause: unknown) {
    super(errorMessage(cause), { cause });
  }
}
export const speechAttempt = <A>(task: () => Promise<A>) =>
  Effect.tryPromise({ try: task, catch: (cause) => new SpeechActionError(cause) });
export const speechSync = <A>(task: () => A) =>
  Effect.try({ try: task, catch: (cause) => new SpeechActionError(cause) });

export const captureMicrophone = Effect.fn("captureMicrophone")(function* (
  services: AppServices,
  available: SpeechAvailable,
  { onStop, onLimit, report }: Readonly<{ onStop(): void; onLimit(): void; report(error: Error): void }>,
) {
  if (services.speech.microphone === "native") {
    return yield* captureNativeMicrophone(services, available, { onStop, onLimit, report });
  }
  if (
    !("MediaRecorder" in globalThis) ||
    !("mediaDevices" in navigator) ||
    !("getUserMedia" in navigator.mediaDevices)
  ) {
    return yield* Effect.fail(new SpeechActionError(new SpeechOperationError("captureUnavailable")));
  }
  const encoding = yield* speechSync(() =>
    available.encodings.find((candidate) => MediaRecorder.isTypeSupported(candidate.mimeType)),
  );
  if (encoding === undefined)
    return yield* Effect.fail(new SpeechActionError(new SpeechOperationError("format")));
  const stream = yield* Effect.acquireRelease(
    speechAttempt(async () =>
      navigator.mediaDevices.getUserMedia({ audio: { channelCount: 1 }, video: false }),
    ),
    (stream) =>
      Effect.sync(() => {
        for (const track of stream.getTracks()) track.stop();
      }),
  );
  const audio = yield* Effect.acquireRelease(
    speechSync(() => new AudioContext()),
    (audio) =>
      speechAttempt(async () => audio.close()).pipe(
        Effect.catch((error) =>
          Effect.sync(() => {
            report(error);
          }),
        ),
      ),
  );
  const analyser = audio.createAnalyser();
  analyser.fftSize = 256;
  audio.createMediaStreamSource(stream).connect(analyser);
  yield* speechAttempt(async () => audio.resume());
  const recorder = yield* speechSync(
    () =>
      new MediaRecorder(stream, {
        mimeType: encoding.mimeType,
        audioBitsPerSecond: encoding.mimeType === "audio/mp4" ? 64_000 : 32_000,
      }),
  );
  const transfer = yield* Effect.acquireRelease(
    speechAttempt(async () => services.speech.begin(available, encoding)),
    (transfer) =>
      speechAttempt(transfer.cancel).pipe(
        Effect.catch((error) =>
          Effect.sync(() => {
            report(error);
          }),
        ),
      ),
  );
  let complete!: (clip: CompletedClip) => void;
  let captureStarted!: (time: number) => void;
  let startupFailed!: (error: unknown) => void;
  const ready = new Promise<number>((resolve, fail) => {
    captureStarted = resolve;
    startupFailed = fail;
  });
  let reject!: (error: unknown) => void;
  const finished = new Promise<CompletedClip>((resolve, fail) => {
    complete = resolve;
    reject = fail;
  });
  let startedAt: number | null = null;
  let stoppedAt: number | null = null;
  let nextChunk: ReturnType<typeof setTimeout> | undefined;
  let limitTimer: ReturnType<typeof setTimeout> | undefined;
  let hasSignal = false;
  let failure: Error | null = null;
  const samples = new Uint8Array(analyser.fftSize);
  const sampleSignal = () => {
    analyser.getByteTimeDomainData(samples);
    hasSignal ||= samples.some((sample) => Math.abs(sample - 128) > 1);
  };
  const stop = () => {
    clearTimeout(nextChunk);
    clearTimeout(limitTimer);
    if (stoppedAt !== null) return;
    sampleSignal();
    stoppedAt = performance.now();
    if (recorder.state !== "inactive") recorder.stop();
    for (const track of stream.getTracks()) track.stop();
    onStop();
  };
  const stopAtLimit = () => {
    if (stoppedAt !== null) return;
    stop();
    onLimit();
  };
  const requestChunk = () => {
    if (recorder.state !== "recording") return;
    sampleSignal();
    recorder.requestData();
  };
  let observationReady!: () => void;
  const observing = new Promise<void>((resolve) => {
    observationReady = resolve;
  });
  const events = nativeObservation<RecorderEvent>(
    async (emit) => {
      const chunk = (event: BlobEvent) => {
        emit({ kind: "chunk", chunk: event.data });
      };
      const stopped = () => {
        startupFailed(new SpeechOperationError("recording"));
        emit({ kind: "stop" });
      };
      const started = () => {
        if (stoppedAt !== null) return;
        startedAt = performance.now();
        nextChunk = setTimeout(requestChunk, 250);
        limitTimer = setTimeout(stopAtLimit, available.maxDurationSeconds * 1000);
        captureStarted(startedAt);
      };
      const failed = () => {
        const error = new SpeechOperationError("recording");
        startupFailed(error);
        emit({ kind: "error", error });
      };
      recorder.addEventListener("start", started);
      recorder.addEventListener("dataavailable", chunk);
      recorder.addEventListener("stop", stopped);
      recorder.addEventListener("error", failed);
      observationReady();
      return () => {
        recorder.removeEventListener("dataavailable", chunk);
        recorder.removeEventListener("stop", stopped);
        recorder.removeEventListener("error", failed);
        recorder.removeEventListener("start", started);
      };
    },
    async () => services.logger.reportObservationOverflow("speech-recorder"),
  );
  yield* events.pipe(
    Stream.runForEach((event) =>
      Effect.promise(async () => {
        switch (event.kind) {
          case "error":
            failure = event.error;
            stop();
            break;
          case "chunk":
            try {
              const written = await transfer.append(event.chunk);
              if (written.limitReached) stopAtLimit();
              else if (recorder.state === "recording") nextChunk = setTimeout(requestChunk, 250);
            } catch (cause) {
              failure = cause instanceof Error ? cause : new Error(String(cause));
              stop();
            }
            break;
          case "stop":
            stop();
            try {
              const durationMs = startedAt === null ? 0 : (stoppedAt ?? performance.now()) - startedAt;
              const source = await transfer.finalize(durationMs);
              complete({ source, hasSignal, error: failure });
            } catch (cause) {
              reject(cause);
            }
            break;
        }
      }),
    ),
    Effect.forkScoped({ startImmediately: true }),
  );
  yield* Effect.acquireRelease(Effect.void, () =>
    speechAttempt(async () => {
      if (startedAt === null) {
        const source = await transfer.finalize(0);
        await services.speech.discard(source);
      } else {
        stop();
        await finished;
      }
    }).pipe(
      Effect.catch((error) =>
        Effect.sync(() => {
          report(error);
        }),
      ),
    ),
  );
  yield* speechAttempt(async () => observing);
  yield* speechSync(() => {
    recorder.start();
    startedAt = performance.now();
  });
  return { analyser, ready, stop, finished, transfer };
});

const captureNativeMicrophone = Effect.fn("captureNativeMicrophone")(function* (
  services: AppServices,
  available: SpeechAvailable,
  { onStop, onLimit, report }: Readonly<{ onStop(): void; onLimit(): void; report(error: Error): void }>,
) {
  const encoding = available.encodings.find((candidate) => candidate.mimeType === "audio/mp4");
  if (encoding === undefined)
    return yield* Effect.fail(new SpeechActionError(new SpeechOperationError("format")));
  const transfer = yield* Effect.acquireRelease(
    speechAttempt(async () => services.speech.begin(available, encoding)),
    (transfer) =>
      speechAttempt(transfer.cancel).pipe(
        Effect.catch((error) =>
          Effect.sync(() => {
            report(error);
          }),
        ),
      ),
  );
  const microphone = transfer.microphone;
  if (microphone === undefined)
    return yield* Effect.fail(new SpeechActionError(new SpeechOperationError("captureUnavailable")));
  let startedAt: number | null = null;
  let stoppedAt: number | null = null;
  let power = 0;
  let hasSignal = false;
  let limitTimer: ReturnType<typeof setTimeout> | undefined;
  let resolveReady!: (time: number) => void;
  let rejectReady!: (error: Error) => void;
  const ready = new Promise<number>((resolve, reject) => {
    resolveReady = resolve;
    rejectReady = reject;
  });
  let complete!: (clip: CompletedClip) => void;
  let rejectFinished!: (error: unknown) => void;
  const finished = new Promise<CompletedClip>((resolve, reject) => {
    complete = resolve;
    rejectFinished = reject;
  });
  const stop = () => {
    if (stoppedAt !== null) return;
    stoppedAt = performance.now();
    clearTimeout(limitTimer);
    onStop();
    void microphone.stop().catch((cause: unknown) => {
      const error = new SpeechActionError(cause);
      rejectReady(error);
      rejectFinished(error);
      report(error);
    });
  };
  const analyser = {
    frequencyBinCount: 56,
    getByteFrequencyData: (data: Uint8Array) => {
      data.fill(Math.round(Math.min(1, power) * 255));
    },
  };
  yield* Effect.acquireRelease(
    speechAttempt(async () =>
      microphone.start((event) => {
        switch (event.kind) {
          case "ready":
            if (stoppedAt !== null) return;
            startedAt = performance.now();
            limitTimer = setTimeout(() => {
              stop();
              onLimit();
            }, available.maxDurationSeconds * 1000);
            resolveReady(startedAt);
            break;
          case "meter":
            power = event.power;
            hasSignal ||= power > 1 / 128;
            break;
          case "limit":
            if (stoppedAt !== null) return;
            stop();
            onLimit();
            break;
          case "finished": {
            clearTimeout(limitTimer);
            stoppedAt ??= performance.now();
            onStop();
            const error = event.error === null ? null : new SpeechActionError(event.error);
            rejectReady(error ?? new SpeechOperationError("recording"));
            const durationMs = startedAt === null ? 0 : stoppedAt - startedAt;
            void transfer.finalize(durationMs).then((source) => {
              complete({ source, hasSignal, error });
            }, rejectFinished);
            break;
          }
        }
      }),
    ),
    () =>
      speechAttempt(async () => {
        stop();
        await finished;
      }).pipe(
        Effect.catch((error) =>
          Effect.sync(() => {
            report(error);
          }),
        ),
      ),
  );
  return { analyser, ready, stop, finished, transfer };
});

export type CaptureSession = Effect.Success<ReturnType<typeof captureMicrophone>>;
