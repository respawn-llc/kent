import type {
  ChatApi,
  ChatGoalAvailability,
  ChatGoalMutationResult,
  ChatGoalSetResult,
  ChatGoalSetTarget,
  ChatSessionTarget,
} from "@/api";
import { ContractError } from "@/api";

export type NewChatGoalBindingSnapshot =
  | Readonly<{
      kind: "unresolved";
      availability: ChatGoalAvailability | null;
      pending: boolean;
    }>
  | Readonly<{
      kind: "resolved_session";
      target: ChatSessionTarget;
    }>;

export type NewChatGoalBindingOptions = Readonly<{
  api: Pick<ChatApi, "setGoal">;
  captureTarget: () => Extract<ChatGoalSetTarget, { kind: "new_chat" }>;
  onResolved?: (delivery: NewChatGoalDelivery) => void;
}>;

export type NewChatGoalDelivery = Readonly<{
  target: ChatSessionTarget;
  mutation: ChatGoalMutationResult | null;
}>;
export type NewChatGoalResolutionListener = (delivery: NewChatGoalDelivery) => void;

export class NewChatGoalBinding {
  readonly #api: Pick<ChatApi, "setGoal">;
  readonly #captureTarget: NewChatGoalBindingOptions["captureTarget"];
  readonly #onResolved: NewChatGoalBindingOptions["onResolved"];
  readonly #listeners = new Set<() => void>();
  readonly #resolutionListeners = new Set<NewChatGoalResolutionListener>();
  #snapshot: NewChatGoalBindingSnapshot = {
    kind: "unresolved",
    availability: null,
    pending: false,
  };

  constructor(options: NewChatGoalBindingOptions) {
    this.#api = options.api;
    this.#captureTarget = options.captureTarget;
    this.#onResolved = options.onResolved;
  }

  get snapshot(): NewChatGoalBindingSnapshot {
    return this.#snapshot;
  }

  subscribe(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  subscribeResolution(listener: NewChatGoalResolutionListener): () => void {
    this.#resolutionListeners.add(listener);
    return () => this.#resolutionListeners.delete(listener);
  }

  setAvailability(availability: ChatGoalAvailability | null): void {
    if (this.#snapshot.kind !== "unresolved" || this.#snapshot.availability === availability) {
      return;
    }
    this.#snapshot = { ...this.#snapshot, availability };
    this.#notify();
  }

  async setGoal(objective: string): Promise<ChatGoalSetResult> {
    if (this.#snapshot.kind !== "unresolved") {
      throw new ContractError("New Chat Goal creation has already resolved.");
    }
    if (this.#snapshot.pending) {
      throw new ContractError("New Chat Goal creation is already pending.");
    }
    const target = this.#captureTarget();
    this.#snapshot = { ...this.#snapshot, pending: true };
    this.#notify();
    try {
      const result = await this.#api.setGoal(target, objective);
      const exactTarget: ChatSessionTarget = {
        projectID: target.projectID,
        workspace: { workspaceID: target.workspaceID },
        sessionID: result.sessionID,
      };
      const delivery = {
        target: exactTarget,
        mutation: result.outcome.kind === "mutation" ? result.outcome.mutation : null,
      } satisfies NewChatGoalDelivery;
      this.#onResolved?.(delivery);
      for (const listener of this.#resolutionListeners) {
        listener(delivery);
      }
      this.#snapshot = { kind: "resolved_session", target: exactTarget };
      this.#notify();
      return result;
    } catch (error) {
      if (this.#snapshot.kind === "unresolved") {
        this.#snapshot = { ...this.#snapshot, pending: false };
      }
      this.#notify();
      throw error;
    }
  }

  #notify(): void {
    for (const listener of this.#listeners) {
      listener();
    }
  }
}
