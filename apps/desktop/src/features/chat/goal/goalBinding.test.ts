import type { ChatGoalSetResult } from "@/api";
import { ChatOperationError, RpcError } from "@/api";
import { NewChatGoalBinding, type NewChatGoalBindingSnapshot, type NewChatGoalDelivery } from "./goalBinding";

const result: ChatGoalSetResult = {
  sessionID: "123e4567-e89b-42d3-a456-426614174000",
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
  it("captures the target once, delivers the result, then publishes the exact Session", async () => {
    const setGoal = vi.fn(async () => result);
    const deliveries: {
      delivery: NewChatGoalDelivery;
      snapshot: NewChatGoalBindingSnapshot;
    }[] = [];
    const binding = new NewChatGoalBinding({
      api: { setGoal },
      captureTarget: () => ({
        kind: "new_chat",
        projectID: "project-1",
        workspaceID: "workspace-1",
        initialSettings: {
          agentRole: "default",
          supervisor: "off",
          thinking: null,
          fast: null,
          questionsEnabled: true,
          autoCompactionEnabled: true,
        },
        initialInputDraft: "draft",
      }),
      onResolved: (delivery) => {
        deliveries.push({ delivery, snapshot: binding.snapshot });
      },
    });
    const snapshots: string[] = [];
    binding.subscribe(() => snapshots.push(binding.snapshot.kind));

    await expect(binding.setGoal("ship")).resolves.toEqual(result);

    expect(deliveries).toEqual([
      {
        delivery: {
          target: {
            projectID: "project-1",
            workspace: { workspaceID: "workspace-1" },
            sessionID: result.sessionID,
          },
          mutation: result.outcome.kind === "mutation" ? result.outcome.mutation : null,
        },
        snapshot: { kind: "unresolved", availability: null, pending: true },
      },
    ]);
    expect(setGoal).toHaveBeenCalledExactlyOnceWith(
      {
        kind: "new_chat",
        projectID: "project-1",
        workspaceID: "workspace-1",
        initialSettings: {
          agentRole: "default",
          supervisor: "off",
          thinking: null,
          fast: null,
          questionsEnabled: true,
          autoCompactionEnabled: true,
        },
        initialInputDraft: "draft",
      },
      "ship",
    );
    expect(binding.snapshot).toEqual({
      kind: "resolved_session",
      target: {
        projectID: "project-1",
        workspace: { workspaceID: "workspace-1" },
        sessionID: result.sessionID,
      },
    });
    expect(snapshots).toEqual(["unresolved", "resolved_session"]);
  });

  it("delivers a Session-bearing rejection without a Goal handoff", async () => {
    const rejected: ChatGoalSetResult = {
      sessionID: result.sessionID,
      outcome: {
        kind: "rejected",
        error: new ChatOperationError(
          new RpcError({ code: 500, message: "Goal Set failed", method: "runtime.goal.set" }),
          { kind: "runtime_unavailable", sessionID: result.sessionID },
        ),
      },
    };
    const deliveries: NewChatGoalDelivery[] = [];
    const binding = new NewChatGoalBinding({
      api: { setGoal: vi.fn(async () => rejected) },
      captureTarget: () => ({
        kind: "new_chat",
        projectID: "project-1",
        workspaceID: "workspace-1",
        initialSettings: {
          agentRole: "default",
          supervisor: "off",
          thinking: null,
          fast: null,
          questionsEnabled: true,
          autoCompactionEnabled: true,
        },
      }),
      onResolved: (delivery) => deliveries.push(delivery),
    });

    await expect(binding.setGoal("ship")).resolves.toEqual(rejected);

    expect(deliveries).toEqual([
      {
        target: {
          projectID: "project-1",
          workspace: { workspaceID: "workspace-1" },
          sessionID: result.sessionID,
        },
        mutation: null,
      },
    ]);
    expect(binding.snapshot).toEqual({
      kind: "resolved_session",
      target: {
        projectID: "project-1",
        workspace: { workspaceID: "workspace-1" },
        sessionID: result.sessionID,
      },
    });
  });
});
