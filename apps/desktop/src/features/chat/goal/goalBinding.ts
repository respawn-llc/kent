import type {
  ChatApi,
  ChatGoal,
  ChatGoalAvailability,
  ChatGoalSetResult,
  ChatGoalSetTarget,
  ChatSessionTarget,
} from "@/api";
import { ContractError } from "@/api";
import { MutationObserver, type QueryClient } from "@tanstack/react-query";

export type NewChatGoalBindingSnapshot =
  | Readonly<{
      kind: "unresolved";
      availability: ChatGoalAvailability | null;
      pending: boolean;
      ready: boolean;
    }>
  | Readonly<{
      kind: "resolved_session";
      target: ChatSessionTarget;
    }>;

export type NewChatGoalHostDelivery = Readonly<{
  target: ChatSessionTarget;
  goal: ChatGoal | null;
}>;

export type NewChatGoalBindingOptions = Readonly<{
  client: QueryClient;
  api: Pick<ChatApi, "setGoal">;
  captureTarget: () => Extract<ChatGoalSetTarget, { kind: "new_chat" }>;
  onHostDelivery: (delivery: NewChatGoalHostDelivery) => void;
  onFailure?(): void;
}>;
type GoalHost = Pick<NewChatGoalBindingOptions, "captureTarget" | "onHostDelivery" | "onFailure">;

export class NewChatGoalBinding {
  #host: GoalHost;
  readonly #request: MutationObserver<
    ChatGoalSetResult,
    Error,
    Readonly<{
      target: Extract<ChatGoalSetTarget, { kind: "new_chat" }>;
      objective: string;
    }>
  >;
  readonly #listeners = new Set<() => void>();
  #snapshot: NewChatGoalBindingSnapshot = {
    kind: "unresolved",
    availability: null,
    pending: false,
    ready: true,
  };

  constructor(options: NewChatGoalBindingOptions) {
    this.#host = options;
    this.#request = new MutationObserver(options.client, {
      retry: false,
      networkMode: "always",
      mutationFn: async ({ target, objective }) => options.api.setGoal(target, objective),
      onSuccess: (result, { target }) => {
        if (result.sessionID.trim().length === 0)
          throw new ContractError("Goal Set success Session is required.");
        this.#host.onHostDelivery({
          target: { projectID: target.projectID, sessionID: result.sessionID },
          goal: result.outcome.kind === "mutation" ? committedGoal(result.outcome.mutation) : null,
        });
      },
      onError: () => this.#host.onFailure?.(),
    });
  }

  get snapshot(): NewChatGoalBindingSnapshot {
    return this.#snapshot;
  }
  updateHost(host: GoalHost): void {
    this.#host = host;
  }

  subscribe(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  setAvailability(availability: ChatGoalAvailability | null): void {
    if (this.#snapshot.kind !== "unresolved" || this.#snapshot.availability === availability) {
      return;
    }
    this.#snapshot = { ...this.#snapshot, availability };
    this.#notify();
  }

  followSession(target: ChatSessionTarget): void {
    if (this.#snapshot.kind === "resolved_session" && this.#snapshot.target.sessionID === target.sessionID)
      return;
    this.#snapshot = { kind: "resolved_session", target };
    this.#notify();
  }

  setReady(ready: boolean): void {
    if (this.#snapshot.kind !== "unresolved" || this.#snapshot.ready === ready) return;
    this.#snapshot = { ...this.#snapshot, ready };
    this.#notify();
  }

  get pending(): boolean {
    return this.#request.getCurrentResult().isPending;
  }

  async setGoal(objective: string): Promise<ChatGoalSetResult> {
    if (this.#snapshot.kind !== "unresolved") {
      throw new ContractError("New Chat Goal creation has already resolved.");
    }
    if (this.pending) {
      throw new ContractError("New Chat Goal creation is already pending.");
    }
    if (!this.#snapshot.ready) throw new ContractError("New Chat Settings are not ready.");
    const target = this.#host.captureTarget();
    const unsubscribe = this.#request.subscribe((result) => {
      if (this.#snapshot.kind === "unresolved")
        this.#snapshot = { ...this.#snapshot, pending: result.isPending };
      this.#notify();
    });
    try {
      return await this.#request.mutate({ target, objective });
    } finally {
      unsubscribe();
    }
  }

  #notify(): void {
    for (const listener of this.#listeners) {
      listener();
    }
  }
}

function committedGoal(
  mutation: Extract<ChatGoalSetResult["outcome"], { kind: "mutation" }>["mutation"],
): ChatGoal {
  if (mutation.kind !== "authoritative_goal") {
    throw new ContractError("New Chat Goal Set returned an illegal mutation result.");
  }
  return mutation.fact.goal;
}
