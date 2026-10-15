import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import { errorMessage, type SpeechAvailable } from "@/api";
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
  const encoding = yield* speechSync(() =>
    available.encodings.find((candidate) => MediaRecorder.isTypeSupported(candidate.mimeType)),
  );
  if (encoding === undefined)
    return yield* Effect.fail(new SpeechActionError("This device cannot record an accepted audio format."));
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
  const events = nativeObservation<RecorderEvent>(
    async (emit) => {
      const chunk = (event: BlobEvent) => {
        emit({ kind: "chunk", chunk: event.data });
      };
      const stopped = () => {
        emit({ kind: "stop" });
      };
      const failed = () => {
        emit({ kind: "error", error: new Error("Microphone recording failed.") });
      };
      recorder.addEventListener("dataavailable", chunk);
      recorder.addEventListener("stop", stopped);
      recorder.addEventListener("error", failed);
      return () => {
        recorder.removeEventListener("dataavailable", chunk);
        recorder.removeEventListener("stop", stopped);
        recorder.removeEventListener("error", failed);
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
  const startTime = yield* speechSync(() => {
    recorder.start();
    startedAt = performance.now();
    nextChunk = setTimeout(requestChunk, 250);
    limitTimer = setTimeout(stopAtLimit, available.maxDurationSeconds * 1000);
    return startedAt;
  });
  return { stream, analyser, startedAt: startTime, stop, finished, transfer };
});

export type CaptureSession = Effect.Success<ReturnType<typeof captureMicrophone>>;
