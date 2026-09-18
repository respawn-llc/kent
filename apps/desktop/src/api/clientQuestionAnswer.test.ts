import { unexpectedProjectOverflow } from "@/test-support/api";
import { create, operationName } from "@app/server-api-contract";
import {
  AnswerService,
  AnswerBatchOutcome,
  ApprovalDecision,
} from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import { ApiClient } from "./client";
import { FakeRpcTransport } from "@/test-support/api";
const sessionID = "11111111-1111-4111-8111-111111111111";
const stepID = "22222222-2222-4222-8222-222222222222";
const batchRequest = {
  sessionID,
  stepID,
  entries: [
    { kind: "question" as const, toolCallID: "q", selectedOptionNumber: 2, freeform: "because" },
    { kind: "approval" as const, toolCallID: "a", decision: "allow_once" as const, commentary: null },
    { kind: "declined" as const, toolCallID: "d" },
  ],
} as const;
const resolvedQuestion = { toolCallId: "q", outcome: AnswerBatchOutcome.RESOLVED } as const;
const skippedApproval = { toolCallId: "a", outcome: AnswerBatchOutcome.SKIPPED };
const resolvedDeclined = { toolCallId: "d", outcome: AnswerBatchOutcome.RESOLVED };
const results = [resolvedQuestion, skippedApproval, resolvedDeclined];
describe("ApiClient prompt answer batches", () => {
  it("encodes Tool Call keyed entries and parses Tool Call keyed outcomes", async () => {
    const transport = new FakeRpcTransport([
      {
        descriptor: AnswerService.method.answerBatch,
        result: create(AnswerService.method.answerBatch.output, {
          outcome: { case: "success", value: { results } },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    const response = await client.answerPromptBatch(batchRequest);
    expect(transport.calls).toEqual([]);
    expect(transport.attachedSessionCalls).toEqual([
      {
        sessionID,
        method: operationName(AnswerService.method.answerBatch),
      },
    ]);
    expect(transport.descriptorCalls[0]?.request).toMatchObject({
      sessionId: sessionID,
      stepId: stepID,
      entries: [
        {
          toolCallId: "q",
          answer: { case: "questionAnswer", value: { selectedOptionNumber: 2, freeform: "because" } },
        },
        {
          toolCallId: "a",
          answer: { case: "approvalAnswer", value: { decision: ApprovalDecision.ALLOW_ONCE } },
        },
        { toolCallId: "d", answer: { case: "declined", value: {} } },
      ],
    });
    expect(response.results).toEqual([
      { toolCallID: "q", outcome: "resolved" },
      { toolCallID: "a", outcome: "skipped" },
      { toolCallID: "d", outcome: "resolved" },
    ]);
  });
  it.each([
    ["missing", { results: [] }],
    ["foreign", { results: [{ toolCallId: "foreign", outcome: AnswerBatchOutcome.RESOLVED }] }],
    [
      "duplicate",
      { results: [resolvedQuestion, { ...resolvedQuestion, outcome: AnswerBatchOutcome.SKIPPED }] },
    ],
    [
      "whitespace-padded",
      { results: [{ ...resolvedQuestion, toolCallId: " q " }, skippedApproval, resolvedDeclined] },
    ],
  ])("rejects %s result identity sets", async (_case, result) => {
    const transport = new FakeRpcTransport([
      {
        descriptor: AnswerService.method.answerBatch,
        result: create(AnswerService.method.answerBatch.output, {
          outcome: { case: "success", value: result },
        }),
      },
    ]);
    const client = new ApiClient(transport, unexpectedProjectOverflow);
    await expect(client.answerPromptBatch(batchRequest)).rejects.toThrow();
    expect(transport.attachedSessionCalls).toHaveLength(1);
  });
});
