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
} from "@/app-facade";

export const target: ChatSessionTarget = {
  projectID: "project-1",
  workspace: { workspaceID: "workspace-1" },
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
        lastCommittedAssistantFinalAnswer: null,
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

export function createFixtureChat(
  getMainView: ChatRuntimeApi["getMainView"],
  getTranscriptPage: ChatRuntimeApi["getTranscriptPage"],
) {
  const handlers: ChatTranscriptHandler[] = [];
  const live = new Set<ChatTranscriptHandler>();
  const subscribeTranscript: ChatRuntimeApi["subscribeTranscript"] = (_target, handler) => {
    handlers.push(handler);
    live.add(handler);
    return {
      close: () => {
        live.delete(handler);
      },
    };
  };
  const api: ChatRuntimeApi = { getMainView, getTranscriptPage, subscribeTranscript };
  return {
    api,
    handlers,
    open: () => {
      for (const handler of live) handler.onOpen?.();
    },
    emit: (event: Parameters<ChatTranscriptHandler["onEvent"]>[0]) => {
      for (const handler of live) handler.onEvent(event);
    },
  };
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
