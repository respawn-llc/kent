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
  return [...prompts].sort((left, right) => Date.parse(left.createdAt) - Date.parse(right.createdAt));
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
    approvalDecisions: approval.options.map((option) =>
      enumValue(option.decision, {
        [ApprovalDecision.ALLOW_ONCE]: "allow_once",
        [ApprovalDecision.ALLOW_SESSION]: "allow_session",
        [ApprovalDecision.DENY]: "deny",
      }),
    ),
    accessTargets: approval.accessTargets.map((target) => ({
      requestedPath: target.requestedPath,
      resolvedPath: target.resolvedPath,
    })),
  };
}
