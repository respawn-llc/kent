import type { ChatGoalSetResult } from "@/api";
import { ChatOperationError, RpcError } from "@/api";
import { createNewChatGoalResource, NewChatGoalBinding } from "./goalBinding";

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
    const resource = createNewChatGoalResource();
    const admitted: string[] = [];
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
    });
    binding.registerResource(resource);
    resource.registerMutationAdmission((mutation) => {
      admitted.push(mutation.fact.goal?.objective ?? "cleared");
      return true;
    });
    const snapshots: string[] = [];
    binding.subscribe(() => snapshots.push(binding.snapshot.kind));

    const completion = await binding.setGoal("ship");
    expect(completion.result).toEqual(result);
    expect(completion.delivery).toEqual({
      target: {
        projectID: "project-1",
        workspace: { workspaceID: "workspace-1" },
        sessionID: result.sessionID,
      },
      mutation: result.outcome.kind === "mutation" ? result.outcome.mutation : null,
    });
    await expect(completion.admission).resolves.toEqual({ kind: "acknowledged" });
    expect(admitted).toEqual(["ship"]);
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
    });

    const completion = await binding.setGoal("ship");
    expect(completion.result).toEqual(rejected);
    expect(completion.delivery).toEqual({
      target: {
        projectID: "project-1",
        workspace: { workspaceID: "workspace-1" },
        sessionID: result.sessionID,
      },
      mutation: null,
    });
    await expect(completion.admission).resolves.toEqual({ kind: "skipped" });
    expect(binding.snapshot).toEqual({
      kind: "resolved_session",
      target: {
        projectID: "project-1",
        workspace: { workspaceID: "workspace-1" },
        sessionID: result.sessionID,
      },
    });
  });

  it("does not let a discarded destination resource consume a staged handoff", async () => {
    const resource = createNewChatGoalResource();
    const firstAdmit = vi.fn(() => true);
    const removeFirst = resource.registerMutationAdmission(firstAdmit);
    const admission = resource.stage({
      target: {
        projectID: "project-1",
        workspace: { workspaceID: "workspace-1" },
        sessionID: result.sessionID,
      },
      mutation: result.outcome.kind === "mutation" ? result.outcome.mutation : null,
    });
    removeFirst();
    const secondAdmit = vi.fn(() => true);
    resource.registerMutationAdmission(secondAdmit);

    await expect(admission).resolves.toEqual({ kind: "acknowledged" });
    expect(firstAdmit).not.toHaveBeenCalled();
    expect(secondAdmit).toHaveBeenCalledOnce();
  });

  it("does not stage a late result into a replacement active root", async () => {
    let resolveResult!: (value: ChatGoalSetResult) => void;
    const setGoal = vi.fn(async () => {
      return new Promise<ChatGoalSetResult>((resolve) => {
        resolveResult = resolve;
      });
    });
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
      }),
    });
    const firstResource = createNewChatGoalResource();
    binding.registerResource(firstResource);
    const completionPromise = binding.setGoal("ship");
    const secondResource = createNewChatGoalResource();
    const secondAdmit = vi.fn(() => true);
    secondResource.registerMutationAdmission(secondAdmit);
    binding.registerResource(secondResource);
    resolveResult(result);

    const completion = await completionPromise;
    await expect(completion.admission).resolves.toEqual({ kind: "skipped" });
    expect(secondAdmit).not.toHaveBeenCalled();
  });
});
