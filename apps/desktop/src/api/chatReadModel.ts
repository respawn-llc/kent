import * as R from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import { ProjectAvailability } from "@app/server-api-contract/gen/kent/api/project/project_pb";
import type { SessionExecutionTarget } from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";
import type {
  ChatActivityFacts,
  ChatActiveKind,
  ChatExecutionFacts,
  ChatTranscriptPayloadByKind,
} from "./chatTranscriptTypes";
import type { ChatMainViewRead, ChatRuntimeStatus } from "./chatTypes";
import { chatExecutionTarget, chatRuntimeActivity } from "./chatProjection";
import { goalFactFromWire } from "./chatGoal";
import { ContractError } from "./errors";
import { enumValue, required, safeNumber } from "./chatWire";

export function conversationFreshness(value: R.ConversationFreshness): 0 | 1 {
  return enumValue(value, { [R.ConversationFreshness.FRESH]: 0, [R.ConversationFreshness.ESTABLISHED]: 1 });
}

export function activeKind(value: R.ActivityActiveKind): ChatActiveKind {
  return enumValue(value, {
    [R.ActivityActiveKind.RUNTIME_ACTIVITY_ACTIVE_KIND_USER_TURN]: "user_turn",
    [R.ActivityActiveKind.RUNTIME_ACTIVITY_ACTIVE_KIND_WORKFLOW_TURN]: "workflow_turn",
    [R.ActivityActiveKind.RUNTIME_ACTIVITY_ACTIVE_KIND_GOAL_LOOP]: "goal_loop",
    [R.ActivityActiveKind.RUNTIME_ACTIVITY_ACTIVE_KIND_COMPACTION]: "compaction",
    [R.ActivityActiveKind.RUNTIME_ACTIVITY_ACTIVE_KIND_PRE_SUBMIT_COMPACTION]: "pre_submit_compaction",
    [R.ActivityActiveKind.RUNTIME_ACTIVITY_ACTIVE_KIND_USER_SHELL]: "user_shell",
    [R.ActivityActiveKind.RUNTIME_ACTIVITY_ACTIVE_KIND_BACKGROUND]: "background",
    [R.ActivityActiveKind.RUNTIME_ACTIVITY_ACTIVE_KIND_RUNTIME_MAINTENANCE]: "runtime_maintenance",
  });
}

function availability(value: ProjectAvailability): ChatExecutionFacts["WorkspaceAvailability"] {
  return enumValue(value, {
    [ProjectAvailability.AVAILABLE]: "available",
    [ProjectAvailability.MISSING]: "missing",
    [ProjectAvailability.INACCESSIBLE]: "inaccessible",
    [ProjectAvailability.UNLINKED]: "unlinked",
  });
}

export function executionFacts(value: SessionExecutionTarget): ChatExecutionFacts {
  return {
    WorkspaceID: value.workspaceId ?? null,
    WorkspaceName: value.workspaceName,
    WorkspaceRoot: value.workspaceRoot,
    WorkspaceAvailability: availability(value.workspaceAvailability),
    Worktree:
      value.worktree === undefined
        ? null
        : {
            ID: value.worktree.id,
            Name: value.worktree.name,
            Root: value.worktree.root,
            Availability: availability(value.worktree.availability),
          },
    CwdRelpath: value.cwdRelpath,
    EffectiveWorkdir: value.effectiveWorkdir,
  };
}

