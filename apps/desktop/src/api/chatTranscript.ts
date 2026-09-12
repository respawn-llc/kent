import * as T from "@app/server-api-contract/gen/kent/api/transcript/transcript_pb";
import { ApprovalDecision } from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import {
  DirtyStateKind,
  SelectorErrorKind,
  TopologyVariant,
} from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";
import type { ChatTranscriptMessage, ChatTranscriptPayloadByKind as Payloads } from "./chatTranscriptTypes";
import type { ChatTranscriptPage } from "./chatTypes";
import { enumValue, required, safeNumber } from "./chatWire";
import {
  activeKind,
  contextUsage,
  conversationFreshness,
  executionFacts,
  readModelUpdate,
} from "./chatReadModel";
import { goalFacts } from "./chatGoal";
import { committedRow, assistantPhase, diagnostic, reasoningIdentity } from "./chatTranscriptRows";
import { toolPresentation } from "./chatToolPresentation";
import { pendingWorkKind } from "./chatMutations";
import { timestampMillis } from "./clientTime";
import { ContractError } from "./errors";

function sessionIdentity(value: T.SessionIdentity): Payloads["session_identity"] {
  return {
    SessionID: value.sessionId,
    SessionName: value.sessionName ?? null,
    ConversationFreshness: conversationFreshness(value.conversationFreshness),
    ExecutionTarget: value.executionTarget === undefined ? null : executionFacts(value.executionTarget),
  };
}

function sessionStatus(value: T.SessionStatus): Payloads["session_status"] {
  return {
    ReviewerFrequency: value.reviewerFrequency,
    ReviewerEnabled: value.reviewerEnabled,
    AutoCompactionEnabled: value.autoCompactionEnabled,
    QuestionsEnabled: value.questionsEnabled,
    FastModeAvailable: value.fastModeAvailable,
    FastModeEnabled: value.fastModeEnabled,
    ThinkingLevel: value.thinkingLevel,
    CompactionMode: value.compactionMode,
    CompactionCount: value.compactionCount,
    PreviousSessionID: value.previousSessionId ?? null,
    ParentAgentSessionID: value.parentAgentSessionId ?? null,
    NavigationTargetSessionID: value.navigationTargetSessionId ?? null,
    Workflow:
      value.workflow === undefined
        ? null
        : { TaskID: value.workflow.taskId, WorkflowID: value.workflow.workflowId },
  };
}

function thinking(value: T.ThinkingStatusUpdate): Payloads["thinking_status_update"] {
  return { StepID: value.stepId, Text: value.text };
}

function reasoning(value: T.ReasoningTraceUpdate): Payloads["reasoning_trace_update"] {
  return {
    StepID: value.stepId,
    Identity: reasoningIdentity(required(value.identity)),
    CompactText: value.compactText,
    Text: value.text,
  };
}

function step(value: T.StepState): Payloads["step_state"] {
  return {
    RunID: value.runId,
    StepID: value.stepId,
    Lifecycle: enumValue(value.lifecycle, {
      [T.StepLifecycle.STARTED]: "started",
      [T.StepLifecycle.FINISHED]: "finished",
    }),
    ActiveKind: activeKind(value.activeKind),
    Status: enumValue(value.status, {
      [T.RunStatus.RUNNING]: "running",
      [T.RunStatus.COMPLETED]: "completed",
      [T.RunStatus.INTERRUPTED]: "interrupted",
      [T.RunStatus.FAILED]: "failed",
    }),
  };
}

function compaction(value: T.CompactionStatus): Payloads["compaction_status"] {
  return {
    StepID: value.stepId,
    RequestID: value.requestId ?? null,
    State: enumValue(value.state, {
      [T.CompactionState.STARTED]: "started",
      [T.CompactionState.COMPLETED]: "completed",
      [T.CompactionState.FAILED]: "failed",
    }),
    Mode: enumValue(value.mode, {
      [T.CompactionMode.AUTO]: "auto",
      [T.CompactionMode.HANDOFF]: "handoff",
      [T.CompactionMode.MANUAL]: "manual",
      [T.CompactionMode.WORKFLOW_POST_COMPLETION]: "workflow_post_completion",
    }),
    Count: value.count,
    Diagnostic: diagnostic(value.diagnostic),
  };
}

function tool(value: T.ToolStart): Payloads["tool_start"] {
  return {
    StepID: value.stepId,
    ToolCallID: value.toolCallId,
    ToolName: value.toolName,
    Presentation: toolPresentation(value.presentation, value.toolName),
  };
}

