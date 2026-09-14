export type ChatWebSearchDetail = Readonly<{
  action:
    | Readonly<{ kind: "search"; queries: readonly string[] }>
    | Readonly<{ kind: "open-page"; url: string | null }>
    | Readonly<{ kind: "find-in-page"; url: string | null; pattern: string | null }>;
  results: readonly Readonly<{ kind: "link" | "image"; title: string | null; destination: string | null }>[];
  sources: readonly string[];
}>;

export type ChatDiagnostic = Readonly<{ Code: string; Detail: string }>;
export type ChatExecutionFacts = Readonly<{
  WorkspaceID: string | null;
  WorkspaceName: string;
  WorkspaceRoot: string;
  WorkspaceAvailability: "available" | "missing" | "inaccessible" | "unlinked";
  Worktree: Readonly<{ ID: string; Name: string; Root: string; Availability: string }> | null;
  CwdRelpath: string;
  EffectiveWorkdir: string;
}>;
export type ChatActiveKind =
  | "user_turn"
  | "workflow_turn"
  | "goal_loop"
  | "compaction"
  | "pre_submit_compaction"
  | "user_shell"
  | "background"
  | "runtime_maintenance";
export type ChatActivityFacts = Readonly<{
  State:
    "unavailable" | "registered_idle" | "starting" | "running" | "awaiting_prompt" | "draining" | "closing";
  ActiveStep: Readonly<{ RunID: string; StepID: string; ActiveKind: ChatActiveKind }> | null;
  Reviewer: "inactive" | "invoking" | "addressing_feedback";
  QueueAccepting: boolean;
  DiagnosticRecovery: boolean;
}>;
export type ChatGoalFacts = Readonly<{
  Goal: Readonly<{
    id: string;
    objective: string;
    status: "active" | "paused" | "complete";
    created_at: string;
    updated_at: string;
    Suspended: boolean;
  }> | null;
  Availability: "available" | "agent_capability_missing" | null;
}>;
export type ChatPatchPath = Readonly<{ Absolute: string; Relative: string }>;
export type ChatPatchGroup = Readonly<{
  Lines: readonly Readonly<{ Kind: "added" | "removed"; Content: string }>[];
}>;
export type ChatPatchDeletion = Readonly<{
  id: Readonly<{ hunk_ordinal: number }>;
  disposition: Readonly<{
    physical_group: Readonly<{ first_operation: Readonly<{ hunk_ordinal: number }> }>;
    removed: number;
  }> | null;
}>;
export type ChatPatchOperation =
  | Readonly<{ Kind: "add" | "update"; Source: null; Groups: readonly ChatPatchGroup[]; Deletion: null }>
  | Readonly<{ Kind: "move"; Source: ChatPatchPath; Groups: readonly ChatPatchGroup[]; Deletion: null }>
  | Readonly<{
      Kind: "delete";
      Source: null;
      Groups: readonly ChatPatchGroup[];
      Deletion: ChatPatchDeletion;
    }>;
export type ChatPatchPresentation =
  | Readonly<{ Variant: "invalid_input"; InvalidInput: Readonly<{ InputDetail: string }> }>
  | Readonly<{
      Variant: "changes";
      InvalidInput: null;
      Files: readonly Readonly<{
        Path: ChatPatchPath;
        Added: number;
        Removed: number | null;
        Operations: readonly ChatPatchOperation[];
      }>[];
    }>;
