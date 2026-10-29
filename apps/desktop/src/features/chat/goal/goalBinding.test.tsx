import { describe, expect, it, vi } from "bun:test";
import type { ChatGoalSetResult, ChatSessionTarget } from "@/api";
import { ChatOperationError, RpcError } from "@/api";
import type { NewChatGoalHostDelivery } from "./goalBinding";
import { createGoalFixtureOwner, useGoalFixtureActions } from "./goalBindingFixtures";
import { act, renderHook } from "@testing-library/react";

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
    const owner = createGoalFixtureOwner({ api: { setGoal }, target });
    const { result } = renderHook(() => useGoalFixtureActions(owner, delivery));
    await act(async () => {
      expect(result.current.setGoal("ship")).resolves.toEqual(committedResult);
    });

    const expectedTarget: ChatSessionTarget = {
      projectID: target.projectID,
      sessionID,
    };
    const resolvedTarget = {
      kind: "session",
      ...expectedTarget,
    } as const;
    expect(delivery).toHaveBeenCalledTimes(1);
    expect(delivery).toHaveBeenCalledWith({
      target: expectedTarget,
      origin: target,
      goal: committedResult.outcome.kind === "mutation" ? committedResult.outcome.mutation.fact.goal : null,
    });
    expect(result.current.state).toEqual({
      kind: "resolved_session",
      target: resolvedTarget,
    });
    expect(setGoal).toHaveBeenCalledTimes(1);
    expect(setGoal).toHaveBeenCalledWith(target, "ship");
  });

  it("delivers a Session-bearing rejection without a Goal", async () => {
    const rejected: ChatGoalSetResult = {
      sessionID,
      outcome: {
        kind: "rejected",
        error: new ChatOperationError(
          new RpcError({ code: "internal_failure", message: "Goal Set failed", method: "runtime.goal.set" }),
          { kind: "runtime_unavailable", sessionID },
        ),
      },
    };
    const delivery = vi.fn<(value: NewChatGoalHostDelivery) => void>();
    const owner = createGoalFixtureOwner({ api: { setGoal: vi.fn(async () => rejected) }, target });
    const { result } = renderHook(() => useGoalFixtureActions(owner, delivery));
    await act(async () => {
      expect(result.current.setGoal("ship")).resolves.toEqual(rejected);
    });

    expect(delivery).toHaveBeenCalledTimes(1);
    expect(delivery).toHaveBeenCalledWith({
      target: {
        projectID: target.projectID,
        sessionID,
      },
      goal: null,
      origin: target,
    });
    expect(result.current.state.kind).toBe("resolved_session");
  });

  it("follows the host Session instead of selecting a private terminal Session", async () => {
    const owner = createGoalFixtureOwner({ api: { setGoal: vi.fn(async () => committedResult) }, target });
    const { result } = renderHook(() => useGoalFixtureActions(owner, () => undefined));
    act(() => {
      result.current.select({ kind: "session", projectID: target.projectID, sessionID: "host-selected" });
    });
    const expectedTarget = {
      kind: "session",
      projectID: target.projectID,
      sessionID: "host-selected",
    } as const;
    expect(result.current.state).toEqual({
      kind: "resolved_session",
      target: expectedTarget,
    });
  });
});
