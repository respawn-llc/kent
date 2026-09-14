import type { PendingPrompt } from "@/api";
import { target } from "@/test-support/chat-runtime";
import { ConnectionStore } from "@/api/composition";
import { create } from "@app/server-api-contract";
import {
  AnswerService,
  QuestionService,
  ApprovalService,
} from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import { ApiClient } from "@/api/composition";
import { FakeRpcTransport } from "@/test-support/api";

export function failedBatchWithFreeform(freeform: PendingPrompt) {
  return new ApiClient(
    new FakeRpcTransport([
      { descriptor: AnswerService.method.answerBatch, error: new Error("Sending failed") },
      {
        descriptor: QuestionService.method.listPending,
        result: create(QuestionService.method.listPending.output, {
          outcome: {
            case: "success",
            value: {
              questions: [
                {
                  sessionId: freeform.sessionID,
                  stepId: freeform.stepID,
                  toolCallId: freeform.toolCallID,
                  question: "Explain?",
                  createdAt: { seconds: 1789171200n, nanos: 0 },
                },
              ],
            },
          },
        }),
      },
      {
        descriptor: ApprovalService.method.listPending,
        result: create(ApprovalService.method.listPending.output, {
          outcome: { case: "success", value: { approvals: [] } },
        }),
      },
    ]),
  ).chat;
}

export function connectedConnection(): ConnectionStore {
  const connection = new ConnectionStore();
  connection.set("connected");
  return connection;
}

export function question(
  toolCallID = "question-1",
  overrides: Partial<Extract<PendingPrompt, { kind: "ordinary" }>> = {},
): Extract<PendingPrompt, { kind: "ordinary" }> {
  return {
    kind: "ordinary",
    toolCallID,
    sessionID: target.sessionID,
    stepID: "323e4567-e89b-42d3-a456-426614174000",
    question: "Choose?",
    suggestions: ["one", "two"],
    recommendedOptionIndex: null,
    createdAt: "2026-09-12T00:00:00.000Z",
    ...overrides,
  };
}

export function approval(toolCallID = "approval-1"): Extract<PendingPrompt, { kind: "approval" }> {
  return {
    kind: "approval",
    toolCallID,
    sessionID: target.sessionID,
    stepID: "323e4567-e89b-42d3-a456-426614174000",
    question: "Allow?",
    approvalDecisions: ["allow_once", "allow_session", "deny"],
    accessTargets: [],
    createdAt: "2026-09-12T00:00:01.000Z",
  };
}
