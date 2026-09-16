import type { ChatGoalSetResult, ChatSessionTarget } from "@/api";
import { ChatOperationError, ContractError, RpcError } from "@/api";
import { NewChatGoalBinding, type NewChatGoalHostDelivery } from "./goalBinding";
import { QueryClient } from "@tanstack/react-query";

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
      client: new QueryClient(),
      api: { setGoal },
      captureTarget: () => target,
      onHostDelivery: (value) => {
        delivery(value);
        binding.followSession(value.target);
      },
    });
    const snapshots: string[] = [];
    binding.subscribe(() => snapshots.push(binding.snapshot.kind));

    await expect(binding.setGoal("ship")).resolves.toEqual(committedResult);

    const expectedTarget: ChatSessionTarget = {
      projectID: target.projectID,
      sessionID,
    };
    expect(delivery).toHaveBeenCalledExactlyOnceWith({
      target: expectedTarget,
      goal: committedResult.outcome.kind === "mutation" ? committedResult.outcome.mutation.fact.goal : null,
    });
    expect(binding.snapshot).toEqual({ kind: "resolved_session", target: expectedTarget });
    expect(snapshots.at(-1)).toBe("resolved_session");
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
      client: new QueryClient(),
      api: { setGoal: vi.fn(async () => rejected) },
      captureTarget: () => target,
      onHostDelivery: (value) => {
        delivery(value);
        binding.followSession(value.target);
      },
    });

    await expect(binding.setGoal("ship")).resolves.toEqual(rejected);

    expect(delivery).toHaveBeenCalledExactlyOnceWith({
      target: {
        projectID: target.projectID,
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
      client: new QueryClient(),
      api: { setGoal: vi.fn(async () => malformed) },
      captureTarget: () => target,
      onHostDelivery: delivery,
    });

    await expect(binding.setGoal("ship")).rejects.toBeInstanceOf(ContractError);
    expect(delivery).not.toHaveBeenCalled();
    expect(binding.snapshot).toEqual({ kind: "unresolved", availability: null, pending: false, ready: true });
  });

  it("follows the host Session instead of selecting a private terminal Session", async () => {
    const binding = new NewChatGoalBinding({
      client: new QueryClient(),
      api: { setGoal: vi.fn(async () => committedResult) },
      captureTarget: () => target,
      onHostDelivery: () => {
        binding.followSession({ projectID: target.projectID, sessionID: "host-selected" });
      },
    });
    await expect(binding.setGoal("ship")).resolves.toEqual(committedResult);
    expect(binding.snapshot).toEqual({
      kind: "resolved_session",
      target: { projectID: target.projectID, sessionID: "host-selected" },
    });
  });
});
