import type {
  ChatApi,
  ChatSessionTarget,
  ChatTranscriptMessage,
  ChatTranscriptPage,
  ChatTranscriptPayloadByKind,
} from "@/api";
import { ContractError } from "@/api";
import {
  TranscriptWindow,
  type TranscriptLiveFact,
  type TranscriptWindowInput,
  type TranscriptWindowResult,
  type TranscriptWindowSnapshot,
} from "./transcript-window";

export type ChatTranscriptPageInput =
  Readonly<{ kind: "newest" }> | Readonly<{ kind: "older" | "newer"; cursor: number }>;
export type ChatTranscriptHostInput = Extract<
  TranscriptWindowInput,
  { kind: "edge-visit" | "opening-retry" | "retry" }
>;
export type ChatTranscriptHostOptions = Readonly<{
  onContractFailure(error: Error): void;
  onOpeningFailure(error: Error): void;
  onScratchRehydration(): void;
}>;
export type ChatTranscriptAdmission =
  Readonly<{ kind: "accepted" }> | Readonly<{ kind: "rejected"; error: Error }>;

type TranscriptPageApi = Pick<ChatApi, "getTranscriptPage">;
export type ChatTailOutcome =
  Readonly<{ kind: "tail-ready" | "destination-ended" }> | Readonly<{ kind: "failed"; error: Error }>;
export type ChatTranscriptPresentationUpdate = Readonly<{ kind: "content" | "membership" }>;

export async function executeChatTranscriptPage(
  api: TranscriptPageApi,
  target: ChatSessionTarget,
  input: ChatTranscriptPageInput,
): Promise<ChatTranscriptPage> {
  switch (input.kind) {
    case "newest":
      return api.getTranscriptPage(target);
    case "older":
    case "newer":
      return api.getTranscriptPage(target, {
        direction: input.kind,
        value: input.cursor,
      });
  }
}

export class ChatTranscriptHost {
  readonly #api: TranscriptPageApi;
  readonly #target: ChatSessionTarget;
  readonly #options: ChatTranscriptHostOptions;
  readonly #window = new TranscriptWindow();
  readonly #listeners = new Set<() => void>();
  #pendingJump: ((outcome: ChatTailOutcome) => void) | null = null;
  #presentationUpdate: ChatTranscriptPresentationUpdate = { kind: "membership" };

  get presentationUpdate(): ChatTranscriptPresentationUpdate {
    return this.#presentationUpdate;
  }

  get jumpPending(): boolean {
    return this.#pendingJump !== null;
  }

  async jumpToLatest(): Promise<ChatTailOutcome> {
    if (this.snapshot.opening.kind === "disposed") {
      return Promise.resolve({ kind: "destination-ended" });
    }
    if (this.snapshot.newer.cursor === null) return Promise.resolve({ kind: "tail-ready" });
    return new Promise((resolve) => {
      this.#pendingJump = resolve;
      this.#notify();
      void executeChatTranscriptPage(this.#api, this.#target, { kind: "newest" }).then(
        (page) => {
          if (this.#pendingJump !== resolve) return;
          const admission = this.#apply(this.#window.dispatch({ kind: "replace-window", page }));
          if (admission.kind === "rejected") {
            this.#finishJump({ kind: "destination-ended" });
            this.#options.onContractFailure(admission.error);
            return;
          }
          this.#finishJump({ kind: "tail-ready" });
        },
        (cause: unknown) => {
          if (this.#pendingJump !== resolve) return;
          if (cause instanceof ContractError) {
            this.#finishJump({ kind: "destination-ended" });
            this.#options.onContractFailure(cause);
            return;
          }
          this.#finishJump({
            kind: "failed",
            error: cause instanceof Error ? cause : new Error("Transcript page failed."),
          });
        },
      );
    });
  }

