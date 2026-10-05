import { create } from "@app/server-api-contract";
import {
  AnswerService,
  AnswerBatchEntrySchema,
  AnswerBatchOutcome,
  ApprovalDecision,
} from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";

import type {
  PromptAnswerBatchEntryInput,
  PromptAnswerBatchInput,
  PromptAnswerBatchResponse,
} from "./clientInputs";
import { enumValue } from "./chatWire";
import { requireUnarySuccess } from "./protobufRpc";
import type { DescriptorRpcTransport } from "./transport";

export async function answerPromptBatch(
  transport: DescriptorRpcTransport,
  input: PromptAnswerBatchInput,
): Promise<PromptAnswerBatchResponse> {
  const method = AnswerService.method.answerBatch;
  const request = create(method.input, {
    sessionId: input.sessionID,
    stepId: input.stepID,
    entries: input.entries.map(encodeEntry),
  });
  const success = requireUnarySuccess(
    method,
    await transport.callDescriptorAttachedSession({ sessionID: input.sessionID }, method, request),
  );
  const response: PromptAnswerBatchResponse = {
    results: success.results.map((result) => ({
      toolCallID: result.toolCallId,
      outcome: enumValue(result.outcome, {
        [AnswerBatchOutcome.RESOLVED]: "resolved",
        [AnswerBatchOutcome.SKIPPED]: "skipped",
      }),
    })),
  };
  if (response.results.length !== input.entries.length) {
    throw new Error("prompt answer batch result count does not match request");
  }
  const requested = new Set(input.entries.map((entry) => entry.toolCallID));
  for (const result of response.results) {
    if (!requested.delete(result.toolCallID)) {
      throw new Error("prompt answer batch result identity is foreign or duplicated");
    }
  }
  return response;
}

function encodeEntry(entry: PromptAnswerBatchEntryInput) {
  switch (entry.kind) {
    case "question":
      return create(AnswerBatchEntrySchema, {
        toolCallId: entry.toolCallID,
        answer: {
          case: "questionAnswer",
          value: {
            selectedOptionNumber: entry.selectedOptionNumber ?? undefined,
            freeform: entry.freeform ?? undefined,
          },
        },
      });
    case "approval":
      return create(AnswerBatchEntrySchema, {
        toolCallId: entry.toolCallID,
        answer: {
          case: "approvalAnswer",
          value: {
            decision: {
              allow_once: ApprovalDecision.ALLOW_ONCE,
              allow_session: ApprovalDecision.ALLOW_SESSION,
              deny: ApprovalDecision.DENY,
            }[entry.decision],
            commentary: entry.commentary ?? undefined,
          },
        },
      });
    case "declined":
      return create(AnswerBatchEntrySchema, {
        toolCallId: entry.toolCallID,
        answer: { case: "declined", value: {} },
      });
  }
}
