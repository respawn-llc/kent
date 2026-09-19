import * as T from "@app/server-api-contract/gen/kent/api/transcript/transcript_pb";
import type {
  ChatCommittedRow,
  ChatDiagnostic,
  ChatNotice,
  ChatReasoningIdentity,
  ChatWebSearchDetail,
} from "./chatTranscriptTypes";
import { enumValue, required, safeNumber } from "./chatWire";
import { timestampMillis } from "./clientTime";
import { toolPresentation } from "./chatToolPresentation";
import { ContractError } from "./errors";

export function diagnostic(value: T.Diagnostic | undefined): ChatDiagnostic | null {
  return value === undefined ? null : { Code: value.code, Detail: value.detail };
}

export function assistantPhase(value: T.AssistantPhase): "commentary" | "final_answer" {
  return enumValue(value, {
    [T.AssistantPhase.COMMENTARY]: "commentary",
    [T.AssistantPhase.FINAL]: "final_answer",
  });
}

export function reasoningIdentity(value: T.ReasoningTraceIdentity): ChatReasoningIdentity {
  switch (value.identity.case) {
    case "provider":
      return {
        Provider: {
          ItemID: value.identity.value.itemId,
          SummaryIndex: safeNumber(required(value.identity.value.summaryIndex)),
        },
        Kent: null,
      };
    case "kentTraceId":
      return { Provider: null, Kent: value.identity.value };
    case undefined:
      throw new ContractError("Reasoning trace identity is missing.");
  }
}

function visibility(value: T.EntryVisibility): ChatCommittedRow["Visibility"] {
  return enumValue(value, {
    [T.EntryVisibility.ONGOING]: "ongoing",
    [T.EntryVisibility.ONGOING_COLLAPSED]: "ongoing_collapsed",
    [T.EntryVisibility.DETAIL]: "detail",
    [T.EntryVisibility.HIDDEN]: "hidden",
  });
}

function messageType(value: T.NoticeMessageType): string {
  return enumValue(value, {
    [T.NoticeMessageType.AGENTS_MD]: "agents.md",
    [T.NoticeMessageType.SKILLS]: "skills",
    [T.NoticeMessageType.SUBAGENTS]: "subagents",
    [T.NoticeMessageType.ENVIRONMENT]: "environment",
    [T.NoticeMessageType.COMPACTION_SUMMARY]: "compaction_summary",
    [T.NoticeMessageType.INTERRUPTION]: "interruption",
    [T.NoticeMessageType.ERROR_FEEDBACK]: "error_feedback",
    [T.NoticeMessageType.COMPACTION_SOON_REMINDER]: "compaction_soon_reminder",
    [T.NoticeMessageType.HANDOFF_FUTURE_MESSAGE]: "handoff_future_message",
    [T.NoticeMessageType.REVIEWER_FEEDBACK]: "reviewer_feedback",
    [T.NoticeMessageType.BACKGROUND_NOTICE]: "background_notice",
    [T.NoticeMessageType.CUSTOM_TOOL_CALL_OUTPUT]: "custom_tool_call_output",
    [T.NoticeMessageType.COMPACTION_PRESERVED_USER_MESSAGE]: "manual_compaction_carryover",
    [T.NoticeMessageType.HEADLESS_MODE]: "headless_mode",
    [T.NoticeMessageType.HEADLESS_MODE_EXIT]: "headless_mode_exit",
    [T.NoticeMessageType.WORKFLOW_MODE]: "workflow_mode",
    [T.NoticeMessageType.WORKTREE_MODE]: "worktree_mode",
    [T.NoticeMessageType.WORKTREE_MODE_EXIT]: "worktree_mode_exit",
    [T.NoticeMessageType.GOAL]: "goal",
    [T.NoticeMessageType.ACTIVE_GOAL_CONTINUATION]: "active_goal_continuation",
    [T.NoticeMessageType.AGENT_STEER]: "agent_steer",
    [T.NoticeMessageType.WORKFLOW_MODE_EXIT]: "workflow_mode_exit",
    [T.NoticeMessageType.SESSION_REBIND]: "session_rebind",
    [T.NoticeMessageType.USER_SHELL_COMMAND]: "user_shell_command",
  });
}

