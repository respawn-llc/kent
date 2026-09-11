import type {
  ApiSubscription,
  ChatApi,
  ChatGoal,
  ChatGoalFact,
  ChatGoalMutationResult,
  ChatGoalProjection,
  ChatGoalStatus,
  ChatSessionTarget,
} from "@/api";
import { ContractError } from "@/api";

export type ChatGoalMutationIntent =
  | Readonly<{ kind: "goal"; preview: Readonly<{ objective: string; status: ChatGoalStatus }> }>
  | Readonly<{ kind: "clear" }>;
export type ChatGoalPresentation =
  Readonly<{ kind: "authority" }> | Readonly<{ kind: "unresolved"; intent: ChatGoalMutationIntent }>;
export type ChatGoalObservationState =
  Readonly<{ kind: "loading" | "observed" | "disposed" }> | Readonly<{ kind: "error"; error: Error }>;
export type ChatGoalDestinationSnapshot = Readonly<{
  authority: ChatGoalProjection;
  presentation: ChatGoalPresentation;
  observation: ChatGoalObservationState;
}>;
export type ChatGoalMutationHandle = Readonly<{ token: symbol }>;

type GoalObserver = Pick<ChatApi, "subscribeGoal">;
type Listener = () => void;

export class ChatGoalProjectionSource {
  #snapshot: ChatGoalProjection = { kind: "unobserved" };
  #generation = 0;
  #disposed = false;

  get snapshot(): ChatGoalProjection {
    return this.#snapshot;
  }

  get generation(): number {
    return this.#generation;
  }

  get disposed(): boolean {
    return this.#disposed;
  }

