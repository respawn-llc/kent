import type {
  ChatApi,
  ChatSessionTarget,
  ChatTranscriptMessage,
  ChatTranscriptPage,
  ChatTranscriptPayloadByKind,
} from "@/api";
import {
  TranscriptWindow,
  type TranscriptLiveFact,
  type TranscriptWindowInput,
  type TranscriptWindowResult,
  type TranscriptWindowSnapshot,
} from "./transcript-window";

export type ChatTranscriptPageInput =
  Readonly<{ kind: "newest" }> | Readonly<{ kind: "older" | "newer"; cursor: number }>;
export type ChatTranscriptHostInput = Extract<TranscriptWindowInput, { kind: "edge-visit" | "retry" }>;
export type ChatTranscriptHostOptions = Readonly<{
  onContractFailure(error: Error): void;
  onOpeningFailure(error: Error): void;
  onScratchRehydration(): void;
}>;

type TranscriptPageApi = Pick<ChatApi, "getTranscriptPage">;

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
    const permit = this.#window.openingPermit;
    void executeChatTranscriptPage(this.#api, this.#target, { kind: "newest" }).then(
      (page) => {
        this.#apply(this.#window.dispatch({ kind: "opening-success", permit, page }));
      },
      (cause: unknown) => {
        const error = cause instanceof Error ? cause : new Error("Transcript page failed.");
        this.#apply(this.#window.dispatch({ kind: "opening-failure", permit, error }));
      },
    );
  }

  hydration(
    kind: "initial" | "scratch" | "reattachment",
    hydration: ChatTranscriptPayloadByKind["hydration"],
  ): void {
    this.#apply(this.#window.dispatch({ kind: `${kind}-hydration`, hydration }));
  }

  event(event: Exclude<ChatTranscriptMessage, { kind: "hydration" }>): void {
    if (event.kind === "committed_row") {
      this.#apply(this.#window.dispatch({ kind: "committed-row", row: event.payload }));
      return;
    }
    if (event.kind === "runtime_read_model_update") {
      this.#apply(this.#window.dispatch({ kind: "runtime-activity", activity: event.payload.Activity }));
      return;
    }
    if (event.kind === "compaction_status") {
      this.#apply(this.#window.dispatch({ kind: "compaction-status", status: event.payload }));
      return;
    }
    if (event.kind === "assistant_delta") {
      this.#admitLive(event);
      return;
    }
    if (event.kind === "assistant_stream_abort") {
      this.#admitLive(event);
      return;
    }
    if (event.kind === "tool_start") {
      this.#admitLive(event);
      return;
    }
    if (event.kind === "tool_abort") {
      this.#admitLive(event);
      return;
    }
    if (event.kind === "reasoning_trace_update") {
      this.#admitLive(event);
      return;
    }
    if (event.kind === "reasoning_trace_reset") {
      this.#admitLive(event);
      return;
    }
    if (event.kind === "thinking_status_update") {
      this.#admitLive(event);
      return;
    }
    if (event.kind === "step_state") {
      this.#admitLive(event);
      return;
    }
  }

  recoveryStarted(): void {
    this.#apply(this.#window.dispatch({ kind: "recovery-begin" }));
  }

  dispatch(input: ChatTranscriptHostInput): void {
    this.#apply(this.#window.dispatch(input));
  }

  dispose(): void {
    this.#apply(this.#window.dispatch({ kind: "dispose" }));
    this.#listeners.clear();
  }

  #admitLive(fact: TranscriptLiveFact): void {
    this.#apply(this.#window.dispatch({ kind: "live-fact", fact }));
  }

  #apply(result: TranscriptWindowResult): void {
    if (result.kind === "contract-failure") {
      this.#options.onContractFailure(result.error);
      return;
    }
    this.#notify();
    for (const effect of result.effects) {
      switch (effect.kind) {
        case "opening-failed":
          this.#options.onOpeningFailure(effect.error);
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
              this.#apply(
                this.#window.dispatch({
                  kind: "page-success",
                  request: effect.request,
                  page,
                }),
              );
            },
            (cause: unknown) => {
              const error = cause instanceof Error ? cause : new Error("Transcript page failed.");
              this.#apply(
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
  }

  #notify(): void {
    for (const listener of this.#listeners) listener();
  }
}
