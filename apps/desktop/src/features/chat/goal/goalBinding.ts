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
}>;

export type NewChatGoalDelivery = Readonly<{
  target: ChatSessionTarget;
  mutation: ChatGoalMutationResult | null;
}>;

export type NewChatGoalAdmission = Readonly<{ kind: "acknowledged" }> | Readonly<{ kind: "skipped" }>;

export type NewChatGoalCompletion = Readonly<{
  result: ChatGoalSetResult;
  delivery: NewChatGoalDelivery;
  admission: Promise<NewChatGoalAdmission>;
}>;

export type NewChatGoalResource = Readonly<{
  registerMutationAdmission(admit: (mutation: ChatGoalMutationResult) => boolean): () => void;
  stage(delivery: NewChatGoalDelivery): NewChatGoalCompletion["admission"];
  close(): void;
}>;

export function createNewChatGoalResource(): NewChatGoalResource {
  let closed = false;
  let resource: Readonly<{ token: symbol; admit: (mutation: ChatGoalMutationResult) => boolean }> | null =
    null;
  let pending: Readonly<{
    resolve(outcome: NewChatGoalAdmission): void;
    delivery: NewChatGoalDelivery;
  }> | null = null;
  let checkScheduled = false;

  const settle = (outcome: NewChatGoalAdmission) => {
    const current = pending;
    if (current === null) return;
    pending = null;
    current.resolve(outcome);
  };
  const check = () => {
    checkScheduled = false;
    if (closed || pending === null || resource === null || pending.delivery.mutation === null) {
      return;
    }
    if (resource.admit(pending.delivery.mutation)) {
      settle({ kind: "acknowledged" });
    }
  };
  const scheduleCheck = () => {
    if (checkScheduled) return;
    checkScheduled = true;
    queueMicrotask(check);
  };

  return {
    registerMutationAdmission: (admit) => {
      if (closed) return () => undefined;
      const current = { token: Symbol("Goal destination resource"), admit };
      resource = current;
      scheduleCheck();
      return () => {
        if (resource?.token === current.token) {
          resource = null;
          scheduleCheck();
        }
      };
    },
    stage: async (delivery) => {
      if (closed || delivery.mutation === null) {
        return Promise.resolve({ kind: "skipped" });
      }
      if (pending !== null) {
        throw new ContractError("New Chat Goal has more than one staged destination handoff.");
      }
      let resolveAdmission: ((outcome: NewChatGoalAdmission) => void) | undefined;
      const admission = new Promise<NewChatGoalAdmission>((resolve) => {
        resolveAdmission = resolve;
      });
      if (resolveAdmission === undefined) {
        throw new Error("New Chat Goal admission did not initialize.");
      }
      pending = { delivery, resolve: resolveAdmission };
      scheduleCheck();
      return admission;
    },
    close: () => {
      if (closed) return;
      closed = true;
      resource = null;
      settle({ kind: "skipped" });
    },
  };
}

export class NewChatGoalBinding {
  readonly #api: Pick<ChatApi, "setGoal">;
  readonly #captureTarget: NewChatGoalBindingOptions["captureTarget"];
  readonly #listeners = new Set<() => void>();
  #resource: NewChatGoalResource | null = null;
  #snapshot: NewChatGoalBindingSnapshot = {
    kind: "unresolved",
    availability: null,
    pending: false,
  };

  constructor(options: NewChatGoalBindingOptions) {
    this.#api = options.api;
    this.#captureTarget = options.captureTarget;
  }

  get snapshot(): NewChatGoalBindingSnapshot {
    return this.#snapshot;
  }

  subscribe(listener: () => void): () => void {
    this.#listeners.add(listener);
    return () => this.#listeners.delete(listener);
  }

  registerResource(resource: NewChatGoalResource): () => void {
    const previous = this.#resource;
    previous?.close();
    this.#resource = resource;
    return () => {
      if (this.#resource === resource) {
        this.#resource = null;
        resource.close();
      }
    };
  }

  setAvailability(availability: ChatGoalAvailability | null): void {
    if (this.#snapshot.kind !== "unresolved" || this.#snapshot.availability === availability) {
      return;
    }
    this.#snapshot = { ...this.#snapshot, availability };
    this.#notify();
  }

  async setGoal(objective: string): Promise<NewChatGoalCompletion> {
    if (this.#snapshot.kind !== "unresolved") {
      throw new ContractError("New Chat Goal creation has already resolved.");
    }
    if (this.#snapshot.pending) {
      throw new ContractError("New Chat Goal creation is already pending.");
    }
    const target = this.#captureTarget();
    const resource = this.#resource;
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
      const admission = resource?.stage(delivery) ?? Promise.resolve({ kind: "skipped" as const });
      this.#snapshot = { kind: "resolved_session", target: exactTarget };
      this.#notify();
      return { admission, delivery, result };
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
