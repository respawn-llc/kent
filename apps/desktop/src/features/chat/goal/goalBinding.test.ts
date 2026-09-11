import type { ChatGoalSetResult } from "@/api";
import { NewChatGoalBinding } from "./goalBinding";

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
  },
  diagnostic: null,
};

describe("New Chat Goal binding", () => {
  it("captures the target once and publishes the exact delivered Session", async () => {
    const setGoal = vi.fn(async () => result);
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
    const snapshots: string[] = [];
    binding.subscribe(() => snapshots.push(binding.snapshot.kind));

    await expect(binding.setGoal("ship")).resolves.toEqual(result);

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
});