function background(value: T.BackgroundActivity): Payloads["background_activity"] {
  return {
    ActivityID: value.activityId,
    ProcessID: value.processId,
    OwnerRunID: value.ownerRunId,
    OwnerStepID: value.ownerStepId,
    Lifecycle: enumValue(value.lifecycle, {
      [T.BackgroundLifecycle.BACKGROUNDED]: "backgrounded",
      [T.BackgroundLifecycle.COMPLETED]: "completed",
      [T.BackgroundLifecycle.KILLED]: "killed",
    }),
    Command: value.command,
    Workdir: value.workdir,
    LogPath: value.logPath ?? null,
    Preview: value.preview ?? null,
    ExitCode: value.exitCode ?? null,
    UserRequestedKill: value.userRequestedKill,
    NoticeSuppressed: value.noticeSuppressed,
    Diagnostic: diagnostic(value.diagnostic),
  };
}

function prompt(value: T.Prompt): Payloads["prompt"] {
  const branch = value.prompt;
  if (branch.case === undefined) throw new ContractError("Transcript prompt is missing.");
  const common = {
    Kind: branch.case,
    State: enumValue(value.status, {
      [T.PromptStatus.PENDING]: "pending",
      [T.PromptStatus.RESOLVED]: "resolved",
    }),
    ToolCallID: branch.value.toolCallId,
    SessionID: branch.value.sessionId,
    StepID: branch.value.stepId,
    CreatedAt: new Date(timestampMillis(required(branch.value.createdAt))).toISOString(),
  };
  if (branch.case === "question")
    return {
      ...common,
      Question: branch.value.question,
      Suggestions: [...branch.value.suggestions],
      RecommendedOptionIndex: branch.value.recommendedOptionIndex ?? null,
      ApprovalOptions: [],
      AccessTargets: [],
    };
  return {
    ...common,
    Question: branch.value.question ?? "",
    Suggestions: [],
    RecommendedOptionIndex: null,
    ApprovalOptions: branch.value.options.map((option) => ({
      Decision: enumValue(option.decision, {
        [ApprovalDecision.ALLOW_ONCE]: "allow_once",
        [ApprovalDecision.ALLOW_SESSION]: "allow_session",
        [ApprovalDecision.DENY]: "deny",
      }),
      Label: option.label,
    })),
    AccessTargets: branch.value.accessTargets.map((target) => ({
      RequestedPath: target.requestedPath,
      ResolvedPath: target.resolvedPath,
    })),
  };
}

function hydration(value: T.Hydration): Payloads["hydration"] {
  const tail = required(value.tailSegment);
  return {
    SessionIdentity: sessionIdentity(required(value.sessionIdentity)),
    SessionStatus: sessionStatus(required(value.sessionStatus)),
    RuntimeReadModelUpdate: readModelUpdate(required(value.runtimeReadModelUpdate)),
    TailSegment: {
      OlderCursor: tail.olderCursor === undefined ? null : safeNumber(tail.olderCursor),
      HasMoreAbove: tail.hasMoreAbove,
      Entries: tail.entries.map(committedRow),
    },
    ActiveAssistant:
      value.activeAssistant === undefined
        ? null
        : {
            StepID: value.activeAssistant.stepId,
            StreamID: value.activeAssistant.streamId,
            Phase: assistantPhase(value.activeAssistant.phase),
            Text: value.activeAssistant.text,
          },
    ActiveThinkingStatus:
      value.activeThinkingStatus === undefined ? null : thinking(value.activeThinkingStatus),
    ActiveReasoningTraces: value.activeReasoningTraces.map(reasoning),
    ActiveStep: value.activeStep === undefined ? null : step(value.activeStep),
    ActiveCompaction: value.activeCompaction === undefined ? null : compaction(value.activeCompaction),
    InFlightTools: value.inFlightTools.map(tool),
    PendingPrompts: value.pendingPrompts.map(prompt),
    BackgroundActivities: value.backgroundActivities.map(background),
    ContextUsage: value.contextUsage === undefined ? null : contextUsage(value.contextUsage),
    GoalStatus: value.goalStatus === undefined ? null : goalFacts(value.goalStatus),
  };
}

