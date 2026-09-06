import type {
  ApiSubscription,
  ChatApi,
  ChatSessionTarget,
  ChatTranscriptHandler,
  ChatTranscriptMessage,
  ChatTranscriptPayloadByKind,
} from "@/api";
import { ContractError } from "@/api";

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
      onComplete: (completion) => {
        if (!this.#accepts(generation, settled)) return;
        settled = true;
        this.#subscription = null;
        if (completion.code === 0 && terminalUnavailable) {
          this.#openPhysicalSubscription();
          return;
        }
        this.#handler.onComplete(completion);
      },
      onError: (error) => {
        if (!this.#accepts(generation, settled)) return;
        settled = true;
        this.#subscription = null;
        this.#handler.onError(error);
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
  | Readonly<{ kind: "loading" | "observing" | "recovering" | "disposed" }>
  | Readonly<{ kind: "error"; error: Error }>;
export type ChatTranscriptObservationHost = Readonly<{
  onHydration(kind: ChatTranscriptHydrationKind, hydration: ChatTranscriptPayloadByKind["hydration"]): void;
  onEvent(event: Exclude<ChatTranscriptMessage, { kind: "hydration" }>): void;
  onRecoveryBegin(): void;
  onForceMainViewRead(): void;
  onError(error: Error): void;
  onStateChange?(): void;
}>;

export class ChatTranscriptObservation {
  readonly #api: TranscriptSubscriber;
  readonly #target: ChatSessionTarget;
  readonly #host: ChatTranscriptObservationHost;
  #physical: ChatTranscriptPhysicalObservation | null = null;
  #state: ChatTranscriptObservationState = { kind: "loading" };
  #hydrationKind: ChatTranscriptHydrationKind = "initial";
  #nextSequence = 0;
  #replacementInFlight = false;
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

  recoverContinuity(): void {
    this.#beginRecovery(true);
  }

  replaceForReconnect(): void {
    this.#beginRecovery(true);
  }

  retry(): void {
    if (this.#state.kind !== "error") return;
    this.#beginRecovery(false);
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
        if (this.#hasHydrated && this.#hydrationKind === "initial") {
          this.#hydrationKind = "reattachment";
        } else if (this.#hasHydrated && this.#hydrationKind !== "scratch") {
          this.#hydrationKind = "reattachment";
        }
        this.#nextSequence = 0;
      },
      onEvent: (event) => {
        this.#admit(event);
      },
      onComplete: (completion) => {
        this.#continuityFailure(
          new ContractError(
            `Transcript observation completed with code ${completion.code.toString()}: ${completion.message}`,
          ),
        );
      },
      onError: (error) => {
        this.#continuityFailure(error);
      },
    });
    this.#physical.start();
  }

  #admit(event: ChatTranscriptMessage): void {
    if (this.#disposed) return;
    if (this.#nextSequence === 0) {
      if (event.kind !== "hydration" || event.sequence !== 1) {
        this.#continuityFailure(
          new ContractError("Transcript observation must begin with sequence-1 hydration."),
        );
        return;
      }
      try {
        this.#host.onHydration(this.#hydrationKind, event.payload);
      } catch (error) {
        this.#continuityFailure(
          error instanceof Error ? error : new ContractError("Transcript hydration admission failed."),
        );
        return;
      }
      this.#nextSequence = 1;
      this.#hydrationKind = "initial";
      this.#replacementInFlight = false;
      this.#hasHydrated = true;
      this.#state = { kind: "observing" };
      this.#host.onStateChange?.();
      return;
    }
    if (event.kind === "hydration" || event.sequence !== this.#nextSequence + 1) {
      this.#continuityFailure(new ContractError("Transcript observation sequence is not continuous."));
      return;
    }
    try {
      this.#host.onEvent(event);
    } catch (error) {
      this.#continuityFailure(
        error instanceof Error ? error : new ContractError("Transcript event admission failed."),
      );
      return;
    }
    this.#nextSequence = event.sequence;
  }

  #continuityFailure(error: Error): void {
    if (this.#disposed) return;
    if (this.#replacementInFlight) {
      this.#physical?.close();
      this.#physical = null;
      this.#state = { kind: "error", error };
      this.#host.onError(error);
      this.#host.onStateChange?.();
      return;
    }
    this.#beginRecovery(true);
  }

  #beginRecovery(forceMainViewRead: boolean): void {
    if (this.#disposed || this.#state.kind === "recovering") return;
    this.#physical?.close();
    this.#physical = null;
    this.#nextSequence = 0;
    this.#hydrationKind = "scratch";
    this.#replacementInFlight = true;
    this.#state = { kind: "recovering" };
    this.#host.onRecoveryBegin();
    if (forceMainViewRead) this.#host.onForceMainViewRead();
    this.#host.onStateChange?.();
    this.#openPhysical();
  }
}