  #finishJump(outcome: ChatTailOutcome): void {
    const complete = this.#pendingJump;
    this.#pendingJump = null;
    complete?.(outcome);
    this.#notify();
  }

  constructor(api: TranscriptPageApi, target: ChatSessionTarget, options: ChatTranscriptHostOptions) {
    this.#api = api;
    this.#target = target;
    this.#options = options;
  }

  get snapshot(): TranscriptWindowSnapshot {
    return this.#window.snapshot;
  }

  subscribe(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  open(): void {
    this.#requestOpeningPage(this.#window.openingPermit);
  }

  #requestOpeningPage(permit: symbol): void {
    void executeChatTranscriptPage(this.#api, this.#target, { kind: "newest" }).then(
      (page) => {
        this.#applyAutonomous(this.#window.dispatch({ kind: "opening-success", permit, page }));
      },
      (cause: unknown) => {
        const error = cause instanceof Error ? cause : new Error("Transcript page failed.");
        this.#applyAutonomous(this.#window.dispatch({ kind: "opening-failure", permit, error }));
      },
    );
  }

  hydration(
    kind: "initial" | "scratch" | "reattachment",
    hydration: ChatTranscriptPayloadByKind["hydration"],
  ): ChatTranscriptAdmission {
    const result = this.#window.dispatch({ kind: `${kind}-hydration`, hydration });
    return this.#apply(
      result,
      result.effects.some((effect) => effect.kind === "hydration-replaced") ? "membership" : "content",
    );
  }

  event(event: Exclude<ChatTranscriptMessage, { kind: "hydration" }>): ChatTranscriptAdmission {
    if (event.kind === "committed_row") {
      return this.#apply(this.#window.dispatch({ kind: "committed-row", row: event.payload }), "content");
    }
    if (event.kind === "runtime_read_model_update") {
      return this.#apply(
        this.#window.dispatch({ kind: "runtime-activity", activity: event.payload.Activity }),
        "content",
      );
    }
    if (event.kind === "compaction_status") {
      return this.#apply(
        this.#window.dispatch({ kind: "compaction-status", status: event.payload }),
        "content",
      );
    }
    if (event.kind === "assistant_delta") {
      return this.#admitLive(event);
    }
    if (event.kind === "assistant_stream_abort") {
      return this.#admitLive(event);
    }
    if (event.kind === "tool_start") {
      return this.#admitLive(event);
    }
    if (event.kind === "tool_abort") {
      return this.#admitLive(event);
    }
    if (event.kind === "reasoning_trace_update") {
      return this.#admitLive(event);
    }
    if (event.kind === "reasoning_trace_reset") {
      return this.#admitLive(event);
    }
    if (event.kind === "thinking_status_update") {
      return this.#admitLive(event);
    }
    if (event.kind === "step_state") {
      return this.#admitLive(event);
    }
    return { kind: "accepted" };
  }

  observationLost(): void {
    this.#applyAutonomous(this.#window.dispatch({ kind: "observation-loss" }));
  }

  dispatch(input: ChatTranscriptHostInput): void {
    this.#applyAutonomous(this.#window.dispatch(input));
  }

  dispose(): void {
    this.#finishJump({ kind: "destination-ended" });
    this.#applyAutonomous(this.#window.dispatch({ kind: "dispose" }));
    this.#listeners.clear();
  }

  #admitLive(fact: TranscriptLiveFact): ChatTranscriptAdmission {
    return this.#apply(this.#window.dispatch({ kind: "live-fact", fact }), "content");
  }

  #applyAutonomous(result: TranscriptWindowResult): void {
    const admission = this.#apply(result);
    if (admission.kind === "rejected") this.#options.onContractFailure(admission.error);
  }

  #apply(
    result: TranscriptWindowResult,
    kind: ChatTranscriptPresentationUpdate["kind"] = "membership",
  ): ChatTranscriptAdmission {
    if (result.kind === "contract-failure") {
      return { kind: "rejected", error: result.error };
    }
    if (result.kind === "accepted") this.#presentationUpdate = { kind };
    this.#notify();
    for (const effect of result.effects) {
      switch (effect.kind) {
        case "hydration-replaced":
          this.#finishJump({ kind: "tail-ready" });
          break;
        case "opening-failed":
          this.#options.onOpeningFailure(effect.error);
          break;
        case "opening-page-request":
          this.#requestOpeningPage(effect.permit);
          break;
        case "scratch-rehydration":
          this.#options.onScratchRehydration();
          break;
        case "page-request":
          void executeChatTranscriptPage(this.#api, this.#target, {
            kind: effect.request.direction,
            cursor: effect.request.cursor,
          }).then(
            (page) => {
              this.#applyAutonomous(
                this.#window.dispatch({
                  kind: "page-success",
                  request: effect.request,
                  page,
                }),
              );
            },
            (cause: unknown) => {
              const error = cause instanceof Error ? cause : new Error("Transcript page failed.");
              this.#applyAutonomous(
                this.#window.dispatch({
                  kind: "page-failure",
                  request: effect.request,
                  error,
                }),
              );
            },
          );
          break;
      }
    }
    return { kind: "accepted" };
  }

  #notify(): void {
    for (const listener of this.#listeners) listener();
  }
}
