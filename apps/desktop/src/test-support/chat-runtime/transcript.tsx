import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import { RegistryProvider, useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { createChatTranscriptModel } from "@/app-facade";
import type { ChatTranscriptPayloadByKind } from "@/api";

function mountTranscript(
  api: Parameters<typeof createChatTranscriptModel>[0],
  target: Parameters<typeof createChatTranscriptModel>[1],
  options: {
    onContractFailure(error: Error): void;
    onOpeningFailure(error: Error): void;
    onScratchRehydration(): void;
  },
) {
  const model = createChatTranscriptModel(api, target, (error) => {
    options.onOpeningFailure(error);
  });
  const initialize = Atom.make((get) => {
    get.mount(model.scope);
    get.mount(model.failure);
    get.mount(model.replacement);
    get.set(model.failure, (error) =>
      Effect.sync(() => {
        options.onContractFailure(error);
      }),
    );
    get.set(model.replacement, () => {
      options.onScratchRehydration();
    });
    return null;
  });
  const receive = Atom.fn<{
    kind: "initial" | "scratch" | "reattachment";
    hydration: ChatTranscriptPayloadByKind["hydration"];
  }>()(
    (input, get) =>
      get(model.scope).apply({ kind: `${input.kind}-hydration`, hydration: input.hydration }, "content"),
    { concurrent: true },
  );
  const open = Atom.fn((_, get) =>
    Effect.sync(() => {
      get.mount(model.mount);
    }),
  );
  return renderHook(
    () => {
      useAtomMount(initialize);
      useAtomMount(receive);
      return {
        snapshot: useAtomValue(model.snapshot),
        presentation: useAtomValue(model.presentation),
        open: useAtomSet(open),
        receive: useAtomSet(receive),
        dispatch: useAtomSet(model.dispatch),
      };
    },
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
}

export class TranscriptHarness {
  readonly view: ReturnType<typeof mountTranscript>;
  constructor(...args: Parameters<typeof mountTranscript>) {
    this.view = mountTranscript(...args);
  }
  open() {
    act(() => {
      this.view.result.current.open(undefined);
    });
  }
  get snapshot() {
    return this.view.result.current.snapshot;
  }
  get presentationUpdate() {
    return this.view.result.current.presentation;
  }
  hydration(
    kind: "initial" | "scratch" | "reattachment",
    hydration: ChatTranscriptPayloadByKind["hydration"],
  ) {
    act(() => {
      this.view.result.current.receive({ kind, hydration });
    });
  }
  dispatch(input: Parameters<ReturnType<typeof mountTranscript>["result"]["current"]["dispatch"]>[0]) {
    act(() => {
      this.view.result.current.dispatch(input);
    });
  }
  dispose() {
    this.view.unmount();
  }
}
