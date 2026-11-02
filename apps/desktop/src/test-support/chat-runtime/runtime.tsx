import { RegistryProvider, useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { createChatRuntimeModel } from "@/app-facade";

function mountRuntime(model: ReturnType<typeof createChatRuntimeModel>) {
  return renderHook(
    () => {
      useAtomMount(model.mount);
      return {
        snapshot: useAtomValue(model.snapshot),
        refresh: useAtomSet(model.forceMainViewRead),
        retry: useAtomSet(model.retryTranscriptObservation),
        replace: useAtomSet(model.replacePendingPrompts),
        resolve: useAtomSet(model.resolvePendingPrompts),
        dispatch: useAtomSet(model.transcript.dispatch),
        jump: useAtomSet(model.transcript.jump),
      };
    },
    { wrapper: ({ children }: { children: ReactNode }) => <RegistryProvider>{children}</RegistryProvider> },
  );
}

export class RuntimeHarness {
  readonly model: ReturnType<typeof createChatRuntimeModel>;
  #view: ReturnType<typeof mountRuntime> | undefined;
  constructor(...args: Parameters<typeof createChatRuntimeModel>) {
    this.model = createChatRuntimeModel(...args);
  }
  start() {
    this.#view = mountRuntime(this.model);
  }
  get current() {
    if (this.#view === undefined) throw new Error("Runtime must be mounted");
    return this.#view.result.current;
  }
  get snapshot() {
    return this.current.snapshot;
  }
  forceMainViewRead() {
    act(() => {
      this.current.refresh(undefined);
    });
  }
  retryMainView() {
    this.forceMainViewRead();
  }
  retryTranscriptObservation() {
    act(() => {
      this.current.retry(undefined);
    });
  }
  replacePendingPrompts(
    prompts: Parameters<ReturnType<typeof mountRuntime>["result"]["current"]["replace"]>[0],
  ) {
    act(() => {
      this.current.replace(prompts);
    });
  }
  resolvePendingPrompts(ids: ReadonlySet<string>) {
    act(() => {
      this.current.resolve(ids);
    });
  }
  readonly transcript = {
    dispatch: (input: Parameters<ReturnType<typeof mountRuntime>["result"]["current"]["dispatch"]>[0]) => {
      act(() => {
        this.current.dispatch(input);
      });
    },
    jump: (input: Parameters<ReturnType<typeof mountRuntime>["result"]["current"]["jump"]>[0]) => {
      act(() => {
        this.current.jump(input);
      });
    },
  };
  dispose() {
    this.#view?.unmount();
  }
}
