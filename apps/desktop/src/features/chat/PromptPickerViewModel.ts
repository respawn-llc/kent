import { useAtomMount, useAtomSet } from "@effect/atom-react";
import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import * as Option from "effect/Option";
import type { ChatApi, PromptAnswerBatchInput } from "@/api";
import { queryAtom, type ChatRuntimeOwner } from "@/app-facade";
import {
  emptyPickerState,
  pickerBatch,
  transitionPicker,
  type PickerAction,
  type PickerState,
} from "./promptPickerState";
import { pickerAnswer } from "./pickerAnswer";

export function createPromptPickerViewModel({
  owner,
  client,
  api,
  onError,
}: Readonly<{
  owner: ChatRuntimeOwner;
  client: QueryClient;
  api: Pick<ChatApi, "answerPromptBatch">;
  onError(error: unknown): void;
}>) {
  const runtime = Atom.make((get) => {
    get.addFinalizer(
      owner.subscribe(() => {
        get.setSelf(owner.snapshot);
      }),
    );
    return owner.snapshot;
  });
  const state = Atom.writable(
    (get): PickerState => {
      const snapshot = get(runtime);
      if (snapshot.disposed) return emptyPickerState();
      return transitionPicker(
        Option.getOrElse(get.self<PickerState>(), emptyPickerState),
        snapshot.pendingPrompts,
        { kind: "sync" },
      ).state;
    },
    (get, value: PickerState) => {
      get.setSelf(value);
    },
  );
  const observer = new MutationObserver(client, {
    retry: false,
    networkMode: "always",
    mutationFn: async (request: PromptAnswerBatchInput) => api.answerPromptBatch(request),
    onSuccess: (response, request) => {
      if (owner.snapshot.disposed) return;
      const submitted = new Set(request.entries.map((entry) => entry.toolCallID));
      const applicable = new Set(
        owner.snapshot.pendingPrompts
          .filter(
            (prompt) =>
              prompt.sessionID === request.sessionID &&
              prompt.stepID === request.stepID &&
              submitted.has(prompt.toolCallID),
          )
          .map((prompt) => prompt.toolCallID),
      );
      owner.resolvePendingPrompts(
        new Set(
          response.results
            .filter((result) => applicable.has(result.toolCallID))
            .map((result) => result.toolCallID),
        ),
      );
    },
    onError,
  });
  const request = queryAtom(observer);
  const dispatch = Atom.fn<{ action: PickerAction; focusField?: () => void }>()(
    (input, get) =>
      Effect.gen(function* () {
        if (owner.snapshot.disposed) return;
        if (
          observer.getCurrentResult().isPending &&
          input.action.kind !== "navigate" &&
          input.action.kind !== "sync"
        )
          return;
        const transition = transitionPicker(get(state), owner.snapshot.pendingPrompts, input.action);
        get.set(state, transition.state);
        if (transition.effect === "focus-field") input.focusField?.();
        if (transition.effect !== "submit") return;
        const batch = pickerBatch(owner.snapshot.pendingPrompts);
        const first = batch[0];
        if (first === undefined) return;
        const variables: PromptAnswerBatchInput = {
          sessionID: first.sessionID,
          stepID: first.stepID,
          entries: batch.map((prompt) =>
            pickerAnswer(prompt, transition.state.drafts.get(prompt.toolCallID)),
          ),
        };
        yield* Effect.tryPromise(async () => observer.mutate(variables)).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const observation: Atom.Atom<PickerState> = state;
  const prompts = Atom.make((get) => get(runtime).pendingPrompts);
  return { state: observation, request, dispatch, prompts } as const;
}

export type PromptPickerViewModel = ReturnType<typeof createPromptPickerViewModel>;

export function usePromptPickerActions(model: PromptPickerViewModel) {
  useAtomMount(model.request);
  useAtomMount(model.state);
  return { dispatch: useAtomSet(model.dispatch) };
}
