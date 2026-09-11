import type { ChatGoalAvailability, ChatGoalFact, ChatGoalStatus } from "@/api";

export const goalFixtureStates = [
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
] as const;
export type GoalFixtureState = (typeof goalFixtureStates)[number];

type GoalFixtureConfig = Readonly<{
  availability: ChatGoalAvailability;
  status: ChatGoalStatus | null;
  description: string;
}>;

export const goalFixtureConfigs: Record<GoalFixtureState, GoalFixtureConfig> = {
  absent: {
    availability: "available",
    status: null,
    description: "Absent Goal opens the focused create editor.",
  },
  active: {
    availability: "available",
    status: "active",
    description: "Active Goal shows Pause and Clear.",
  },
  paused: {
    availability: "available",
    status: "paused",
    description: "Paused Goal shows Resume and Clear.",
  },
  complete: {
    availability: "available",
    status: "complete",
    description: "Complete Goal shows Reopen and Clear.",
  },
  "unsupported-agent": {
    availability: "agent_capability_missing",
    status: "active",
    description: "Save and Resume remain visible but explain why they are unavailable.",
  },
  "workflow-session": {
    availability: "available",
    status: "active",
    description: "Workflow-controlled Sessions keep the ordinary Goal controls.",
  },
  "new-chat": {
    availability: "available",
    status: null,
    description: "New Chat captures its target only when Save is activated.",
  },
  loading: {
    availability: "available",
    status: null,
    description: "Existing Goal starts with compact Loading.",
  },
  "error-retry": {
    availability: "available",
    status: null,
    description: "Observation errors expose Retry without partial state.",
  },
  "dirty-broadcast": {
    availability: "available",
    status: "active",
    description: "A dirty draft survives an authoritative broadcast.",
  },
  "overlapping-read": {
    availability: "available",
    status: "active",
    description: "A broadcast wins over a late overlapping read.",
  },
  "pending-pause": {
    availability: "available",
    status: "active",
    description: "Pause is single-flight and only the initiating action is pending.",
  },
  "pending-save": {
    availability: "available",
    status: "active",
    description: "Save is single-flight and preserves the dirty draft.",
  },
  "error-action": {
    availability: "available",
    status: "active",
    description: "Mutation errors use ordinary status notifications and restore local editing state.",
  },
  "questions-off": {
    availability: "available",
    status: "active",
    description: "Questions off does not block Goal.",
  },
  "question-picker": {
    availability: "available",
    status: "active",
    description: "Goal remains available while a Question picker is open.",
  },
  "approval-picker": {
    availability: "available",
    status: "active",
    description: "Goal remains available while an Approval picker is open.",
  },
};

export function goalFixtureFact(state: GoalFixtureState): ChatGoalFact {
  const config = goalFixtureConfigs[state];
  return {
    goal:
      config.status === null
        ? null
        : {
            id: "goal-fixture",
            objective: "Deterministic Goal fixture",
            status: config.status,
            createdAt: "2026-09-11T10:00:00.000Z",
            updatedAt: "2026-09-11T10:00:00.000Z",
          },
    availability: config.availability,
  };
}
