import * as Effect from "effect/Effect";
import * as Queue from "effect/Queue";
import * as Stream from "effect/Stream";
import * as Atom from "effect/reactivity/Atom";
import { RegistryProvider, useAtomSuspense } from "@effect/atom-react";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import type {
  ChatApi,
  ChatSessionTarget,
  ChatTranscriptCompletion,
  ChatTranscriptMessage,
  ChatTranscriptObservation,
} from "@/api";
import { TransportError } from "@/api";

export type ChatTranscriptHandler = Readonly<{
  onOpen?(): void;
  onTransportLoss?(): void;
  onEvent(event: ChatTranscriptMessage): void;
  onComplete(completion: ChatTranscriptCompletion): void;
  onError(error: Error): void;
}>;

export function transcriptSource(
  register: (handler: ChatTranscriptHandler) => { close(): void },
): Stream.Stream<ChatTranscriptObservation> {
  return Stream.callback<ChatTranscriptObservation>((queue) =>
    Effect.acquireRelease(
      Effect.sync(() =>
        register({
          onOpen: () => {
            Queue.offerUnsafe(queue, { kind: "open" });
          },
          onEvent: (event) => {
            Queue.offerUnsafe(queue, { kind: "event", event });
          },
          onComplete: (completion) => {
            Queue.offerUnsafe(queue, { kind: "complete", completion });
          },
          onError: (error) => {
            Queue.offerUnsafe(queue, { kind: "error", error });
          },
          onTransportLoss: () => {
            Queue.offerUnsafe(queue, { kind: "error", error: new TransportError("Test transport lost.") });
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

export function observeTranscript(
  api: Pick<ChatApi, "subscribeTranscript">,
  target: ChatSessionTarget,
  handler: ChatTranscriptHandler,
) {
  const observation = Atom.make(
    api
      .subscribeTranscript(target, async () => {
        throw new Error("Unexpected observation overflow");
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
                handler.onComplete(value.completion);
                break;
              case "error":
                if (value.error instanceof TransportError && handler.onTransportLoss !== undefined)
                  handler.onTransportLoss();
                else handler.onError(value.error);
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
