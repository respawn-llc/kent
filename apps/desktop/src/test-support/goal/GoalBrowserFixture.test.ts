import { goalFixtureFact, goalFixtureStates } from "./goalFixtureState";

describe("Goal browser fixture", () => {
  it("provides every deterministic Goal and host-state scenario", () => {
    expect(goalFixtureStates).toEqual([
      "absent",
      "active",
      "paused",
      "complete",
      "unsupported-agent",
      "workflow-session",
      "new-chat",
      "loading",
      "error-retry",
      "dirty-broadcast",
      "overlapping-read",
      "pending-pause",
      "pending-save",
      "error-action",
      "questions-off",
      "question-picker",
      "approval-picker",
    ]);
  });

  it.each(["absent", "active", "paused", "complete"] as const)(
    "projects the selectable Goal state %s",
    (state) => {
      const fact = goalFixtureFact(state);
      expect(fact.goal === null ? "absent" : fact.goal.status).toBe(state);
      expect(fact.availability).toBe("available");
    },
  );

  it("projects unsupported Agent availability without hiding the Goal", () => {
    const fact = goalFixtureFact("unsupported-agent");
    expect(fact.goal).not.toBeNull();
    expect(fact.availability).toBe("agent_capability_missing");
  });
});
