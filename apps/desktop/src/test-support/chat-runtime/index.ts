import { vi, type Mock } from "vitest";

import type {
  ChatMainViewRead,
  ChatSessionTarget,
  ChatTranscriptHandler,
  ChatTranscriptPage,
  ChatTranscriptPayloadByKind,
} from "@/api";
import {
  emptyChatProjectionState,
  reduceChatProjection,
  type ChatProjectionState,
  type ChatRuntimeApi,
  type ChatRuntimeHost,
} from "@/app-facade";
import { row } from "@/test-support/transcript-window";

export const target: ChatSessionTarget = {
  projectID: "project-1",
  sessionID: "123e4567-e89b-42d3-a456-426614174000",
};

export function mainViewRead(sequence = 1): ChatMainViewRead {
  return {
    mainView: {
      version: { epoch: "epoch-1", generation: 1, sequence },
      sessionID: target.sessionID,
      sessionName: "Session",
      executionTarget: {
        workspaceID: "workspace-1",
        workspaceName: "Workspace",
        workspaceRoot: "/workspace",
        workspaceAvailability: "available",
        worktree: null,
        cwdRelpath: ".",
        effectiveWorkdir: "/workspace",
      },
      activity: runtimeActivity("registered_idle"),
      status: {
        reviewerFrequency: "off",
        reviewerEnabled: false,
        autoCompactionEnabled: true,
        questionsEnabled: true,
        fastModeAvailable: false,
        fastModeEnabled: false,
        conversationFreshness: 0,
        previousSessionID: null,
        parentAgentSessionID: null,
        navigationTargetSessionID: null,
        thinkingLevel: "medium",
        compactionMode: "local",
        contextUsage: {
          usedTokens: 10,
          windowTokens: 100,
          cacheHitPercent: 0,
          hasCacheHitPercentage: false,
        },
        compactionCount: 0,
        workflowSession: null,
      },
    },
    goal: { goal: null, availability: "available" },
  };
}

export function hydration(): ChatTranscriptPayloadByKind["hydration"] {
  return {
    SessionIdentity: {
      SessionID: target.sessionID,
      SessionName: null,
      ConversationFreshness: 1,
      ExecutionTarget: null,
    },
    SessionStatus: {
      ReviewerFrequency: "edits",
      ReviewerEnabled: true,
      AutoCompactionEnabled: false,
      QuestionsEnabled: false,
      FastModeAvailable: true,
      FastModeEnabled: true,
      ThinkingLevel: "high",
      CompactionMode: "native",
      CompactionCount: 2,
      PreviousSessionID: null,
      ParentAgentSessionID: null,
      NavigationTargetSessionID: null,
      Workflow: null,
    },
    RuntimeReadModelUpdate: {
      Version: { Epoch: "epoch-1", Generation: 1, Sequence: 2 },
      Activity: {
        State: "running",
        ActiveStep: {
          RunID: "223e4567-e89b-42d3-a456-426614174000",
          StepID: "323e4567-e89b-42d3-a456-426614174000",
          ActiveKind: "user_turn",
        },
        Reviewer: "inactive",
        QueueAccepting: true,
        DiagnosticRecovery: false,
      },
    },
    TailSegment: { OlderCursor: null, HasMoreAbove: false, Entries: [] },
    ActiveAssistant: null,
    ActiveThinkingStatus: null,
    ActiveReasoningTraces: [],
    ActiveStep: null,
    ActiveCompaction: null,
    InFlightTools: [],
    PendingPrompts: [],
    BackgroundActivities: [],
    ContextUsage: null,
    GoalStatus: {
      Goal: null,
      Availability: "agent_capability_missing",
    },
  };
}

export function hydrationWithCursor(cursor: number): ChatTranscriptPayloadByKind["hydration"] {
  return {
    ...hydration(),
    TailSegment: { OlderCursor: cursor, HasMoreAbove: true, Entries: [] },
  };
}

export function incompatibleHydration(
  sessionName: string,
  text: string,
): ChatTranscriptPayloadByKind["hydration"] {
  const payload = hydration();
  return {
    ...payload,
    SessionIdentity: { ...payload.SessionIdentity, SessionName: sessionName },
    TailSegment: {
      Entries: [{ ...row(10), User: { Text: text } }],
      OlderCursor: null,
      HasMoreAbove: false,
    },
  };
}

export function seededProjection(sequence = 1): ChatProjectionState {
  return reduceChatProjection(emptyChatProjectionState(), {
    kind: "authoritative-read",
    read: mainViewRead(sequence),
    metadataRevisionAtStart: 0,
    goalGenerationAtStart: 0,
    currentGoalGeneration: 0,
  }).state;
}

