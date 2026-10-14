import { create, validate } from "@app/server-api-contract";
import * as T from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";

describe("Attention notification contract", () => {
  it("limits Question occurrence positivity to Question Attention", () => {
    for (const seconds of [-1n, 0n, 1n]) {
      const base = {
        id: "attention-key",
        projectId: "project-1",
        workflowId: "22222222-2222-4222-8222-222222222222",
        taskId: "task-1",
        taskShortId: "T-1",
        taskTitle: "Task",
        occurredAt: { seconds, nanos: 0 },
      };
      const question = create(T.AttentionItemSchema, {
        ...base,
        kind: T.AttentionItemKind.QUESTION,
        detail: {
          case: "question",
          value: {
            message: "Proceed?",
            currentNode: { nodeId: "node-1" },
            question: {
              sessionId: "11111111-1111-4111-8111-111111111111",
              stepId: "22222222-2222-4222-8222-222222222222",
              toolCallId: "ask-1",
              kind: T.AttentionQuestionKind.ORDINARY,
              prompt: { case: "ordinary", value: {} },
            },
          },
        },
      });
      if (seconds > 0n) expect(() => validate(T.AttentionItemSchema, question)).not.toThrow();
      else expect(() => validate(T.AttentionItemSchema, question)).toThrow();
      const interrupted = create(T.AttentionItemSchema, {
        ...base,
        kind: T.AttentionItemKind.INTERRUPTED_CURRENT_NODE,
        detail: { case: "interruptedCurrentNode", value: { currentNode: { nodeId: "node-1" } } },
      });
      expect(() => validate(T.AttentionItemSchema, interrupted)).not.toThrow();
    }
  });
  it("rejects blank notification keys without imposing UUID spelling", () => {
    for (const uuid of [" \t ", "approval-key"]) {
      const message = create(T.AttentionNotificationIDSchema, {
        kind: T.AttentionNotificationKind.WORKFLOW_APPROVAL,
        uuid,
      });
      if (uuid === "approval-key")
        expect(() => validate(T.AttentionNotificationIDSchema, message)).not.toThrow();
      else expect(() => validate(T.AttentionNotificationIDSchema, message)).toThrow();
    }
  });

  it("requires substantive workflow approval and Session prompt Project identities", () => {
    expect(() =>
      validate(T.WorkflowApprovalStateSchema, create(T.WorkflowApprovalStateSchema, { approvalId: " \t " })),
    ).toThrow();
    expect(() =>
      validate(
        T.WorkflowApprovalStateSchema,
        create(T.WorkflowApprovalStateSchema, { approvalId: "approval-key" }),
      ),
    ).not.toThrow();
    const sessionId = "11111111-1111-4111-8111-111111111111";
    expect(() =>
      validate(
        T.SessionPromptAttentionTargetSchema,
        create(T.SessionPromptAttentionTargetSchema, { sessionId, projectId: " \t " }),
      ),
    ).toThrow();
    expect(() =>
      validate(
        T.SessionPromptAttentionTargetSchema,
        create(T.SessionPromptAttentionTargetSchema, { sessionId, projectId: "project-key" }),
      ),
    ).not.toThrow();
  });

  it("rejects Go zero occurrence while allowing epoch and pre-epoch occurrences", () => {
    for (const seconds of [-62135596800n, 0n, -1n]) {
      const id = { kind: T.AttentionNotificationKind.WORKFLOW_APPROVAL, uuid: "approval-key" };
      const occurredAt = { seconds, nanos: 0 };
      const resolved = create(T.ResolvedAttentionNotificationSchema, { id, kind: id.kind, occurredAt });
      const pending = create(T.AttentionNotificationSchema, {
        id,
        kind: id.kind,
        occurredAt,
        revision: 1n,
        state: { case: "workflowApproval", value: { approvalId: "approval-key" } },
        target: {
          kind: T.AttentionNotificationTargetKind.ATTENTION_NOTIFICATION_TARGET_WORKFLOW_TASK,
          target: {
            case: "workflowTask",
            value: {
              taskId: "task-1",
              workflowId: "22222222-2222-4222-8222-222222222222",
              focus: {
                kind: T.AttentionNotificationFocusKind.ATTENTION_NOTIFICATION_FOCUS_APPROVAL,
                focus: { case: "approval", value: { approvalId: "approval-key" } },
              },
            },
          },
        },
      });
      if (seconds === -62135596800n) {
        expect(() => validate(T.ResolvedAttentionNotificationSchema, resolved)).toThrow();
        expect(() => validate(T.AttentionNotificationSchema, pending)).toThrow();
      } else {
        expect(() => validate(T.ResolvedAttentionNotificationSchema, resolved)).not.toThrow();
        expect(() => validate(T.AttentionNotificationSchema, pending)).not.toThrow();
      }
    }
  });
});
