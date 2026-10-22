import * as Cause from "effect/Cause";
import * as Effect from "effect/Effect";
import * as Queue from "effect/Queue";
import * as Stream from "effect/Stream";
import type { ApiSubscription } from "./apiService";

export function subscriptionStream<A>(
  subscribe: (emit: (value: A) => void) => ApiSubscription,
  reportOverflow: (value: A) => Promise<void>,
): Stream.Stream<A> {
  return Stream.callback<A>(
    (queue) =>
      Effect.acquireRelease(
        Effect.sync(() =>
          subscribe((value) => {
            if (!Queue.offerUnsafe(queue, value)) {
              void reportOverflow(value).catch((error: unknown) => {
                Queue.failCauseUnsafe(queue, Cause.die(error));
              });
            }
          }),
        ),
        (subscription) =>
          Effect.sync(() => {
            subscription.close();
          }),
      ),
    { bufferSize: 1000, strategy: "dropping" },
  );
}