function worktreeOutcome(value: T.WorktreeTransitionOutcome): Payloads["worktree_transition_outcome"] {
  const failure = value.failureDetail;
  const dirty =
    value.deletePrecondition === undefined ? undefined : required(value.deletePrecondition.dirtyState);
  return {
    OperationID: value.operationId,
    Transition: enumValue(value.transition, {
      [T.WorktreeTransitionKind.ENTER]: "enter",
      [T.WorktreeTransitionKind.LEAVE]: "leave",
      [T.WorktreeTransitionKind.DELETE]: "delete",
    }),
    State: enumValue(value.state, {
      [T.WorktreeTransitionState.COMPLETED]: "completed",
      [T.WorktreeTransitionState.FAILED]: "failed",
    }),
    Failure: failure.case === "failure" ? diagnostic(failure.value) : null,
    SelectorError:
      failure.case !== "selectorError"
        ? null
        : {
            kind: enumValue(failure.value.kind, {
              [SelectorErrorKind.WORKTREE_SELECTOR_ERROR_KIND_NOT_FOUND]: 1,
              [SelectorErrorKind.WORKTREE_SELECTOR_ERROR_KIND_AMBIGUOUS]: 2,
              [SelectorErrorKind.WORKTREE_SELECTOR_ERROR_KIND_UNAVAILABLE]: 3,
            }),
            input: failure.value.input,
            candidates: failure.value.candidates.map((candidate) => ({
              variant: enumValue(candidate.variant, {
                [TopologyVariant.WORKTREE_TOPOLOGY_VARIANT_MAIN_WORKSPACE]: 1,
                [TopologyVariant.WORKTREE_TOPOLOGY_VARIANT_REGISTERED]: 2,
                [TopologyVariant.WORKTREE_TOPOLOGY_VARIANT_EXTERNAL]: 3,
              }),
              selector: candidate.selector,
              fallback_identity: candidate.fallbackIdentity,
              ...(candidate.branchName === undefined ? {} : { branch_name: candidate.branchName }),
              ...(candidate.displayName === undefined ? {} : { display_name: candidate.displayName }),
            })),
          },
    DeletePrecondition:
      dirty === undefined
        ? null
        : {
            kind: enumValue(dirty.kind, {
              [DirtyStateKind.DIRTY_STATE_DIRTY]: "dirty",
              [DirtyStateKind.DIRTY_STATE_UNKNOWN]: "unknown",
            }),
            ...(dirty.dirtyFileCount === undefined ? {} : { dirty_file_count: dirty.dirtyFileCount }),
            ...(dirty.unknownCause === undefined ? {} : { unknown_cause: dirty.unknownCause }),
          },
  };
}

export function transcriptMessage(value: T.Message, sessionID: string): ChatTranscriptMessage {
  const event = required(value.event).payload;
  const sequence = safeNumber(value.sequence);
  if ((event.case === "hydration" && sequence !== 1) || (event.case !== "hydration" && sequence < 2))
    throw new ContractError("Transcript sequence does not match event kind.");
  if (event.case === undefined) throw new ContractError("Transcript event payload is missing.");
  return projectEvent(event, sequence, sessionID);
}

type EventKind = Exclude<T.Event["payload"]["case"], undefined>;
type EventValue<Kind extends EventKind> = Extract<T.Event["payload"], { case: Kind }>["value"];
type PresentEvent<Kind extends EventKind> = {
  [Key in Kind]: Readonly<{ case: Key; value: EventValue<Key> }>;
}[Kind];

function projectEvent<Kind extends EventKind>(
  event: PresentEvent<Kind>,
  sequence: number,
  sessionID: string,
): ChatTranscriptMessage {
  return eventProjections[event.case](event.value, sequence, sessionID);
}