  admit(fact: ChatGoalFact): boolean {
    if (this.#disposed) return false;
    this.#snapshot = { kind: "observed", value: cloneFact(fact) };
    this.#generation++;
    return true;
  }

  dispose(): void {
    if (this.#disposed) return;
    this.#disposed = true;
  }
}

interface ActiveMutation {
  handle: ChatGoalMutationHandle;
  capturedGeneration: number;
  intent: ChatGoalMutationIntent;
}

export class ChatGoalDestinationController {
  readonly source = new ChatGoalProjectionSource();
  readonly #api: GoalObserver;
  readonly #target: ChatSessionTarget;
  readonly #listeners = new Set<Listener>();
  #subscription: ApiSubscription | null = null;
  #observationGeneration = 0;
  #nextSequence = 0;
  #observation: ChatGoalObservationState = { kind: "loading" };
  #mutation: ActiveMutation | null = null;
  #automaticReplacementAvailable = false;
  #started = false;
  #disposed = false;

  constructor(api: GoalObserver, target: ChatSessionTarget) {
    this.#api = api;
    this.#target = target;
  }

  get snapshot(): ChatGoalDestinationSnapshot {
    const presentation: ChatGoalPresentation =
      this.#mutation !== null ? { kind: "unresolved", intent: this.#mutation.intent } : { kind: "authority" };
    return {
      authority: this.source.snapshot,
      presentation,
      observation: this.#observation,
    };
  }

  subscribe(listener: Listener): () => void {
    if (this.#disposed) return () => undefined;
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  start(): void {
    if (this.#started || this.#disposed) return;
    this.#started = true;
    this.#openObservation();
  }

  replaceObservation(): void {
    if (this.#disposed) return;
    this.#subscription?.close();
    this.#subscription = null;
    this.#automaticReplacementAvailable = false;
    this.#openObservation();
  }

  begin(intent: ChatGoalMutationIntent): ChatGoalMutationHandle {
    if (this.#disposed) throw new ContractError("Disposed Goal destination cannot begin a mutation.");
    if (this.source.snapshot.kind !== "observed")
      throw new ContractError("Goal mutation requires successful hydration.");
    if (this.#mutation !== null) throw new ContractError("Goal mutation is already pending.");
    const handle = { token: Symbol("Goal mutation") };
    this.#mutation = {
      handle,
      capturedGeneration: this.source.generation,
      intent: cloneIntent(intent),
    };
    this.#notify();
    return handle;
  }

  succeed(handle: ChatGoalMutationHandle, result: ChatGoalMutationResult): boolean {
    const mutation = this.#currentMutation(handle);
    if (mutation === null) return false;
    const authorityUnchanged = this.source.generation === mutation.capturedGeneration;
    this.#mutation = null;
    if (authorityUnchanged) this.source.admit(result.fact);
    this.#notify();
    return true;
  }

  fail(handle: ChatGoalMutationHandle): boolean {
    if (this.#currentMutation(handle) === null) return false;
    this.#mutation = null;
    this.#notify();
    return true;
  }

  dispose(): void {
    if (this.#disposed) return;
    this.#disposed = true;
    this.#observationGeneration++;
    this.#subscription?.close();
    this.#subscription = null;
    this.#mutation = null;
    this.#observation = { kind: "disposed" };
    this.source.dispose();
    this.#notify();
    this.#listeners.clear();
  }

  #openObservation(): void {
    const generation = ++this.#observationGeneration;
    this.#nextSequence = 0;
    this.#observation = { kind: "loading" };
    this.#notify();
    this.#subscription = this.#api.subscribeGoal(this.#target, {
      onOpen: () => {
        if (!this.#accepts(generation)) return;
        this.#nextSequence = 0;
        this.#observation = { kind: "loading" };
        this.#notify();
      },
      onEvent: (observation) => {
        if (!this.#accepts(generation)) return;
        if (
          (this.#nextSequence === 0 && (observation.kind !== "hydration" || observation.sequence !== 1)) ||
          (this.#nextSequence !== 0 &&
            (observation.kind === "hydration" || observation.sequence !== this.#nextSequence + 1))
        ) {
          this.#failObservation(new ContractError("Goal observation sequence is not continuous."));
          return;
        }
        this.#nextSequence = observation.sequence;
        this.#automaticReplacementAvailable = true;
        this.#observation = { kind: "observed" };
        this.source.admit(observation.fact);
        this.#notify();
      },
      onComplete: (code, message) => {
        if (!this.#accepts(generation)) return;
        this.#handleObservationFailure(
          new ContractError(
            code === 0
              ? "Goal observation completed unexpectedly."
              : `Goal observation completed with code ${code.toString()}: ${message}`,
          ),
        );
      },
      onError: (error) => {
        if (this.#accepts(generation)) this.#handleObservationFailure(error);
      },
    });
  }

  #handleObservationFailure(error: Error): void {
    if (this.source.snapshot.kind === "observed" && this.#automaticReplacementAvailable) {
      this.#automaticReplacementAvailable = false;
      this.#subscription?.close();
      this.#subscription = null;
      this.#openObservation();
      return;
    }
    this.#failObservation(error);
  }

  #failObservation(error: Error): void {
    this.#observationGeneration++;
    this.#subscription?.close();
    this.#subscription = null;
    this.#observation = { kind: "error", error };
    this.#notify();
  }

  #accepts(generation: number): boolean {
    return !this.#disposed && generation === this.#observationGeneration;
  }

  #currentMutation(handle: ChatGoalMutationHandle): ActiveMutation | null {
    if (this.#disposed || this.#mutation === null || this.#mutation.handle.token !== handle.token) {
      return null;
    }
    return this.#mutation;
  }

  #notify(): void {
    for (const listener of this.#listeners) listener();
  }
}

function cloneIntent(intent: ChatGoalMutationIntent): ChatGoalMutationIntent {
  return intent.kind === "clear" ? intent : { kind: "goal", preview: { ...intent.preview } };
}

function cloneFact(fact: ChatGoalFact): ChatGoalFact {
  return {
    goal: fact.goal === null ? null : cloneGoal(fact.goal),
    availability: fact.availability,
  };
}

function cloneGoal(goal: ChatGoal): ChatGoal {
  return { ...goal };
}
