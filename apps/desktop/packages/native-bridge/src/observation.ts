import * as Cause from "effect/Cause";
import * as Effect from "effect/Effect";
import * as Queue from "effect/Queue";
import * as Stream from "effect/Stream";

export type NativeOverflowReporter = () => Promise<void>;

export function nativeEmitter<A, E>(queue: Queue.Enqueue<A, E>, reportOverflow: NativeOverflowReporter) {
  return (value: A): void => {
    if (!Queue.offerUnsafe(queue, value)) {
      void reportOverflow().catch((error: unknown) => {
        Queue.failCauseUnsafe(queue, Cause.die(error));
      });
    }
  };
}

export function nativeObservation<A>(
  register: (emit: (value: A) => void) => Promise<() => void>,
  reportOverflow: NativeOverflowReporter,
): Stream.Stream<A, Error> {
  return Stream.callback<A, Error>(
    Effect.fn("nativeObservation")(function* (queue: Queue.Queue<A, Error | Cause.Done>) {
      // Registration completes before scope cleanup, even when disposal arrives while it is pending.
      yield* Effect.acquireRelease(
        Effect.tryPromise({
          try: async () => register(nativeEmitter(queue, reportOverflow)),
          catch: (cause) => (cause instanceof Error ? cause : new Error(String(cause))),
        }),
        (cleanup) => Effect.sync(cleanup),
      ).pipe(Effect.catch((error) => Queue.fail(queue, error)));
    }),
    { bufferSize: 1000, strategy: "dropping" },
  );
}