const eventProjections: {
  [Kind in EventKind]: (
    value: EventValue<Kind>,
    sequence: number,
    sessionID: string,
  ) => ChatTranscriptMessage;
} = {
  hydration: (value, sequence, sessionID) => {
    if (required(value.sessionIdentity).sessionId !== sessionID)
      throw new ContractError("Transcript hydration does not match the requested Session.");
    return { sequence, kind: "hydration", payload: hydration(value) };
  },
  committedRow: (value, sequence) => ({ sequence, kind: "committed_row", payload: committedRow(value) }),
  assistantDelta: (value, sequence) => ({
    sequence,
    kind: "assistant_delta",
    payload: {
      StepID: value.stepId,
      StreamID: value.streamId,
      Phase: assistantPhase(value.phase),
      Delta: value.delta,
    },
  }),
  assistantStreamAbort: (value, sequence) => ({
    sequence,
    kind: "assistant_stream_abort",
    payload: {
      StepID: value.stepId,
      StreamID: value.streamId,
      Reason: enumValue(value.reason, {
        [T.AssistantAbortReason.INTERRUPTED]: "interrupted",
        [T.AssistantAbortReason.FAILED]: "failed",
        [T.AssistantAbortReason.SUPERSEDED]: "superseded",
      }),
      Diagnostic: diagnostic(value.diagnostic),
    },
  }),
  thinkingStatusUpdate: (value, sequence) => ({
    sequence,
    kind: "thinking_status_update",
    payload: thinking(value),
  }),
  reasoningTraceUpdate: (value, sequence) => ({
    sequence,
    kind: "reasoning_trace_update",
    payload: reasoning(value),
  }),
  reasoningTraceReset: (value, sequence) => ({
    sequence,
    kind: "reasoning_trace_reset",
    payload: { StepID: value.stepId },
  }),
  toolStart: (value, sequence) => ({ sequence, kind: "tool_start", payload: tool(value) }),
  toolAbort: (value, sequence) => ({
    sequence,
    kind: "tool_abort",
    payload: {
      StepID: value.stepId,
      ToolCallID: value.toolCallId,
      Reason: enumValue(value.reason, {
        [T.ToolAbortReason.CANCELED]: "canceled",
        [T.ToolAbortReason.FAILED]: "failed",
      }),
      Diagnostic: diagnostic(value.diagnostic),
    },
  }),
  userMessageFlushed: (value, sequence) => ({
    sequence,
    kind: "user_message_flushed",
    payload: { StepID: value.stepId ?? null },
  }),
  queuedMessageState: (value, sequence) => ({
    sequence,
    kind: "queued_message_state",
    payload: {
      QueueItemID: value.queueItemId,
      Status: enumValue(value.status, {
        [T.QueuedMessageStatus.ACCEPTED]: "accepted",
        [T.QueuedMessageStatus.SUBMITTED]: "submitted",
        [T.QueuedMessageStatus.FAILED]: "failed",
        [T.QueuedMessageStatus.DISCARDED]: "discarded",
      }),
      FailureReason:
        value.failureReason === undefined
          ? null
          : enumValue(value.failureReason, {
              [T.QueuedMessageFailureReason.CLOSING]: "closing",
              [T.QueuedMessageFailureReason.TERMINAL_WORKFLOW_COMPLETION]: "terminal_workflow_completion",
              [T.QueuedMessageFailureReason.RUNTIME_UNAVAILABLE]: "runtime_unavailable",
            }),
      Text: value.text ?? null,
    },
  }),
  pendingWorkChanged: (_, sequence) => ({ sequence, kind: "pending_work_changed", payload: {} }),
  pendingWorkRestored: (value, sequence) => {
    const restoration = required(value.restoration);
    return {
      sequence,
      kind: "pending_work_restored",
      payload: {
        Restoration: {
          ItemID: restoration.itemId,
          Kind: pendingWorkKind(restoration.kind),
          CanonicalInput: restoration.canonicalInput,
        },
      },
    };
  },
  humanInputInterrupted: (value, sequence) => ({
    sequence,
    kind: "human_input_interrupted",
    payload: { Items: value.items.map((item) => ({ QueueItemID: item.queueItemId, Text: item.text })) },
  }),
  sessionSettingFeedback: (value, sequence) => {
    const field = value.value;
    return {
      sequence,
      kind: "session_setting_feedback",
      payload: {
        Kind: enumValue(value.kind, {
          [T.SessionSettingKind.SESSION_NAME]: "session_name",
          [T.SessionSettingKind.THINKING]: "thinking",
          [T.SessionSettingKind.FAST_MODE]: "fast_mode",
          [T.SessionSettingKind.SUPERVISOR]: "supervisor",
          [T.SessionSettingKind.QUESTIONS]: "questions",
          [T.SessionSettingKind.AUTO_COMPACTION]: "auto_compaction",
        }),
        Changed: value.changed,
        SessionName: field.case === "sessionName" ? field.value : null,
        Thinking: field.case === "thinking" ? field.value : null,
        FastMode: field.case === "fastMode" ? field.value : null,
        Supervisor: field.case === "supervisor" ? field.value : null,
        Questions: field.case === "questions" ? field.value : null,
        AutoCompaction: field.case === "autoCompaction" ? field.value : null,
      },
    };
  },
  stepState: (value, sequence) => ({ sequence, kind: "step_state", payload: step(value) }),
  runtimeReadModelUpdate: (value, sequence) => ({
    sequence,
    kind: "runtime_read_model_update",
    payload: readModelUpdate(value),
  }),
  sessionStatus: (value, sequence) => ({ sequence, kind: "session_status", payload: sessionStatus(value) }),
  sessionIdentity: (value, sequence, sessionID) => {
    if (value.sessionId !== sessionID)
      throw new ContractError("Transcript identity does not match the requested Session.");
    return { sequence, kind: "session_identity", payload: sessionIdentity(value) };
  },
  compactionStatus: (value, sequence) => ({
    sequence,
    kind: "compaction_status",
    payload: compaction(value),
  }),
  contextUsage: (value, sequence) => ({ sequence, kind: "context_usage", payload: contextUsage(value) }),
  goalStatus: (value, sequence) => ({ sequence, kind: "goal_status", payload: goalFacts(value) }),
  backgroundActivity: (value, sequence) => ({
    sequence,
    kind: "background_activity",
    payload: background(value),
  }),
  prompt: (value, sequence) => ({ sequence, kind: "prompt", payload: prompt(value) }),
  worktreeTransitionOutcome: (value, sequence) => ({
    sequence,
    kind: "worktree_transition_outcome",
    payload: worktreeOutcome(value),
  }),
  operationalDiagnostic: (value, sequence) => ({
    sequence,
    kind: "operational_diagnostic",
    payload: {
      Code: enumValue(value.code, {
        [T.OperationalDiagnosticCode.SLEEP_GUARD_FAILED]: "sleep_guard_failed",
        [T.OperationalDiagnosticCode.PROMPT_HISTORY_PERSIST_FAILED]: "prompt_history_persist_failed",
        [T.OperationalDiagnosticCode.CONTEXT_FACTS_PERSIST_FAILED]: "context_facts_persist_failed",
        [T.OperationalDiagnosticCode.IN_FLIGHT_CLEAR_FAILED]: "in_flight_clear_failed",
        [T.OperationalDiagnosticCode.PROVIDER_TURN_STATE_INVALID]: "provider_turn_state_invalid",
      }),
      StepID: value.stepId ?? null,
      Detail: value.detail,
    },
  }),
  liveRunFinished: (value, sequence) => ({
    sequence,
    kind: "live_run_finished",
    payload: {
      Status: enumValue(value.status, {
        [T.LiveRunStatus.COMPLETED]: "completed",
        [T.LiveRunStatus.INTERRUPTED]: "interrupted",
        [T.LiveRunStatus.FAILED]: "failed",
      }),
      ResultKind: enumValue(value.resultKind, {
        [T.LiveRunResultKind.ASSISTANT_FINAL_ANSWER]: "assistant_final_answer",
        [T.LiveRunResultKind.NO_FINAL_ANSWER]: "no_final_answer",
      }),
      NoFinalReason: value.noFinalReason,
      WorkPerformed: value.workPerformed,
      FinalAnswer: value.finalAnswer ?? null,
      Failure: value.failure ?? null,
      StartedAt: new Date(timestampMillis(required(value.startedAt))).toISOString(),
      FinishedAt: new Date(timestampMillis(required(value.finishedAt))).toISOString(),
    },
  }),
};

export function transcriptPage(value: T.Page, sessionID: string): ChatTranscriptPage {
  if (value.sessionId !== sessionID)
    throw new ContractError("Transcript page does not match the requested Session.");
  return {
    sessionID: value.sessionId,
    sessionName: value.sessionName ?? null,
    conversationFreshness: conversationFreshness(value.conversationFreshness),
    olderCursor: value.olderCursor === undefined ? null : safeNumber(value.olderCursor),
    hasMoreAbove: value.hasMoreAbove,
    newerCursor: value.newerCursor === undefined ? null : safeNumber(value.newerCursor),
    hasMoreBelow: value.hasMoreBelow,
    latestRollbackCandidate:
      value.latestRollbackCandidate === undefined
        ? null
        : {
            user_message_seq: safeNumber(value.latestRollbackCandidate.userMessageSeq),
            candidate_page_end_byte: safeNumber(value.latestRollbackCandidate.candidatePageEndByte),
          },
    entries: value.entries.map(committedRow),
  };
}
