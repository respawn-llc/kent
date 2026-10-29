import * as Effect from "effect/Effect";
import * as Queue from "effect/Queue";
import * as Stream from "effect/Stream";
import * as Atom from "effect/reactivity/Atom";
import { RegistryProvider, useAtomSuspense } from "@effect/atom-react";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import type { ChatApi, ChatSessionTarget, ChatGoalObservation, ChatGoalStreamObservation } from "@/api";

export type GoalObservationHandler = Readonly<{
  onOpen?(): void;
  onEvent(value: ChatGoalObservation): void;
  onComplete(code: number | null, message: string | null): void;
  onError(error: Error): void;
}>;

export function goalSource(register: (handler: GoalObservationHandler) => { close(): void }) {
  return Stream.callback<ChatGoalStreamObservation>((queue) =>
    Effect.acquireRelease(
      Effect.sync(() =>
        register({
          onOpen: () => {
            Queue.offerUnsafe(queue, { kind: "open" });
          },
          onEvent: (event) => {
            Queue.offerUnsafe(queue, { kind: "event", event });
          },
          onComplete: (code, message) => {
            Queue.offerUnsafe(queue, { kind: "complete", code, message });
          },
          onError: (error) => {
            Queue.offerUnsafe(queue, { kind: "error", error });
          },
        }),
      ),
      (subscription) =>
        Effect.sync(() => {
          subscription.close();
        }),
    ),
  );
}

export function observeGoal(
  api: Pick<ChatApi, "subscribeGoal">,
  target: ChatSessionTarget,
  handler: GoalObservationHandler,
) {
  const observation = Atom.make(
    api
      .subscribeGoal(target, async () => {
        throw new Error("Unexpected Goal observation overflow");
      })
      .pipe(
        Stream.runForEach((value) =>
          Effect.sync(() => {
            switch (value.kind) {
              case "open":
                handler.onOpen?.();
                break;
              case "event":
                handler.onEvent(value.event);
                break;
              case "complete":
                handler.onComplete(value.code, value.message);
                break;
              case "error":
                handler.onError(value.error);
                break;
            }
          }),
        ),
        Effect.as(null),
      ),
    { initialValue: null },
  );
  const view = renderHook(() => useAtomSuspense(observation), {
    wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider>,
  });
  return {
    flush: async () => {
      await act(async () => undefined);
    },
    close: () => {
      view.unmount();
    },
  };
}