export type ChatToolPresentation = Readonly<{
  ToolName: string;
  Presentation: "default" | "shell" | "ask_question";
  RenderBehavior: string;
  IsShell: boolean;
  UserInitiated: boolean;
  Command: string;
  CompactText: string;
  InlineMeta: string;
  TimeoutLabel: string;
  PatchPresentation: ChatPatchPresentation | null;
  RenderHint?: Readonly<{ Kind: string; Path: string; ResultOnly: boolean; ShellDialect: string }> | null;
  Question: string;
  Suggestions: readonly string[];
  RecommendedOptionIndex: number;
  OmitSuccessfulResult: boolean;
  RawOutputRequested: boolean;
  OutputTruncated: boolean;
  MovedToBackground: boolean;
  ShellExitCode?: number | null;
}>;
export type ChatReasoningIdentity = Readonly<{
  Provider?: Readonly<{ ItemID: string; SummaryIndex: number }> | null;
  Kent?: string | null;
}>;
export type ChatNotice = Readonly<{
  StepID?: string | null;
  Reason:
    | "cache_warning"
    | "compaction"
    | "legacy_untyped_notice"
    | "runtime_diagnostic"
    | "tool_output_repair"
    | "provider_model_mismatch"
    | "thinking_update";
  Severity: "info" | "warning" | "error";
  MessageType?: string | null;
  LegacyText?: string | null;
  ThinkingEffort?: string | null;
  NoticeID?: string | null;
  SourcePath?: string | null;
  Worktree?: Readonly<{
    Branch?: string | null;
    WorktreePath: string;
    WorkspaceRoot: string;
    EffectiveCwd: string;
  }> | null;
  CacheWarning?: Readonly<{
    Scope: string;
    Reason: string;
    LostInputTokens?: number | null;
    Visibility: string;
  }> | null;
  Compaction?: Readonly<{ Count?: number | null; Detail?: string | null }> | null;
  ToolOutputRepair?: Readonly<{ kind: "fresh_resource" | "live_provider_rejection"; count: number }> | null;
  ProviderModelMismatch?: Readonly<{ requested_model: string; served_model: string }> | null;
  Diagnostic?: ChatDiagnostic | null;
  Background?: Readonly<{ ActivityID: string; ProcessID: string; ExitCode?: number | null }> | null;
  CondensedText?: string | null;
  CompactLabel?: string | null;
}>;
export type ChatCommittedRow = Readonly<{
  Visibility: "ongoing" | "ongoing_collapsed" | "detail" | "hidden";
  Integrity: 0 | 1 | 2;
  Kind: "user" | "assistant" | "tool" | "reasoning_trace" | "notice" | "reviewer_feedback" | "reviewer_error";
  Locator: Readonly<{ event_sequence: number; row_ordinal: number }>;
  User: Readonly<{
    StepID?: string | null;
    Text: string;
    CondensedText?: string | null;
    RollbackTargetID?: string | null;
    committed_at_unix_ms?: number | null;
  }> | null;
  Assistant: Readonly<{
    StepID: string;
    StreamID?: string | null;
    Text: string;
    CondensedText?: string | null;
    Phase: "commentary" | "final_answer";
    committed_at_unix_ms?: number | null;
  }> | null;
  Tool: Readonly<{
    StepID?: string | null;
    ToolCallID: string | null;
    ToolName: string | null;
    Text: string;
    IsError: boolean;
    ResultSummary?: string | null;
    CondensedText?: string | null;
    Presentation?: ChatToolPresentation | null;
    QuestionAnswer?: Readonly<{ SelectedOptionNumber?: number | null; Freeform?: string | null }> | null;
    WebSearch?: ChatWebSearchDetail | null;
  }> | null;
  ReasoningTrace: Readonly<{
    StepID: string;
    CompactText: string;
    Text: string;
    duration_ms?: number | null;
    ProvisionalIdentity?: ChatReasoningIdentity | null;
  }> | null;
  Notice: ChatNotice | null;
  ReviewerFeedback: Readonly<{
    ID: string;
    StepID: string;
    Suggestions: readonly string[];
    SuggestionCount: number;
  }> | null;
  ReviewerError: Readonly<{ ID: string; StepID: string; Detail: string }> | null;
}>;
type AssistantFacts = Readonly<{ StepID: string; StreamID: string; Phase: "commentary" | "final_answer" }>;
export interface ChatTranscriptPayloadByKind {
  hydration: Readonly<{
    SessionIdentity: ChatTranscriptPayloadByKind["session_identity"];
    SessionStatus: ChatTranscriptPayloadByKind["session_status"];
    RuntimeReadModelUpdate: ChatTranscriptPayloadByKind["runtime_read_model_update"];
    TailSegment: Readonly<{
      OlderCursor: number | null;
      HasMoreAbove: boolean;
      Entries: readonly ChatCommittedRow[];
    }>;
    ActiveAssistant: (AssistantFacts & Readonly<{ Text: string }>) | null;
    ActiveThinkingStatus: ChatTranscriptPayloadByKind["thinking_status_update"] | null;
    ActiveReasoningTraces: readonly ChatTranscriptPayloadByKind["reasoning_trace_update"][];
    ActiveStep: ChatTranscriptPayloadByKind["step_state"] | null;
    ActiveCompaction: ChatTranscriptPayloadByKind["compaction_status"] | null;
    InFlightTools: readonly ChatTranscriptPayloadByKind["tool_start"][];
    PendingPrompts: readonly ChatTranscriptPayloadByKind["prompt"][];
    BackgroundActivities: readonly ChatTranscriptPayloadByKind["background_activity"][];
    ContextUsage: ChatTranscriptPayloadByKind["context_usage"] | null;
    GoalStatus: ChatGoalFacts | null;
  }>;
  committed_row: ChatCommittedRow;
  assistant_delta: AssistantFacts & Readonly<{ Delta: string }>;
  assistant_stream_abort: Readonly<{
    StepID: string;
    StreamID: string;
    Reason: "interrupted" | "failed" | "superseded";
    Diagnostic?: ChatDiagnostic | null;
  }>;
  thinking_status_update: Readonly<{ StepID: string; Text: string }>;
  reasoning_trace_update: Readonly<{
    StepID: string;
    Identity: ChatReasoningIdentity;
    CompactText: string;
    Text: string;
  }>;
  reasoning_trace_reset: Readonly<{ StepID: string }>;
  tool_start: Readonly<{
    StepID: string;
    ToolCallID: string;
    ToolName: string;
    Presentation?: ChatToolPresentation | null;
  }>;
  tool_abort: Readonly<{
    StepID: string;
    ToolCallID: string;
    Reason: "canceled" | "failed";
    Diagnostic?: ChatDiagnostic | null;
  }>;
  user_message_flushed: Readonly<{ StepID?: string | null }>;
  queued_message_state: Readonly<{
    QueueItemID: string;
    Status: "accepted" | "submitted" | "failed" | "discarded";
    FailureReason?: "closing" | "terminal_workflow_completion" | "runtime_unavailable" | null;
    Text?: string | null;
  }>;
  pending_work_changed: Readonly<Record<never, never>>;
  pending_work_restored: Readonly<{
    Restoration: Readonly<{ ItemID: string; Kind: string; CanonicalInput: string }>;
  }>;
  session_setting_feedback: Readonly<{
    Kind: "session_name" | "thinking" | "fast_mode" | "supervisor" | "questions" | "auto_compaction";
    Changed: boolean;
    SessionName?: string | null;
    Thinking?: string | null;
    FastMode?: boolean | null;
    Supervisor?: string | null;
    Questions?: boolean | null;
    AutoCompaction?: boolean | null;
  }>;
  human_input_interrupted: Readonly<{ Items: readonly Readonly<{ QueueItemID: string; Text: string }>[] }>;
  step_state: Readonly<{
    RunID: string;
    StepID: string;
    Lifecycle: "started" | "finished";
    ActiveKind: ChatActiveKind;
    Status: "running" | "completed" | "interrupted" | "failed";
  }>;
  runtime_read_model_update: Readonly<{
    Version: Readonly<{ Epoch: string; Generation: number; Sequence: number }>;
    Activity: ChatActivityFacts;
  }>;
  session_status: Readonly<{
    ReviewerFrequency: string;
    ReviewerEnabled: boolean;
    AutoCompactionEnabled: boolean;
    QuestionsEnabled: boolean;
    FastModeAvailable: boolean;
    FastModeEnabled: boolean;
    ThinkingLevel: string;
    CompactionMode: string;
    CompactionCount: number;
    PreviousSessionID?: string | null;
    ParentAgentSessionID?: string | null;
    NavigationTargetSessionID?: string | null;
    Workflow: Readonly<{ TaskID: string; WorkflowID: string }> | null;
  }>;
  session_identity: Readonly<{
    SessionID: string;
    SessionName: string | null;
    ConversationFreshness: 0 | 1;
    ExecutionTarget: ChatExecutionFacts | null;
  }>;
  compaction_status: Readonly<{
    StepID: string;
    RequestID?: string | null;
    State: "started" | "completed" | "failed";
    Mode: "auto" | "handoff" | "manual" | "workflow_post_completion";
    Count: number;
    Diagnostic?: ChatDiagnostic | null;
  }>;
  context_usage: Readonly<{ UsedTokens: number; WindowTokens: number; CacheHitPercent?: number | null }>;
  goal_status: ChatGoalFacts;
  background_activity: Readonly<{
    ActivityID: string;
    ProcessID: string;
    OwnerRunID: string;
    OwnerStepID: string;
    Lifecycle: "backgrounded" | "completed" | "killed";
    Command: string;
    Workdir: string;
    LogPath?: string | null;
    Preview?: string | null;
    ExitCode?: number | null;
    UserRequestedKill: boolean;
    NoticeSuppressed: boolean;
    Diagnostic?: ChatDiagnostic | null;
  }>;
  prompt: Readonly<{
    Kind: "question" | "approval";
    State: "pending" | "resolved";
    ToolCallID: string;
    SessionID: string;
    StepID: string;
    Question: string;
    CreatedAt: string;
    Suggestions: readonly string[];
    RecommendedOptionIndex?: number | null;
    ApprovalOptions: readonly Readonly<{
      Decision: "allow_once" | "allow_session" | "deny";
      Label: string;
    }>[];
    AccessTargets: readonly Readonly<{ RequestedPath: string; ResolvedPath: string }>[];
  }>;
  worktree_transition_outcome: Readonly<{
    OperationID: string;
    Transition: "enter" | "leave" | "delete";
    State: "completed" | "failed";
    Failure?: ChatDiagnostic | null;
    SelectorError?: Readonly<{
      kind: 1 | 2 | 3;
      input: string;
      candidates?: readonly Readonly<{
        variant: 1 | 2 | 3;
        selector: string;
        branch_name?: string;
        display_name?: string;
        fallback_identity: string;
      }>[];
    }> | null;
    DeletePrecondition?: Readonly<{
      kind: "dirty" | "unknown";
      dirty_file_count?: number;
      unknown_cause?: string;
    }> | null;
  }>;
  operational_diagnostic: Readonly<{
    Code:
      | "sleep_guard_failed"
      | "prompt_history_persist_failed"
      | "context_facts_persist_failed"
      | "in_flight_clear_failed"
      | "provider_turn_state_invalid";
    StepID?: string | null;
    Detail: string;
  }>;
  live_run_finished: Readonly<{
    Status: "completed" | "interrupted" | "failed";
    ResultKind: "assistant_final_answer" | "no_final_answer";
    NoFinalReason: string;
    WorkPerformed: boolean;
    FinalAnswer?: string | null;
    Failure?: string | null;
    StartedAt: string;
    FinishedAt: string;
  }>;
}
export type ChatTranscriptKind = keyof ChatTranscriptPayloadByKind;
export type ChatTranscriptPayload = ChatTranscriptPayloadByKind[ChatTranscriptKind];
export type ChatTranscriptMessageByKind = {
  [Kind in ChatTranscriptKind]: Readonly<{
    sequence: number;
    kind: Kind;
    payload: ChatTranscriptPayloadByKind[Kind];
  }>;
}[ChatTranscriptKind];
export type ChatTranscriptMessage = ChatTranscriptMessageByKind;
