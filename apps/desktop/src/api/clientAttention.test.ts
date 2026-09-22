import { create } from "@app/server-api-contract";
import * as pb from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import { ApprovalDecision } from "@app/server-api-contract/gen/kent/api/prompt/prompt_pb";
import { ApiClient } from "./client";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";

const base = {
  id: "attention-1", projectId: "project-1", workflowId: "11111111-1111-4111-8111-111111111111",
  taskId: "task-1", taskShortId: "T-1", taskTitle: "Task",
  occurredAt: { seconds: 0n, nanos: 1_000_000 },
};

function client(question: pb.QuestionAttention) {
  return new ApiClient(new FakeRpcTransport([{
    descriptor: pb.AttentionReadService.method.listTask,
    result: create(pb.TaskAttentionListResultSchema, { outcome: { case: "success", value: {
      generatedAt: { seconds: 0n, nanos: 1_000_000 },
      items: [{ ...base, kind: pb.AttentionItemKind.QUESTION, detail: { case: "question", value: question } }],
    } } }),
  }]), unexpectedProjectOverflow);
}

it("allows absent attention text only for a structured access Approval", async () => {
  const question = create(pb.QuestionAttentionSchema, {
    currentNode: { nodeId: "node-1" },
    question: {
      sessionId: "session-1", stepId: "22222222-2222-4222-8222-222222222222", toolCallId: "ask-1",
      kind: pb.AttentionQuestionKind.APPROVAL,
      prompt: { case: "approval", value: {
        approvalDecisions: [ApprovalDecision.ALLOW_ONCE, ApprovalDecision.DENY],
        accessTargets: [{ requestedPath: " /alias/a ", resolvedPath: " /real/file " }],
      } },
    },
  });
  await expect(client(question).listTaskAttention("task-1")).resolves.toMatchObject({ items: [{
    message: null, question: { toolCallID: "ask-1", approvalDecisions: ["allow_once", "deny"],
      accessTargets: [{ requestedPath: " /alias/a ", resolvedPath: " /real/file " }] },
  }] });
  const withoutTargets = create(pb.QuestionAttentionSchema, {
    currentNode: question.currentNode,
    question: {
      sessionId: "session-1", stepId: "22222222-2222-4222-8222-222222222222", toolCallId: "ask-1",
      kind: pb.AttentionQuestionKind.APPROVAL,
      prompt: { case: "approval", value: { approvalDecisions: [ApprovalDecision.DENY] } },
    },
  });
  await expect(client(withoutTargets).listTaskAttention("task-1")).rejects.toThrow();
  await expect(client(create(pb.QuestionAttentionSchema, { ...question, message: "" })).listTaskAttention("task-1"))
    .rejects.toThrow();
});
