import type {
  ApiConnectionSource,
  ChatApi,
  ChatSessionTarget,
  PendingPrompt,
  PromptAnswerBatchEntryInput,
  PromptAnswerBatchInput,
} from "@/api";
import { promptAnswerEntry } from "@/api";
import type { ChatRuntimeOwner } from "@/app-facade";
import {
  emptyPickerState,
  pickerBatch,
  transitionPicker,
  type PickerAction,
  type PickerDraft,
  type PickerState,
} from "./promptPickerState";

type PickerApi = Pick<ChatApi, "answerPromptBatch" | "listPendingPrompts">;
export type PromptPickerSnapshot = Readonly<{ state: PickerState; isPending: boolean }>;

export class PromptPickerController {
  readonly #owner: ChatRuntimeOwner;
  readonly #api: PickerApi;
  readonly #target: ChatSessionTarget;
  readonly #onError: (error: unknown) => void;
  readonly #connection: ApiConnectionSource;
  readonly #listeners = new Set<() => void>();
  #unsubscribe: (() => void) | null = null;
  #request: PromptAnswerBatchInput | null = null;
  #snapshot: PromptPickerSnapshot = { state: emptyPickerState(), isPending: false };

  constructor(
    owner: ChatRuntimeOwner,
    api: PickerApi,
    target: ChatSessionTarget,
    host: Readonly<{ onError(error: unknown): void; connection: ApiConnectionSource }>,
  ) {
    this.#owner = owner;
    this.#api = api;
    this.#target = target;
    this.#onError = host.onError;
    this.#connection = host.connection;
  }

  get snapshot(): PromptPickerSnapshot {
    return this.#snapshot;
  }

  subscribe = (listener: () => void): (() => void) => {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  };

  mount(): () => void {
    const unsubscribeOwner = this.#owner.subscribe(() => {
      this.#sync();
    });
    const unsubscribeConnection = this.#connection.subscribe(() => {
      this.#sync();
    });
    this.#unsubscribe = () => {
      unsubscribeOwner();
      unsubscribeConnection();
    };
    this.#sync();
    return () => {
      this.dispose();
    };
  }

  dispose(): void {
    this.#unsubscribe?.();
    this.#unsubscribe = null;
    this.#request = null;
    this.#update(emptyPickerState());
  }

  dispatch(action: PickerAction): "none" | "focus-field" {
    if (this.#unsubscribe === null || this.#owner.snapshot.disposed) return "none";
    if (this.#connection.snapshot().phase !== "connected") return "none";
    if (this.#request !== null && action.kind !== "navigate" && action.kind !== "sync") return "none";
    const transition = transitionPicker(this.#snapshot.state, this.#owner.snapshot.pendingPrompts, action);
    this.#update(transition.state);
    if (transition.effect === "submit") {
      const batch = pickerBatch(this.#owner.snapshot.pendingPrompts);
      const first = batch[0];
      if (first === undefined) return "none";
      const request: PromptAnswerBatchInput = {
        sessionID: first.sessionID,
        stepID: first.stepID,
        entries: batch.map((prompt) => answer(prompt, transition.state.drafts.get(prompt.toolCallID))),
      };
      this.#request = request;
      this.#update(transition.state);
      void this.#submit(request);
      return "none";
    }
    return transition.effect;
  }

  #sync(): void {
    if (this.#owner.snapshot.disposed) {
      this.dispose();
      return;
    }
    if (this.#connection.snapshot().phase !== "connected") {
      this.#request = null;
      this.#update(emptyPickerState());
      return;
    }
    this.#update(
      transitionPicker(this.#snapshot.state, this.#owner.snapshot.pendingPrompts, { kind: "sync" }).state,
    );
  }

  #update(state: PickerState): void {
    this.#snapshot = { state, isPending: this.#request !== null };
    for (const listener of this.#listeners) listener();
  }

  async #submit(request: PromptAnswerBatchInput): Promise<void> {
    try {
      const response = await this.#api.answerPromptBatch(request);
      if (this.#request !== request) return;
      this.#owner.resolvePendingPrompts(new Set(response.results.map((result) => result.toolCallID)));
    } catch (error) {
      if (this.#request === request) {
        this.#onError(error);
        try {
          const prompts = await this.#api.listPendingPrompts(this.#target);
          if (this.#request === request && this.#owner.snapshot.pendingPrompts[0]?.stepID === request.stepID)
            this.#owner.replacePendingPrompts(prompts);
        } catch (refreshError) {
          if (this.#request === request) this.#onError(refreshError);
        }
      }
    } finally {
      if (this.#request === request) {
        this.#request = null;
        this.#sync();
      }
    }
  }
}

function answer(prompt: PendingPrompt, draft: PickerDraft | undefined): PromptAnswerBatchEntryInput {
  if (draft === undefined || draft.status === "tentative")
    throw new Error("Cannot submit an unfinished prompt.");
  if (draft.status === "declined") return { kind: "declined", toolCallID: prompt.toolCallID };
  const identity = { toolCallID: prompt.toolCallID, sessionID: prompt.sessionID, stepID: prompt.stepID };
  switch (draft.selection.kind) {
    case "approval":
      return promptAnswerEntry({
        ...identity,
        kind: "approval",
        decision: draft.selection.decision,
        commentary: draft.commentary,
      });
    case "suggested":
    case "neither":
    case "freeform":
      return promptAnswerEntry({
        ...identity,
        kind: "ordinary",
        selectedOptionNumber: draft.selection.kind === "suggested" ? draft.selection.number : null,
        freeformAnswer: draft.commentary,
      });
    case "none":
      throw new Error("Cannot submit a prompt without an answer.");
  }
}
