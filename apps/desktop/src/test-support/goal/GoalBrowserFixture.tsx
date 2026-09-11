import { useState } from "react";

import { GoalAffordance } from "@/features/chat";
import {
  goalFixtureConfigs,
  goalFixtureFact,
  goalFixtureStates,
  type GoalFixtureState,
} from "./goalFixtureState";

export function GoalBrowserFixture() {
  const [state, setState] = useState<GoalFixtureState>("absent");
  const config = goalFixtureConfigs[state];
  return (
    <div className="grid gap-[var(--space-3)] p-[var(--space-4)]" data-testid="goal-browser-fixture">
      <label className="grid gap-[var(--space-1)] text-sm">
        Goal fixture state
        <select
          aria-label="Goal fixture state"
          onChange={(event) => {
            const nextState = goalFixtureStates.find((candidate) => candidate === event.target.value);
            if (nextState !== undefined) setState(nextState);
          }}
          value={state}
        >
          {goalFixtureStates.map((option) => (
            <option key={option} value={option}>
              {option}
            </option>
          ))}
        </select>
      </label>
      <GoalAffordance goal={goalFixtureFact(state)} onActivate={() => undefined} />
      <p className="m-0 text-sm text-[var(--color-muted)]" data-testid="goal-browser-fixture-description">
        {config.description}
      </p>
    </div>
  );
}
