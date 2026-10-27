import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/reactivity/Atom";
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
  interactionRestricted,
}: Readonly<{
  owner: ChatRuntimeOwner;
  client: QueryClient;
  api: Pick<ChatApi, "answerPromptBatch">;
  onError(error: unknown): void;
  interactionRestricted?: Atom.Atom<boolean>;
}>) {
  const runtime = owner.snapshot;
  const state = Atom.writable(
    (get): PickerState => {
      const snapshot = get(runtime);
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
  const requests = Atom.make((get) => {
    const observer = new MutationObserver(client, {
      retry: false,
      networkMode: "always",
      mutationFn: async (request: PromptAnswerBatchInput) => api.answerPromptBatch(request),
      onSuccess: (response, variables) => {
        if (Option.isNone(get.self())) return;
        const submitted = new Set(variables.entries.map((entry) => entry.toolCallID));
        const applicable = new Set(
          get
            .once(runtime)
            .pendingPrompts.filter(
              (prompt) =>
                prompt.sessionID === variables.sessionID &&
                prompt.stepID === variables.stepID &&
                submitted.has(prompt.toolCallID),
            )
            .map((prompt) => prompt.toolCallID),
        );
        get.set(
          owner.resolvePendingPrompts,
          new Set(
            response.results
              .filter((result) => applicable.has(result.toolCallID))
              .map((result) => result.toolCallID),
          ),
        );
      },
      onError,
    });
    return { observer, request: queryAtom(observer) };
  });
  const request = Atom.make((get) => get(get(requests).request));
  const dispatch = Atom.fn<{ action: PickerAction; focusField?: () => void }>()(
    (input, get) =>
      Effect.gen(function* () {
        const { observer } = get(requests);
        if (interactionRestricted !== undefined && get(interactionRestricted) && input.action.kind !== "sync")
          return;
        if (
          observer.getCurrentResult().isPending &&
          input.action.kind !== "navigate" &&
          input.action.kind !== "sync"
        )
          return;
        const transition = transitionPicker(get(state), get(runtime).pendingPrompts, input.action);
        get.set(state, transition.state);
        if (transition.effect === "focus-field") input.focusField?.();
        if (transition.effect !== "submit") return;
        const batch = pickerBatch(get(runtime).pendingPrompts);
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
