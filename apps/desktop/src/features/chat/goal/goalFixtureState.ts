import type { ChatGoalAvailability, ChatGoalFact, ChatGoalStatus } from "@/api";

export const goalFixtureStates = [
  "absent",
  "active",
  "paused",
  "complete",
  "unsupported-agent",
  "workflow-session",
  "new-chat",
  "new-chat-loss",
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
export type GoalFixtureConfig = Readonly<{
  availability: ChatGoalAvailability;
  mode: "exact" | "new_chat";
  picker: "none" | "question" | "approval";
  status: ChatGoalStatus | null;
  observation: "ready" | "loading" | "error";
  mutation: "success" | "pending" | "error";
  description: string;
}>;

export const goalFixtureConfigs: Record<GoalFixtureState, GoalFixtureConfig> = {
  absent: {
    availability: "available",
    mode: "exact",
    picker: "none",
    status: null,
    observation: "ready",
    mutation: "success",
    description: "Absent Goal opens the focused create editor.",
  },
  active: {
    availability: "available",
    mode: "exact",
    picker: "none",
    status: "active",
    observation: "ready",
    mutation: "success",
    description: "Active Goal shows Pause and Clear.",
  },
  paused: {
    availability: "available",
    mode: "exact",
    picker: "none",
    status: "paused",
    observation: "ready",
    mutation: "success",
    description: "Paused Goal shows Resume and Clear.",
  },
  complete: {
    availability: "available",
    mode: "exact",
    picker: "none",
    status: "complete",
    observation: "ready",
    mutation: "success",
    description: "Complete Goal shows Reopen and Clear.",
  },
  "unsupported-agent": {
    availability: "agent_capability_missing",
    mode: "exact",
    picker: "none",
    status: "active",
    observation: "ready",
    mutation: "success",
    description: "Unavailable Goal actions keep their labels and explain the reason.",
  },
  "workflow-session": {
    availability: "available",
    mode: "exact",
    picker: "none",
    status: "active",
    observation: "ready",
    mutation: "success",
    description: "Workflow-controlled Sessions keep the ordinary Goal controls.",
  },
  "new-chat": {
    availability: "available",
    mode: "new_chat",
    picker: "none",
    status: null,
    observation: "ready",
    mutation: "success",
    description: "New Chat captures its target only when Save is activated.",
  },
  "new-chat-loss": {
    availability: "available",
    mode: "new_chat",
    picker: "none",
    status: null,
    observation: "ready",
    mutation: "pending",
    description: "Ambiguous loss preserves the New Chat draft and reports the failure.",
  },
  loading: {
    availability: "available",
    mode: "exact",
    picker: "none",
    status: "active",
    observation: "loading",
    mutation: "success",
    description: "Existing Goal starts with compact Loading until hydration is released.",
  },
  "error-retry": {
    availability: "available",
    mode: "exact",
    picker: "none",
    status: "active",
    observation: "error",
    mutation: "success",
    description: "Observation errors expose Retry without partial state.",
  },
  "dirty-broadcast": {
    availability: "available",
    mode: "exact",
    picker: "none",
    status: "active",
    observation: "ready",
    mutation: "success",
    description: "A dirty draft survives an authoritative broadcast.",
  },
  "overlapping-read": {
    availability: "available",
    mode: "exact",
    picker: "none",
    status: "active",
    observation: "ready",
    mutation: "success",
    description: "A broadcast wins over a late overlapping read.",
  },
  "pending-pause": {
    availability: "available",
    mode: "exact",
    picker: "none",
    status: "active",
    observation: "ready",
    mutation: "pending",
    description: "Pause changes the visible status and remains single-flight until resolved.",
  },
  "pending-save": {
    availability: "available",
    mode: "exact",
    picker: "none",
    status: "active",
    observation: "ready",
    mutation: "pending",
    description: "Save renders the submitted Markdown while the request is unresolved.",
  },
  "error-action": {
    availability: "available",
    mode: "exact",
    picker: "none",
    status: "active",
    observation: "ready",
    mutation: "error",
    description: "Mutation errors report through status notifications and restore the editor.",
  },
  "questions-off": {
    availability: "available",
    mode: "new_chat",
    picker: "none",
    status: null,
    observation: "ready",
    mutation: "success",
    description: "Questions off does not block Goal.",
  },
  "question-picker": {
    availability: "available",
    mode: "exact",
    picker: "question",
    status: "active",
    observation: "ready",
    mutation: "success",
    description: "Goal remains available while a Question picker is open.",
  },
  "approval-picker": {
    availability: "available",
    mode: "exact",
    picker: "approval",
    status: "active",
    observation: "ready",
    mutation: "success",
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
