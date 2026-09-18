import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import * as Queue from "effect/Queue";
import * as Stream from "effect/Stream";
import type { QueryObserver } from "@tanstack/react-query";
import type { NativeBridge } from "@app/native-bridge";

export function observe<A>(observer: QueryObserver<A>) {
  return Atom.make((get) => {
    get.addFinalizer(observer.subscribe((result) => get.setSelf(result)));
    return observer.getCurrentResult();
  });
}

export function changes(bridge: NativeBridge, projectID: string) {
  return Stream.callback<{ projectID: string }>(
    (queue) =>
      Effect.acquireRelease(
        Effect.promise(() =>
          bridge.projectDeletion.onDeleted((event) => {
            if (event.projectID === projectID) Queue.offerUnsafe(queue, event);
          }),
        ),
        (release) => Effect.sync(release),
      ),
    { bufferSize: 1, strategy: "sliding" },
  );
}
