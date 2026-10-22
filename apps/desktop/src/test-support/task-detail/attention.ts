import { create } from "@app/server-api-contract";
import * as taskAttention from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";

export const attentionBase = {
  projectId: "project-1",
  workflowId: "11111111-1111-4111-8111-111111111111",
  taskId: "task-1",
  taskShortId: "T-1",
  taskTitle: "Resolve blocker",
  occurredAt: { seconds: 0n, nanos: 1_000_000 },
};

export function taskQuestionPage(prompts: readonly (readonly [string, number])[], generatedAt = 3) {
  return create(taskAttention.TaskAttentionListSuccessSchema, {
    items: prompts.map(([askID, optionCount]) =>
      create(taskAttention.AttentionItemSchema, {
        ...attentionBase,
        id: `attention-${askID}`,
        kind: taskAttention.AttentionItemKind.QUESTION,
        detail: {
          case: "question",
          value: {
            message: askID,
            currentNode: { nodeId: "node-1" },
            sessionName: "Session one",
            question: {
              sessionId: "33333333-3333-4333-8333-333333333333",
              stepId: "22222222-2222-4222-8222-222222222222",
              toolCallId: askID,
              kind: taskAttention.AttentionQuestionKind.ORDINARY,
              prompt: {
                case: "ordinary",
                value: {
                  recommendedOptionIndex: 1,
                  suggestions: Array.from(
                    { length: optionCount },
                    (_, index) => `option-${String(index + 1)}`,
                  ),
                },
              },
            },
          },
        },
      }),
    ),
    generatedAt: { seconds: 0n, nanos: generatedAt * 1_000_000 },
  });
}
