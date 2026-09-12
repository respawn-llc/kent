import type { Hydration, RuntimeActivity, ThinkingStatusPresentation } from "./types";

export function thinkingStatus(
  activity: RuntimeActivity | null,
  latest: Hydration["ActiveThinkingStatus"],
): ThinkingStatusPresentation | null {
  if (activity === null || activity.State === "unavailable" || activity.State === "awaiting_prompt")
    return null;
  if (activity.State !== "running" || activity.ActiveStep === null) {
    return activity.Reviewer === "invoking" ? { kind: "reviewing" } : null;
  }
  return mainStatus(activity.ActiveStep, latest);
}

function mainStatus(
  step: NonNullable<RuntimeActivity["ActiveStep"]>,
  latest: Hydration["ActiveThinkingStatus"],
): ThinkingStatusPresentation {
  switch (step.ActiveKind) {
    case "user_turn":
    case "workflow_turn":
    case "goal_loop":
      return latest?.StepID === step.StepID ? { kind: "text", text: latest.Text } : { kind: "working" };
    case "compaction":
    case "pre_submit_compaction":
      return { kind: "compacting" };
    case "user_shell":
    case "background":
    case "runtime_maintenance":
      return { kind: "running" };
  }
}
