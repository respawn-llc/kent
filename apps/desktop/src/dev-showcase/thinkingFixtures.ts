import type { ChatTranscriptPayloadByKind, ChatTranscriptPage } from "@/api";
import type { TranscriptWindowInput } from "@/app-facade";

import { replayDurationMs, replayText } from "./thinkingReplay";

type Hydration = ChatTranscriptPayloadByKind["hydration"];
export type FixtureActivity = Hydration["RuntimeReadModelUpdate"]["Activity"];
export type FixtureActiveKind = NonNullable<FixtureActivity["ActiveStep"]>["ActiveKind"];
export const fixtureIdle: FixtureActivity = {
  State: "registered_idle",
  ActiveStep: null,
  Reviewer: "inactive",
  QueueAccepting: true,
  DiagnosticRecovery: false,
};
export const fixtureRunning: FixtureActivity = {
  ...fixtureIdle,
  State: "running",
  ActiveStep: { RunID: "fixture-run", StepID: "fixture-step", ActiveKind: "user_turn" },
};
export const fixtureKindControls: readonly FixtureActiveKind[] = [
  "user_turn",
  "workflow_turn",
  "goal_loop",
  "compaction",
  "pre_submit_compaction",
  "user_shell",
  "background",
  "runtime_maintenance",
];

export function fixtureTrace(
  position = replayText.length,
  identity = "fixture-trace",
): Hydration["ActiveReasoningTraces"][number] {
  return {
    StepID: "fixture-step",
    Identity: { Kent: identity },
    CompactText: replayText.split("\n")[0] ?? replayText,
    Text: replayText.slice(0, position),
  };
}

function fixtureUserRow(sequence: number): ChatTranscriptPayloadByKind["committed_row"] {
  return {
    Kind: "user",
    Locator: { event_sequence: sequence, row_ordinal: 1 },
    Visibility: "ongoing",
    Integrity: 0,
    User: {
      Text: `Bounded history fixture ${String(sequence)}. Scroll past the reasoning row to unmount it.`,
    },
    Assistant: null,
    Tool: null,
    ReasoningTrace: null,
    Notice: null,
    ReviewerFeedback: null,
    ReviewerError: null,
  };
}

export function fixtureReasoningRow(
  sequence: number,
  duration: number | null = replayDurationMs,
  trace = fixtureTrace(),
): ChatTranscriptPayloadByKind["committed_row"] {
  const { Identity, ...value } = trace;
  return {
    ...fixtureUserRow(sequence),
    Kind: "reasoning_trace",
    User: null,
    ReasoningTrace: { ...value, duration_ms: duration, ProvisionalIdentity: Identity },
  };
}

export function fixturePage(older: boolean): ChatTranscriptPage {
  const start = older ? 1 : 61;
  return {
    sessionID: "fixture-session",
    sessionName: null,
    conversationFreshness: 0,
    latestRollbackCandidate: null,
    entries: Array.from({ length: 60 }, (_, index) => fixtureUserRow(start + index)),
    olderCursor: older ? null : 1,
    newerCursor: older ? 61 : null,
    hasMoreAbove: !older,
    hasMoreBelow: older,
  };
}

export function fixtureHydration(
  activity = fixtureRunning,
  traces = [fixtureTrace(180)],
  entries: Hydration["TailSegment"]["Entries"] = [],
): Hydration {
  return {
    SessionIdentity: {
      SessionID: "fixture-session",
      SessionName: null,
      ConversationFreshness: 0,
      ExecutionTarget: null,
    },
    SessionStatus: {
      ReviewerFrequency: "off",
      ReviewerEnabled: true,
      AutoCompactionEnabled: true,
      QuestionsEnabled: true,
      FastModeAvailable: false,
      FastModeEnabled: false,
      ThinkingLevel: "medium",
      CompactionMode: "local",
      CompactionCount: 0,
      Workflow: null,
    },
    RuntimeReadModelUpdate: {
      Version: { Epoch: "fixture-epoch", Generation: 1, Sequence: 1 },
      Activity: activity,
    },
    TailSegment: { Entries: entries, OlderCursor: null, HasMoreAbove: false },
    ActiveThinkingStatus: null,
    ActiveReasoningTraces: traces,
    ActiveAssistant: null,
    InFlightTools: [],
    ActiveStep: null,
    ActiveCompaction: null,
    PendingPrompts: [],
    BackgroundActivities: [],
    ContextUsage: null,
    GoalStatus: null,
  };
}

export function fixtureStatus(Text: string): TranscriptWindowInput {
  return {
    kind: "live-fact",
    fact: { kind: "thinking_status_update", payload: { StepID: "fixture-step", Text } },
  };
}
