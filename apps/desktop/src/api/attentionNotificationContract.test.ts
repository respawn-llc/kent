import { describe, expect, it } from "bun:test";
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
        detail: {
          case: "question",
          value: {
            currentNode: { nodeId: "node-1" },
            question: {
              question: {
                case: "ask",
                value: {
                  sessionId: "11111111-1111-4111-8111-111111111111",
                  stepId: "22222222-2222-4222-8222-222222222222",
                  toolCallId: "ask-1",
                  question: "Proceed?",
                  createdAt: { seconds: 0n, nanos: 1_000_000 },
                },
              },
            },
          },
        },
      });
      if (seconds > 0n) expect(() => validate(T.AttentionItemSchema, question)).not.toThrow();
      else expect(() => validate(T.AttentionItemSchema, question)).toThrow();
      const interrupted = create(T.AttentionItemSchema, {
        ...base,
        detail: { case: "interruptedCurrentNode", value: { currentNode: { nodeId: "node-1" } } },
      });
      expect(() => validate(T.AttentionItemSchema, interrupted)).not.toThrow();
      const approval = create(T.AttentionItemSchema, {
        ...base,
        detail: {
          case: "approval",
          value: {
            approvalId: "approval-1",
            approvalSnapshot: { sourceNodeDisplayName: "Implement" },
          },
        },
      });
      expect(() => validate(T.AttentionItemSchema, approval)).not.toThrow();
      expect(() => validate(T.AttentionItemSchema, create(T.AttentionItemSchema, base))).toThrow();
      const missingOccurrence = create(T.AttentionItemSchema, {
        ...interrupted,
        occurredAt: undefined,
      });
      expect(() => validate(T.AttentionItemSchema, missingOccurrence)).toThrow();
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
      const resolved = create(T.ResolvedAttentionNotificationSchema, { id, occurredAt });
      const pending = create(T.AttentionNotificationSchema, {
        id,
        occurredAt,
        revision: 1n,
        state: { case: "workflowApproval", value: { approvalId: "approval-key" } },
        target: {
          target: {
            case: "workflowTask",
            value: {
              taskId: "task-1",
              workflowId: "22222222-2222-4222-8222-222222222222",
              focus: {
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
