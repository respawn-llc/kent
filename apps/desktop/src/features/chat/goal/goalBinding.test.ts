import type { ChatGoalSetResult, ChatSessionTarget } from "@/api";
import { ChatOperationError, ContractError, RpcError } from "@/api";
import { NewChatGoalBinding, type NewChatGoalHostDelivery } from "./goalBinding";

const sessionID = "123e4567-e89b-42d3-a456-426614174000";
const target = {
  kind: "new_chat" as const,
  projectID: "project-1",
  workspaceID: "workspace-1",
  initialSettings: {
    agentRole: "default" as const,
    supervisor: "off" as const,
    thinking: null,
    fast: null,
    questionsEnabled: true,
    autoCompactionEnabled: true,
  },
  initialInputDraft: "composer draft",
};

const committedResult: ChatGoalSetResult = {
  sessionID,
  outcome: {
    kind: "mutation",
    mutation: {
      kind: "authoritative_goal",
      fact: {
        goal: {
          id: "goal-1",
          objective: "ship",
          status: "active",
          createdAt: "2026-09-11T10:00:00Z",
          updatedAt: "2026-09-11T10:00:00Z",
        },
        availability: "available",
      },
    },
    diagnostic: null,
  },
};

describe("New Chat Goal binding", () => {
  it("captures the target and delivers Session/Goal before resolving the binding", async () => {
    const setGoal = vi.fn(async () => committedResult);
    const delivery = vi.fn<(value: NewChatGoalHostDelivery) => void>();
    const binding = new NewChatGoalBinding({
      api: { setGoal },
      captureTarget: () => target,
      onHostDelivery: delivery,
    });
    const snapshots: string[] = [];
    binding.subscribe(() => snapshots.push(binding.snapshot.kind));

    await expect(binding.setGoal("ship")).resolves.toEqual(committedResult);

    const expectedTarget: ChatSessionTarget = {
      projectID: target.projectID,
      workspace: { workspaceID: target.workspaceID },
      sessionID,
    };
    expect(delivery).toHaveBeenCalledExactlyOnceWith({
      target: expectedTarget,
      goal: committedResult.outcome.kind === "mutation" ? committedResult.outcome.mutation.fact.goal : null,
    });
    expect(binding.snapshot).toEqual({ kind: "resolved_session", target: expectedTarget });
    expect(snapshots).toEqual(["unresolved", "resolved_session"]);
    expect(setGoal).toHaveBeenCalledExactlyOnceWith(target, "ship");
  });

  it("delivers a Session-bearing rejection without a Goal", async () => {
    const rejected: ChatGoalSetResult = {
      sessionID,
      outcome: {
        kind: "rejected",
        error: new ChatOperationError(
          new RpcError({ code: 500, message: "Goal Set failed", method: "runtime.goal.set" }),
          { kind: "runtime_unavailable", sessionID },
        ),
      },
    };
    const delivery = vi.fn<(value: NewChatGoalHostDelivery) => void>();
    const binding = new NewChatGoalBinding({
      api: { setGoal: vi.fn(async () => rejected) },
      captureTarget: () => target,
      onHostDelivery: delivery,
    });

    await expect(binding.setGoal("ship")).resolves.toEqual(rejected);

    expect(delivery).toHaveBeenCalledExactlyOnceWith({
      target: {
        projectID: target.projectID,
        workspace: { workspaceID: target.workspaceID },
        sessionID,
      },
      goal: null,
    });
    expect(binding.snapshot.kind).toBe("resolved_session");
  });

  it("does not deliver or resolve a malformed Goal result", async () => {
    const delivery = vi.fn<(value: NewChatGoalHostDelivery) => void>();
    const malformed: ChatGoalSetResult = {
      ...committedResult,
      sessionID: "",
    };
    const binding = new NewChatGoalBinding({
      api: { setGoal: vi.fn(async () => malformed) },
      captureTarget: () => target,
      onHostDelivery: delivery,
    });

    await expect(binding.setGoal("ship")).rejects.toBeInstanceOf(ContractError);
    expect(delivery).not.toHaveBeenCalled();
    expect(binding.snapshot).toEqual({ kind: "unresolved", availability: null, pending: false });
  });

  it("waits for asynchronous host delivery before publishing the terminal Session", async () => {
    let resolveDelivery!: () => void;
    const hostDelivery = new Promise<void>((resolve) => {
      resolveDelivery = resolve;
    });
    const binding = new NewChatGoalBinding({
      api: { setGoal: vi.fn(async () => committedResult) },
      captureTarget: () => target,
      onHostDelivery: async () => hostDelivery,
    });
    const completion = binding.setGoal("ship");

    await vi.waitFor(() => {
      expect(binding.snapshot).toEqual({ kind: "unresolved", availability: null, pending: true });
    });
    resolveDelivery();
    await expect(completion).resolves.toEqual(committedResult);
    expect(binding.snapshot.kind).toBe("resolved_session");
  });
});
