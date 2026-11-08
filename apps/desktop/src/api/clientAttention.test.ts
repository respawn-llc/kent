import { expect, it } from "bun:test";
import { create } from "@app/server-api-contract";
import * as pb from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import {
  ApprovalDecision,
  ApprovalSchema,
  ObservationQuestionSchema,
  QuestionSchema,
} from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import { ApiClient } from "./client";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";

const base = {
  id: "attention-1",
  projectId: "project-1",
  workflowId: "11111111-1111-4111-8111-111111111111",
  taskId: "task-1",
  taskShortId: "T-1",
  taskTitle: "Task",
  occurredAt: { seconds: 0n, nanos: 1_000_000 },
};

function client(question: pb.QuestionAttention) {
  return new ApiClient(
    new FakeRpcTransport([
      {
        descriptor: pb.AttentionReadService.method.listTask,
        result: create(pb.TaskAttentionListResultSchema, {
          outcome: {
            case: "success",
            value: {
              items: [
                {
                  ...base,
                  detail: { case: "question", value: question },
                },
              ],
            },
          },
        }),
      },
    ]),
    unexpectedProjectOverflow,
  );
}

it("keeps a structured access Approval without question copy", async () => {
  const question = create(pb.QuestionAttentionSchema, {
    currentNode: { nodeId: "node-1" },
    sessionName: "Session one",
    question: {
      question: {
        case: "approval",
        value: {
          sessionId: "11111111-1111-4111-8111-111111111111",
          stepId: "22222222-2222-4222-8222-222222222222",
          toolCallId: "ask-1",
          createdAt: { seconds: 1n },
          batch: { toolCallIds: ["ask-1", "ask-2"], unmaterializedCount: 1 },
          options: [{ decision: ApprovalDecision.ALLOW_ONCE }, { decision: ApprovalDecision.DENY }],
          accessTargets: [{ requestedPath: " /alias/a ", resolvedPath: " /real/file " }],
        },
      },
    },
  });
  expect(client(question).listTaskAttention("task-1")).resolves.toMatchObject({
    items: [
      {
        currentNode: { nodeID: "node-1" },
        sessionName: "Session one",
        question: {
          kind: "approval",
          sessionID: "11111111-1111-4111-8111-111111111111",
          stepID: "22222222-2222-4222-8222-222222222222",
          toolCallID: "ask-1",
          question: null,
          createdAt: "1970-01-01T00:00:01.000Z",
          batch: { toolCallIDs: ["ask-1", "ask-2"], unmaterializedCount: 1 },
          approvalDecisions: ["allow_once", "deny"],
          accessTargets: [{ requestedPath: " /alias/a ", resolvedPath: " /real/file " }],
        },
      },
    ],
  });
  const withoutTargets = create(pb.QuestionAttentionSchema, {
    currentNode: question.currentNode,
    question: {
      question: {
        case: "approval",
        value: {
          sessionId: "11111111-1111-4111-8111-111111111111",
          stepId: "22222222-2222-4222-8222-222222222222",
          toolCallId: "ask-1",
          createdAt: { seconds: 1n },
          options: [{ decision: ApprovalDecision.DENY }],
        },
      },
    },
  });
  expect(client(withoutTargets).listTaskAttention("task-1")).rejects.toThrow();
  expect(
    client(
      create(pb.QuestionAttentionSchema, {
        ...question,
        question: create(ObservationQuestionSchema, {
          question: {
            case: "approval",
            value: create(ApprovalSchema, {
              sessionId: "11111111-1111-4111-8111-111111111111",
              stepId: "22222222-2222-4222-8222-222222222222",
              toolCallId: "ask-1",
              createdAt: { seconds: 1n },
              options: [{ decision: ApprovalDecision.DENY }],
              question: "",
              accessTargets: [{ requestedPath: "/alias/a", resolvedPath: "/real/file" }],
            }),
          },
        }),
      }),
    ).listTaskAttention("task-1"),
  ).rejects.toThrow();
});

it("preserves the full Question and Task context without inventing a recommendation", async () => {
  const ask = create(QuestionSchema, {
    sessionId: "11111111-1111-4111-8111-111111111111",
    stepId: "22222222-2222-4222-8222-222222222222",
    toolCallId: "ask-1",
    question: "Proceed?",
    suggestions: ["Yes", "No"],
    createdAt: { seconds: 5n },
    batch: { toolCallIds: ["ask-1", "ask-2"], unmaterializedCount: 1 },
  });
  const result = await client(
    create(pb.QuestionAttentionSchema, {
      currentNode: { nodeId: "node-1" },
      sessionName: "Session one",
      question: { question: { case: "ask", value: ask } },
    }),
  ).listTaskAttention("task-1");
  expect(result.items[0]).toMatchObject({
    id: base.id,
    taskID: base.taskId,
    currentNode: { nodeID: "node-1" },
    sessionName: "Session one",
    question: {
      kind: "ordinary",
      sessionID: ask.sessionId,
      stepID: ask.stepId,
      toolCallID: ask.toolCallId,
      question: ask.question,
      suggestions: ask.suggestions,
      recommendedOptionIndex: null,
      createdAt: "1970-01-01T00:00:05.000Z",
      batch: { toolCallIDs: ["ask-1", "ask-2"], unmaterializedCount: 1 },
    },
  });
});
