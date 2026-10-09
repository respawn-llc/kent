import {
  ApprovalDecision,
  type Approval,
  type Question,
} from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import { enumValue, required } from "./chatWire";
import { timestampMillis } from "./clientTime";
import type { PendingAsk } from "./models";
import type { PendingPrompt } from "./promptModels";

export function orderPendingPrompts(prompts: readonly PendingPrompt[]): readonly PendingPrompt[] {
  return [...prompts].sort((left, right) => {
    if (left.stepID !== right.stepID) return left.stepID.localeCompare(right.stepID);
    const leftOrdinal = left.batch?.toolCallIDs.indexOf(left.toolCallID);
    const rightOrdinal = right.batch?.toolCallIDs.indexOf(right.toolCallID);
    const leftPrepared = leftOrdinal !== undefined && leftOrdinal >= 0;
    const rightPrepared = rightOrdinal !== undefined && rightOrdinal >= 0;
    if (leftPrepared !== rightPrepared) return leftPrepared ? -1 : 1;
    if (leftPrepared && rightPrepared) return leftOrdinal - rightOrdinal;
    return Date.parse(left.createdAt) - Date.parse(right.createdAt);
  });
}

export function pendingQuestion(question: Question): PendingAsk {
  return {
    toolCallID: question.toolCallId,
    sessionID: question.sessionId,
    stepID: question.stepId,
    question: question.question,
    suggestions: question.suggestions,
    recommendedOptionIndex: question.recommendedOptionIndex ?? null,
    createdAt: new Date(timestampMillis(required(question.createdAt))).toISOString(),
    ...(question.batch === undefined
      ? {}
      : {
          batch: {
            toolCallIDs: question.batch.toolCallIds,
            unmaterializedCount: question.batch.unmaterializedCount,
          },
        }),
  };
}

export function questionPrompt(question: Question): PendingPrompt {
  return { ...pendingQuestion(question), kind: "ordinary" };
}

export function approvalPrompt(approval: Approval): PendingPrompt {
  return {
    kind: "approval",
    toolCallID: approval.toolCallId,
    sessionID: approval.sessionId,
    stepID: approval.stepId,
    question: approval.question ?? null,
    createdAt: new Date(timestampMillis(required(approval.createdAt))).toISOString(),
    ...(approval.batch === undefined
      ? {}
      : {
          batch: {
            toolCallIDs: approval.batch.toolCallIds,
            unmaterializedCount: approval.batch.unmaterializedCount,
          },
        }),
    approvalDecisions: approval.options.map((option) => approvalDecision(option.decision)),
    accessTargets: approval.accessTargets.map((target) => ({
      requestedPath: target.requestedPath,
      resolvedPath: target.resolvedPath,
    })),
  };
}

export function approvalDecision(value: ApprovalDecision) {
  return enumValue(value, {
    [ApprovalDecision.ALLOW_ONCE]: "allow_once",
    [ApprovalDecision.ALLOW_SESSION]: "allow_session",
    [ApprovalDecision.DENY]: "deny",
  });
}
