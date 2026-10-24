import type {
  ApiSubscription,
  ChatApi,
  ChatSessionTarget,
  ChatTranscriptHandler,
  ChatTranscriptMessage,
  ChatTranscriptPayloadByKind,
} from "@/api";
import { ContractError, TransportError } from "@/api";
import { completesNativeCall } from "./transcript-window/segments";

type TranscriptSubscriber = Pick<ChatApi, "subscribeTranscript">;

export class ChatTranscriptPhysicalObservation {
  readonly #api: TranscriptSubscriber;
  readonly #target: ChatSessionTarget;
  readonly #handler: ChatTranscriptHandler;
  #subscription: ApiSubscription | null = null;
  #generation = 0;
  #started = false;
  #closed = false;

  constructor(api: TranscriptSubscriber, target: ChatSessionTarget, handler: ChatTranscriptHandler) {
    this.#api = api;
    this.#target = target;
    this.#handler = handler;
  }

  start(): void {
    if (this.#started || this.#closed) return;
    this.#started = true;
    this.#openPhysicalSubscription();
  }

  close(): void {
    if (this.#closed) return;
    this.#closed = true;
    this.#generation++;
    this.#subscription?.close();
    this.#subscription = null;
  }

  #openPhysicalSubscription(): void {
    if (this.#closed) return;
    const generation = ++this.#generation;
    let settled = false;
    let terminalUnavailable = false;
    this.#subscription = this.#api.subscribeTranscript(this.#target, {
      ...(this.#handler.onOpen === undefined
        ? {}
        : {
            onOpen: () => {
              if (this.#accepts(generation, settled)) this.#handler.onOpen?.();
            },
          }),
      onEvent: (event) => {
        if (!this.#accepts(generation, settled)) return;
        terminalUnavailable ||= isTerminalRuntimeUnavailable(event);
        this.#handler.onEvent(event);
      },
      ...(this.#handler.onTransportLoss === undefined
        ? {}
        : {
            onTransportLoss: () => {
              if (this.#accepts(generation, settled)) this.#handler.onTransportLoss?.();
            },
          }),
      onComplete: (completion) => {
        if (!this.#accepts(generation, settled)) return;
        settled = true;
        this.#subscription = null;
        if (completion.code === null && terminalUnavailable) {
          this.#openPhysicalSubscription();
          return;
        }
        this.#handler.onComplete(completion);
      },
      onError: (error) => {
        if (!this.#accepts(generation, settled)) return;
        settled = true;
        const subscription = this.#subscription;
        try {
          this.#handler.onError(error);
        } finally {
          if (this.#subscription === subscription) {
            subscription?.close();
            this.#subscription = null;
          }
        }
      },
    });
  }

  #accepts(generation: number, settled: boolean): boolean {
    return !this.#closed && !settled && generation === this.#generation;
  }
}

function isTerminalRuntimeUnavailable(event: ChatTranscriptMessage): boolean {
  return event.kind === "runtime_read_model_update" && event.payload.Activity.State === "unavailable";
}

export type ChatTranscriptHydrationKind = "initial" | "scratch" | "reattachment";
export type ChatTranscriptObservationState =
  Readonly<{ kind: "loading" | "observing" | "disposed" }> | Readonly<{ kind: "error"; error: Error }>;
export type ChatTranscriptObservationHost = Readonly<{
  onHydration(kind: ChatTranscriptHydrationKind, hydration: ChatTranscriptPayloadByKind["hydration"]): void;
  onEvent(event: Exclude<ChatTranscriptMessage, { kind: "hydration" }>): void;
  onIntegrityFailure?(error: Error): void;
  onObservationLoss?(): void;
  onError(error: Error): void;
  onStateChange?(): void;
}>;

type CommittedRowLocator = Readonly<{ eventSequence: number; rowOrdinal: number }>;

export class ChatTranscriptObservation {
  readonly #api: TranscriptSubscriber;
  readonly #target: ChatSessionTarget;
  readonly #host: ChatTranscriptObservationHost;
  #physical: ChatTranscriptPhysicalObservation | null = null;
  #state: ChatTranscriptObservationState = { kind: "loading" };
  #hydrationKind: ChatTranscriptHydrationKind = "initial";
  #nextSequence = 0;
  #hydratedCommittedEventSequence: number | null = null;
  #lastLiveCommittedLocator: CommittedRowLocator | null = null;
  #nativeToolCalls = new Map<string, ChatTranscriptPayloadByKind["committed_row"]>();
  #hasHydrated = false;
  #disposed = false;

  constructor(api: TranscriptSubscriber, target: ChatSessionTarget, host: ChatTranscriptObservationHost) {
    this.#api = api;
    this.#target = target;
    this.#host = host;
  }

  get state(): ChatTranscriptObservationState {
    return this.#state;
  }

  start(): void {
    if (this.#disposed || this.#physical !== null) return;
    this.#openPhysical();
  }

  rehydrateAfterCompaction(): void {
    this.#replaceObservation();
  }

  retry(): void {
    if (this.#state.kind !== "error") return;
    this.#replaceObservation();
  }

  rejectIntegrity(error: Error): void {
    this.#integrityFailure(error);
  }

  close(): void {
    if (this.#disposed) return;
    this.#disposed = true;
    this.#physical?.close();
    this.#physical = null;
    this.#state = { kind: "disposed" };
    this.#host.onStateChange?.();
  }

  #openPhysical(): void {
    if (this.#disposed) return;
    this.#physical = new ChatTranscriptPhysicalObservation(this.#api, this.#target, {
      onOpen: () => {
        if (this.#disposed) return;
        if (this.#hasHydrated && this.#hydrationKind !== "scratch") {
          this.#hydrationKind = "reattachment";
        }
        this.#nextSequence = 0;
        this.#hydratedCommittedEventSequence = null;
        this.#lastLiveCommittedLocator = null;
        this.#nativeToolCalls.clear();
      },
      onEvent: (event) => {
        this.#admit(event);
      },
      onTransportLoss: () => {
        this.#continuityFailure(new TransportError("Transcript observation lost its transport."));
      },
      onComplete: (completion) => {
        this.#continuityFailure(
          new ContractError(
            `Transcript observation completed with code ${String(completion.code)}: ${String(completion.message)}`,
          ),
        );
      },
      onError: (error) => {
        if (error instanceof ContractError) {
          this.#integrityFailure(error);
        } else {
          this.#continuityFailure(error);
        }
      },
    });
    this.#physical.start();
  }

  #admit(event: ChatTranscriptMessage): void {
    if (this.#disposed) return;
    if (this.#nextSequence === 0) {
      this.#admitHydration(event);
      return;
    }
    this.#admitUpdate(event);
  }

  #admitHydration(event: ChatTranscriptMessage): void {
    if (event.kind !== "hydration" || event.sequence !== 1) {
      this.#integrityFailure(
        new ContractError("Transcript observation must begin with sequence-1 hydration."),
      );
      return;
    }
    try {
      this.#host.onHydration(this.#hydrationKind, event.payload);
    } catch (error) {
      this.#integrityFailure(
        error instanceof Error ? error : new ContractError("Transcript hydration admission failed."),
      );
      return;
    }
    this.#hydratedCommittedEventSequence =
      event.payload.TailSegment.Entries.at(-1)?.Locator.event_sequence ?? null;
    this.#lastLiveCommittedLocator = null;
    for (const row of event.payload.TailSegment.Entries) {
      if (row.ToolCall !== null && row.Visibility !== "hidden") {
        this.#nativeToolCalls.set(row.ToolCall.ToolCallID, row);
      }
    }
    this.#nextSequence = 1;
    this.#hydrationKind = "initial";
    this.#hasHydrated = true;
    this.#state = { kind: "observing" };
    this.#host.onStateChange?.();
  }

  #admitUpdate(event: ChatTranscriptMessage): void {
    if (event.kind === "hydration" || event.sequence !== this.#nextSequence + 1) {
      this.#integrityFailure(new ContractError("Transcript observation sequence is not continuous."));
      return;
    }
    let committedLocator: CommittedRowLocator | null = null;
    try {
      if (event.kind === "committed_row") {
        committedLocator = this.#admitCommittedRow(event.payload);
      } else if (event.kind === "tool_abort") {
        this.#nativeToolCalls.delete(event.payload.ToolCallID);
      } else if (event.kind === "step_state" && event.payload.Lifecycle === "finished") {
        this.#forgetNativeToolCallsForStep(event.payload.StepID);
      }
      this.#host.onEvent(event);
    } catch (error) {
      this.#integrityFailure(
        error instanceof Error ? error : new ContractError("Transcript event admission failed."),
      );
      return;
    }
    if (committedLocator !== null) this.#lastLiveCommittedLocator = committedLocator;
    this.#nextSequence = event.sequence;
  }

