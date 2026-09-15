import type { ChatApi, ChatGoal, ChatGoalSetResult, ChatGoalSetTarget, ChatSessionTarget } from "@/api";
import { ContractError } from "@/api";

export type NewChatGoalBindingSnapshot =
  | Readonly<{
      kind: "unresolved";
      pending: boolean;
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
  api: Pick<ChatApi, "setGoal">;
  captureTarget: () => Extract<ChatGoalSetTarget, { kind: "new_chat" }>;
  onHostDelivery: (delivery: NewChatGoalHostDelivery) => void | Promise<void>;
}>;

export class NewChatGoalBinding {
  readonly #api: Pick<ChatApi, "setGoal">;
  readonly #captureTarget: NewChatGoalBindingOptions["captureTarget"];
  readonly #onHostDelivery: NewChatGoalBindingOptions["onHostDelivery"];
  readonly #listeners = new Set<() => void>();
  #snapshot: NewChatGoalBindingSnapshot = {
    kind: "unresolved",
    pending: false,
  };

  constructor(options: NewChatGoalBindingOptions) {
    this.#api = options.api;
    this.#captureTarget = options.captureTarget;
    this.#onHostDelivery = options.onHostDelivery;
  }

  get snapshot(): NewChatGoalBindingSnapshot {
    return this.#snapshot;
  }

  subscribe(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
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
      if (result.sessionID.trim().length === 0) {
        throw new ContractError("Goal Set success Session is required.");
      }
      const exactTarget: ChatSessionTarget = {
        projectID: target.projectID,
        workspace: { workspaceID: target.workspaceID },
        sessionID: result.sessionID,
      };
      const goal = result.outcome.kind === "mutation" ? committedGoal(result.outcome.mutation) : null;
      await this.#onHostDelivery({ goal, target: exactTarget });
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

function committedGoal(
  mutation: Extract<ChatGoalSetResult["outcome"], { kind: "mutation" }>["mutation"],
): ChatGoal {
  if (mutation.kind !== "authoritative_goal") {
    throw new ContractError("New Chat Goal Set returned an illegal mutation result.");
  }
  return mutation.fact.goal;
}
