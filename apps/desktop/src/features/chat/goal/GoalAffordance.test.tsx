import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

import type { ChatGoalFact } from "@/api";
import { TestAppProviders, createTestServices } from "@/test-support/app-services";
import { GoalAffordance } from "./GoalAffordance";

const goal: NonNullable<ChatGoalFact["goal"]> = {
  id: "goal-1",
  objective: "ship",
  status: "active",
  createdAt: "2026-09-11T10:00:00Z",
  updatedAt: "2026-09-11T10:00:00Z",
};

describe("Goal affordance", () => {
  it.each([
    ["absent", { goal: null, availability: "available" }],
    ["active", { goal, availability: "available" }],
    ["paused", { goal: { ...goal, status: "paused" }, availability: "available" }],
    ["complete", { goal: { ...goal, status: "complete" }, availability: "available" }],
  ] satisfies readonly [string, ChatGoalFact][])(
    "keeps the Goal label and target icon for %s",
    (_name, fact) => {
      const onActivate = vi.fn();
      render(
        <TestAppProviders services={createTestServices([])}>
          <GoalAffordance goal={fact} onActivate={onActivate} />
        </TestAppProviders>,
      );

      const control = screen.getByRole("button", { name: "Goal" });
      expect(control).toHaveTextContent("Goal");
      expect(control).toHaveAttribute("data-state", fact.goal?.status === "active" ? "active" : "neutral");
    },
  );

  it("emits activation every time the operator activates it", async () => {
    const onActivate = vi.fn();
    const user = userEvent.setup();
    render(
      <TestAppProviders services={createTestServices([])}>
        <GoalAffordance goal={{ goal: null, availability: "available" }} onActivate={onActivate} />
      </TestAppProviders>,
    );
    const control = screen.getByRole("button", { name: "Goal" });
    await user.click(control);
    await user.click(control);
    expect(onActivate).toHaveBeenCalledTimes(2);
  });
});