function notice(value: T.NoticeRow): ChatNotice {
  return {
    StepID: value.stepId ?? null,
    Reason: enumValue(value.reason, {
      [T.NoticeReason.CACHE_WARNING]: "cache_warning",
      [T.NoticeReason.COMPACTION]: "compaction",
      [T.NoticeReason.LEGACY_UNTYPED_NOTICE]: "legacy_untyped_notice",
      [T.NoticeReason.RUNTIME_DIAGNOSTIC]: "runtime_diagnostic",
      [T.NoticeReason.TOOL_OUTPUT_REPAIR]: "tool_output_repair",
      [T.NoticeReason.PROVIDER_MODEL_MISMATCH]: "provider_model_mismatch",
      [T.NoticeReason.THINKING_UPDATE]: "thinking_update",
    }),
    Severity: enumValue(value.severity, {
      [T.NoticeSeverity.INFO]: "info",
      [T.NoticeSeverity.WARNING]: "warning",
      [T.NoticeSeverity.ERROR]: "error",
    }),
    MessageType: value.messageType === undefined ? null : messageType(value.messageType),
    LegacyText: value.legacyText ?? null,
    ThinkingEffort: value.thinkingEffort ?? null,
    NoticeID: value.noticeId ?? null,
    SourcePath: value.sourcePath ?? null,
    ...noticeContext(value),
    ...noticeDiagnostics(value),
    CondensedText: value.condensedText ?? null,
    CompactLabel: value.compactLabel ?? null,
  };
}

function noticeContext(value: T.NoticeRow): Pick<ChatNotice, "Worktree" | "CacheWarning" | "Compaction"> {
  return {
    Worktree:
      value.worktree === undefined
        ? null
        : {
            Branch: value.worktree.branch ?? null,
            WorktreePath: value.worktree.worktreePath,
            WorkspaceRoot: value.worktree.workspaceRoot,
            EffectiveCwd: value.worktree.effectiveCwd,
          },
    CacheWarning:
      value.cacheWarning === undefined
        ? null
        : {
            Scope: value.cacheWarning.scope,
            Reason: value.cacheWarning.reason,
            LostInputTokens: value.cacheWarning.lostInputTokens ?? null,
            Visibility: visibility(value.cacheWarning.visibility),
          },
    Compaction:
      value.compaction === undefined
        ? null
        : { Count: value.compaction.count ?? null, Detail: value.compaction.detail ?? null },
  };
}

function noticeDiagnostics(
  value: T.NoticeRow,
): Pick<ChatNotice, "ToolOutputRepair" | "ProviderModelMismatch" | "Diagnostic" | "Background"> {
  const repairKind = value.toolOutputRepair?.kind;
  if (repairKind !== undefined && repairKind !== "fresh_resource" && repairKind !== "live_provider_rejection")
    throw new ContractError("Tool output repair kind is invalid.");
  return {
    ToolOutputRepair:
      repairKind === undefined ? null : { kind: repairKind, count: required(value.toolOutputRepair).count },
    ProviderModelMismatch:
      value.providerModelMismatch === undefined
        ? null
        : {
            requested_model: value.providerModelMismatch.requestedModel,
            served_model: value.providerModelMismatch.servedModel,
          },
    Diagnostic: diagnostic(value.diagnostic),
    Background:
      value.background === undefined
        ? null
        : {
            ActivityID: value.background.activityId,
            ProcessID: value.background.processId,
            ExitCode: value.background.exitCode ?? null,
          },
  };
}