export function activityFacts(value: R.Activity): ChatActivityFacts {
  return {
    State: enumValue(value.state, {
      [R.ActivityState.RUNTIME_ACTIVITY_UNAVAILABLE]: "unavailable",
      [R.ActivityState.RUNTIME_ACTIVITY_REGISTERED_IDLE]: "registered_idle",
      [R.ActivityState.RUNTIME_ACTIVITY_STARTING]: "starting",
      [R.ActivityState.RUNTIME_ACTIVITY_RUNNING]: "running",
      [R.ActivityState.RUNTIME_ACTIVITY_AWAITING_PROMPT]: "awaiting_prompt",
      [R.ActivityState.RUNTIME_ACTIVITY_DRAINING]: "draining",
      [R.ActivityState.RUNTIME_ACTIVITY_CLOSING]: "closing",
    }),
    ActiveStep:
      value.activeStep === undefined
        ? null
        : {
            RunID: value.activeStep.runId,
            StepID: value.activeStep.stepId,
            ActiveKind: activeKind(value.activeStep.activeKind),
          },
    Reviewer: enumValue(value.reviewer, {
      [R.ReviewerActivity.INACTIVE]: "inactive",
      [R.ReviewerActivity.INVOKING]: "invoking",
      [R.ReviewerActivity.ADDRESSING_FEEDBACK]: "addressing_feedback",
    }),
    QueueAccepting: value.queueAccepting,
    DiagnosticRecovery: value.diagnosticRecovery,
  };
}

export function readModelUpdate(
  value: R.ReadModelUpdate,
): ChatTranscriptPayloadByKind["runtime_read_model_update"] {
  const version = required(value.version);
  return {
    Version: {
      Epoch: version.epoch,
      Generation: safeNumber(version.generation),
      Sequence: safeNumber(version.sequence),
    },
    Activity: activityFacts(required(value.activity)),
  };
}

export function contextUsage(value: R.ContextUsage): ChatTranscriptPayloadByKind["context_usage"] {
  return {
    UsedTokens: value.usedTokens,
    WindowTokens: value.windowTokens,
    CacheHitPercent: value.cacheHitPercent ?? null,
  };
}

function runtimeStatus(value: R.Status): ChatRuntimeStatus {
  const usage = required(value.contextUsage);
  return {
    reviewerFrequency: value.reviewerFrequency,
    reviewerEnabled: value.reviewerEnabled,
    autoCompactionEnabled: value.autoCompactionEnabled,
    questionsEnabled: value.questionsEnabled,
    fastModeAvailable: value.fastModeAvailable,
    fastModeEnabled: value.fastModeEnabled,
    conversationFreshness: conversationFreshness(value.conversationFreshness),
    previousSessionID: value.previousSessionId ?? null,
    parentAgentSessionID: value.parentAgentSessionId ?? null,
    navigationTargetSessionID: value.navigationTargetSessionId ?? null,
    lastCommittedAssistantFinalAnswer: value.lastCommittedAssistantFinalAnswer ?? null,
    thinkingLevel: value.thinkingLevel,
    compactionMode: value.compactionMode,
    contextUsage: {
      usedTokens: usage.usedTokens,
      windowTokens: usage.windowTokens,
      cacheHitPercent: usage.cacheHitPercent ?? 0,
      hasCacheHitPercentage: usage.cacheHitPercent !== undefined,
    },
    compactionCount: value.compactionCount,
    workflowSession:
      value.workflowSession === undefined
        ? null
        : {
            taskID: value.workflowSession.taskId,
            workflowID: value.workflowSession.workflowId,
          },
  };
}

export function mainView(value: R.MainView, sessionID: string): ChatMainViewRead {
  const session = required(value.session);
  if (session.sessionId !== sessionID)
    throw new ContractError("Session Main View does not match the requested Session.");
  const version = required(value.version);
  const status = required(value.status);
  return {
    mainView: {
      version: {
        epoch: version.epoch,
        generation: safeNumber(version.generation),
        sequence: safeNumber(version.sequence),
      },
      status: runtimeStatus(status),
      sessionID: session.sessionId,
      sessionName: session.sessionName ?? null,
      executionTarget: chatExecutionTarget(executionFacts(required(session.executionTarget))),
      activity: chatRuntimeActivity(activityFacts(required(value.activity))),
    },
    goal: goalFactFromWire(status.goal),
  };
}