  #admitCommittedRow(row: ChatTranscriptPayloadByKind["committed_row"]): CommittedRowLocator | null {
    const resultID = row.Tool?.ToolCallID;
    const source = resultID == null ? undefined : this.#nativeToolCalls.get(resultID);
    if (resultID != null && source !== undefined) {
      if (!completesNativeCall(source, row))
        throw new ContractError("Native result does not match its accepted call source.");
      this.#nativeToolCalls.delete(resultID);
      return null;
    }
    const locator = this.#nextCommittedRowLocator(row.Locator);
    if (row.ToolCall !== null) {
      if (this.#nativeToolCalls.has(row.ToolCall.ToolCallID))
        throw new ContractError("Native call repeats a pending identity.");
      this.#nativeToolCalls.set(row.ToolCall.ToolCallID, row);
    }
    return locator;
  }

  #forgetNativeToolCallsForStep(stepID: string): void {
    for (const [id, row] of this.#nativeToolCalls) {
      if (row.ToolCall?.StepID === stepID) this.#nativeToolCalls.delete(id);
    }
  }

  #nextCommittedRowLocator(
    locator: ChatTranscriptPayloadByKind["committed_row"]["Locator"],
  ): CommittedRowLocator {
    const next = {
      eventSequence: locator.event_sequence,
      rowOrdinal: locator.row_ordinal,
    };
    const previous = this.#lastLiveCommittedLocator;
    if (previous === null) {
      if (
        (this.#hydratedCommittedEventSequence !== null &&
          next.eventSequence <= this.#hydratedCommittedEventSequence) ||
        next.rowOrdinal !== 1
      ) {
        throw new ContractError("Live committed transcript rows must advance beyond hydration.");
      }
      return next;
    }
    if (
      next.eventSequence < previous.eventSequence ||
      (next.eventSequence === previous.eventSequence && next.rowOrdinal !== previous.rowOrdinal + 1) ||
      (next.eventSequence > previous.eventSequence && next.rowOrdinal !== 1)
    ) {
      throw new ContractError("Live committed transcript row locators are not continuous.");
    }
    return next;
  }

  #continuityFailure(error: Error): void {
    if (this.#disposed) return;
    this.#settleFailure(error);
    this.#host.onError(error);
  }

  #integrityFailure(error: Error): void {
    if (this.#disposed || this.#physical === null) return;
    this.#settleFailure(error);
    if (this.#host.onIntegrityFailure === undefined) {
      this.#host.onError(error);
      return;
    }
    this.#host.onIntegrityFailure(error);
  }

  #settleFailure(error: Error): void {
    this.#physical?.close();
    this.#physical = null;
    this.#state = { kind: "error", error };
    this.#host.onObservationLoss?.();
    this.#host.onStateChange?.();
  }

  #replaceObservation(): void {
    if (this.#disposed) return;
    this.#physical?.close();
    this.#physical = null;
    this.#nextSequence = 0;
    this.#hydrationKind = "scratch";
    this.#state = { kind: "loading" };
    this.#host.onStateChange?.();
    this.#openPhysical();
  }
}
