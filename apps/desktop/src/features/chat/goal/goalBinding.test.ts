import type { ChatGoalSetResult, ChatSessionTarget } from "@/api";
import { ChatOperationError, ContractError, RpcError } from "@/api";
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
      await expect(result.current.setGoal("ship")).resolves.toEqual(committedResult);
    });

    const expectedTarget: ChatSessionTarget = {
      projectID: target.projectID,
      sessionID,
    };
    expect(delivery).toHaveBeenCalledExactlyOnceWith({
      target: expectedTarget,
      origin: target,
      goal: committedResult.outcome.kind === "mutation" ? committedResult.outcome.mutation.fact.goal : null,
    });
    expect(result.current.state).toEqual({
      kind: "resolved_session",
      target: { kind: "session", ...expectedTarget },
    });
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
    const owner = createGoalFixtureOwner({ api: { setGoal: vi.fn(async () => rejected) }, target });
    const { result } = renderHook(() => useGoalFixtureActions(owner, delivery));
    await act(async () => {
      await expect(result.current.setGoal("ship")).resolves.toEqual(rejected);
    });

    expect(delivery).toHaveBeenCalledExactlyOnceWith({
      target: {
        projectID: target.projectID,
        sessionID,
      },
      goal: null,
      origin: target,
    });
    expect(result.current.state.kind).toBe("resolved_session");
  });

  it("does not deliver or resolve a malformed Goal result", async () => {
    const delivery = vi.fn<(value: NewChatGoalHostDelivery) => void>();
    const malformed: ChatGoalSetResult = {
      ...committedResult,
      sessionID: "",
    };
    const owner = createGoalFixtureOwner({ api: { setGoal: vi.fn(async () => malformed) }, target });
    const { result } = renderHook(() => useGoalFixtureActions(owner, delivery));
    await act(async () => {
      await expect(result.current.setGoal("ship")).rejects.toBeInstanceOf(ContractError);
    });
    expect(delivery).not.toHaveBeenCalled();
    expect(result.current.state).toEqual({
      kind: "unresolved",
      availability: null,
      pending: false,
      ready: true,
    });
  });

  it("follows the host Session instead of selecting a private terminal Session", async () => {
    const owner = createGoalFixtureOwner({ api: { setGoal: vi.fn(async () => committedResult) }, target });
    const { result } = renderHook(() => useGoalFixtureActions(owner, () => undefined));
    act(() => {
      result.current.select({ kind: "session", projectID: target.projectID, sessionID: "host-selected" });
    });
    expect(result.current.state).toEqual({
      kind: "resolved_session",
      target: { kind: "session", projectID: target.projectID, sessionID: "host-selected" },
    });
  });
});