export function runtimeUpdate(
  sequence: number,
  generation: number,
  state: "registered_idle" | "running" | "closing",
) {
  return {
    sequence,
    kind: "runtime_read_model_update",
    payload: {
      Version: { Epoch: "epoch-1", Generation: generation, Sequence: sequence },
      Activity: {
        State: state,
        ActiveStep:
          state === "running"
            ? {
                RunID: "223e4567-e89b-42d3-a456-426614174000",
                StepID: "323e4567-e89b-42d3-a456-426614174000",
                ActiveKind: "user_turn" as const,
              }
            : null,
        Reviewer: "inactive",
        QueueAccepting: state !== "closing",
        DiagnosticRecovery: false,
      },
    },
  } as const;
}

export function runtimeUnavailablePayload(): ChatTranscriptPayloadByKind["runtime_read_model_update"] {
  return {
    Version: { Epoch: "epoch-1", Generation: 1, Sequence: 3 },
    Activity: {
      State: "unavailable",
      ActiveStep: null,
      Reviewer: "inactive",
      QueueAccepting: false,
      DiagnosticRecovery: false,
    },
  };
}

export function changedIdentity(): ChatTranscriptPayloadByKind["session_identity"] {
  return {
    SessionID: target.sessionID,
    SessionName: null,
    ConversationFreshness: 1,
    ExecutionTarget: {
      WorkspaceID: "workspace-2",
      WorkspaceName: "Workspace 2",
      WorkspaceRoot: "/workspace-2",
      WorkspaceAvailability: "missing",
      Worktree: {
        ID: "worktree-2",
        Name: "Detached",
        Root: "/workspace-2/worktree",
        Availability: "missing",
      },
      CwdRelpath: "src",
      EffectiveWorkdir: "/workspace-2/worktree/src",
    },
  };
}

export function goalStatus(): ChatTranscriptPayloadByKind["goal_status"] {
  return {
    Goal: {
      id: "goal-2",
      objective: "new Goal",
      status: "active",
      created_at: "2026-09-04T10:00:00Z",
      updated_at: "2026-09-04T10:00:00Z",
      Suspended: true,
    },
    Availability: null,
  };
}

export function worktreeOutcome(): ChatTranscriptPayloadByKind["worktree_transition_outcome"] {
  return {
    OperationID: "operation-1",
    Transition: "enter",
    State: "completed",
    Failure: null,
    SelectorError: null,
    DeletePrecondition: null,
  };
}

export function transcriptPage(olderCursor: number | null): ChatTranscriptPage {
  return {
    sessionID: target.sessionID,
    sessionName: null,
    conversationFreshness: 0,
    olderCursor,
    hasMoreAbove: olderCursor !== null,
    newerCursor: null,
    hasMoreBelow: false,
    latestRollbackCandidate: null,
    entries: [],
  };
}

export function runtimeHost(effects: Omit<ChatRuntimeHost, "logger"> = {}): ChatRuntimeHost {
  return {
    logger: { append: vi.fn().mockResolvedValue(undefined) },
    ...effects,
  };
}

export function runtimeApi({
  reads = [Promise.resolve(mainViewRead())],
  pages = [Promise.resolve(transcriptPage(null))],
}: Readonly<{
  reads?: readonly Promise<ChatMainViewRead>[];
  pages?: readonly Promise<ChatTranscriptPage>[];
}> = {}): Readonly<{
  api: ChatRuntimeApi;
  getMainView: Mock<ChatRuntimeApi["getMainView"]>;
  getTranscriptPage: Mock<ChatRuntimeApi["getTranscriptPage"]>;
  subscribeTranscript: Mock<ChatRuntimeApi["subscribeTranscript"]>;
  handlers: readonly ChatTranscriptHandler[];
}> {
  let readIndex = 0;
  let pageIndex = 0;
  const handlers: ChatTranscriptHandler[] = [];
  const getMainView = vi.fn(async () => {
    const read = reads[readIndex++];
    return read ?? Promise.reject(new Error("Unexpected Main View call."));
  });
  const getTranscriptPage = vi.fn(async () => {
    const page = pages[pageIndex++];
    return page ?? Promise.reject(new Error("Unexpected transcript page call."));
  });
  const subscribeTranscript = vi.fn((_target: ChatSessionTarget, handler: ChatTranscriptHandler) => {
    handlers.push(handler);
    return { close: vi.fn() };
  });
  const api: ChatRuntimeApi = { getMainView, getTranscriptPage, subscribeTranscript };
  return { api, getMainView, getTranscriptPage, subscribeTranscript, handlers };
}

export function deferred<Value>() {
  let resolve!: (value: Value) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<Value>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

export function requireValue<Value>(value: Value | undefined): Value {
  if (value === undefined) throw new Error("Required test value is missing.");
  return value;
}

function runtimeActivity(state: "registered_idle") {
  return {
    state,
    activeStep: null,
    reviewer: "inactive" as const,
    queueAccepting: true,
    diagnosticRecovery: false,
  };
}