export function committedRow(value: T.CommittedRow): ChatCommittedRow {
  const locator = required(value.locator);
  const base = {
    Visibility: visibility(value.visibility),
    Integrity: enumValue(value.integrity, {
      [T.RowIntegrity.VALID]: 0,
      [T.RowIntegrity.RECOVERABLE_MALFORMED]: 1,
      [T.RowIntegrity.UNRECOVERABLE_MALFORMED]: 2,
    }),
    Locator: { event_sequence: safeNumber(locator.eventSequence), row_ordinal: locator.rowOrdinal },
    User: null,
    Assistant: null,
    Tool: null,
    ReasoningTrace: null,
    Notice: null,
    ReviewerFeedback: null,
    ReviewerError: null,
  };
  const row = value.row;
  switch (row.case) {
    case "user":
      return {
        ...base,
        Kind: "user",
        User: userRow(row.value),
      };
    case "assistant":
      return {
        ...base,
        Kind: "assistant",
        Assistant: assistantRow(row.value),
      };
    case "tool":
      return {
        ...base,
        Kind: "tool",
        Tool: toolRow(row.value),
      };
    case "reasoningTrace":
      return {
        ...base,
        Kind: "reasoning_trace",
        ReasoningTrace: reasoningRow(row.value),
      };
    case "notice":
      return { ...base, Kind: "notice", Notice: notice(row.value) };
    case "reviewerFeedback":
      return {
        ...base,
        Kind: "reviewer_feedback",
        ReviewerFeedback: {
          ID: row.value.id,
          StepID: row.value.stepId,
          Suggestions: [...row.value.suggestions],
          SuggestionCount: row.value.suggestionCount,
        },
      };
    case "reviewerError":
      return {
        ...base,
        Kind: "reviewer_error",
        ReviewerError: {
          ID: row.value.id,
          StepID: row.value.stepId,
          Detail: row.value.detail,
        },
      };
    case undefined:
      throw new ContractError("Committed transcript row is missing.");
  }
}

function userRow(value: T.UserRow): NonNullable<ChatCommittedRow["User"]> {
  return {
    StepID: value.stepId ?? null,
    Text: value.text,
    CondensedText: value.condensedText ?? null,
    RollbackTargetID: value.rollbackTargetId ?? null,
    committed_at_unix_ms: value.committedAt === undefined ? null : timestampMillis(value.committedAt),
  };
}

function assistantRow(value: T.AssistantRow): NonNullable<ChatCommittedRow["Assistant"]> {
  return {
    StepID: value.stepId,
    StreamID: value.streamId ?? null,
    Text: value.text,
    CondensedText: value.condensedText ?? null,
    Phase: assistantPhase(value.phase),
    committed_at_unix_ms: value.committedAt === undefined ? null : timestampMillis(value.committedAt),
  };
}

function toolRow(value: T.ToolRow): NonNullable<ChatCommittedRow["Tool"]> {
  return {
    StepID: value.stepId ?? null,
    ToolCallID: value.toolCallId ?? null,
    ToolName: value.toolName ?? null,
    Text: value.text,
    IsError: value.isError,
    ResultSummary: value.resultSummary ?? null,
    CondensedText: value.condensedText ?? null,
    Presentation: toolPresentation(value.presentation, value.toolName),
    WebSearch: value.webSearch === undefined ? null : webSearchDetail(value.webSearch),
    QuestionAnswer:
      value.questionAnswer === undefined
        ? null
        : {
            SelectedOptionNumber: value.questionAnswer.selectedOptionNumber ?? null,
            Freeform: value.questionAnswer.freeform ?? null,
          },
  };
}

function webSearchDetail(value: T.WebSearchDetail): ChatWebSearchDetail {
  return {
    action: webSearchAction(value),
    results: value.results.map((result) => ({
      kind: enumValue(result.kind, {
        [T.WebSearchResultKind.LINK]: "link",
        [T.WebSearchResultKind.IMAGE]: "image",
      }),
      title: result.title ?? null,
      destination: result.destination ?? null,
    })),
    sources: value.sources,
  };
}

function webSearchAction(value: T.WebSearchDetail): ChatWebSearchDetail["action"] {
  switch (value.action.case) {
    case "search":
      return { kind: "search", queries: value.action.value.queries };
    case "openPage":
      return { kind: "open-page", url: value.action.value.url ?? null };
    case "findInPage":
      return {
        kind: "find-in-page",
        url: value.action.value.url ?? null,
        pattern: value.action.value.pattern ?? null,
      };
    case undefined:
      throw new ContractError("Web Search action is missing.");
  }
}

function reasoningRow(value: T.ReasoningTraceRow): NonNullable<ChatCommittedRow["ReasoningTrace"]> {
  return {
    StepID: value.stepId,
    CompactText: value.compactText,
    Text: value.text,
    duration_ms: value.duration === undefined ? null : timestampMillis(value.duration),
    ProvisionalIdentity:
      value.provisionalIdentity === undefined ? null : reasoningIdentity(value.provisionalIdentity),
  };
}
