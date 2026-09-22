import { create, decode, encode, type DescMessage } from "@app/server-api-contract";
import * as pb from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import type { FakeRoute, FakeRpcTransport } from "../api";

export function globalAttentionPage(items: readonly pb.AttentionItem[], nextPageToken?: string) {
  return create(pb.AttentionListResultSchema, {
    outcome: {
      case: "success",
      value: {
        items: [...items],
        nextPageToken,
        generatedAt: { seconds: 0n, nanos: 1_000_000 },
      },
    },
  });
}

export function globalAttentionRoute(
  page: (token: string | null, callIndex: number) => pb.AttentionListResult,
): FakeRoute {
  return {
    descriptor: pb.AttentionReadService.method.list,
    resultFactory: (request, callIndex) => {
      const params = decode(
        pb.AttentionListRequestSchema,
        encode<DescMessage>(pb.AttentionListRequestSchema, request),
      );
      return page(params.pageToken ?? null, callIndex);
    },
  };
}

export function globalAttentionRequests(transport: FakeRpcTransport): readonly (string | null)[] {
  return transport.descriptorCalls
    .filter((call) => call.descriptor === pb.AttentionReadService.method.list)
    .map(
      (call) =>
        decode(pb.AttentionListRequestSchema, encode<DescMessage>(call.descriptor.input, call.request))
          .pageToken ?? null,
    );
}

export function approvalAttentionFixture(taskID: string) {
  return create(pb.AttentionItemSchema, {
    id: `approval:${taskID}`,
    kind: pb.AttentionItemKind.APPROVAL,
    projectId: "project-1",
    workflowId: "11111111-1111-4111-8111-111111111111",
    taskId: taskID,
    taskShortId: taskID,
    taskTitle: taskID,
    detail: {
      case: "approval",
      value: {
        approvalId: `approval-${taskID}`,
        message: "Approval required",
        approvalSnapshot: {
          sourceNodeDisplayName: "Review",
          targets: [{ displayName: "Done" }],
          commentary: "",
          workflowRevisionSeen: 1n,
        },
      },
    },
    occurredAt: { seconds: 0n, nanos: 1_000_000 },
  });
}

const workflowID = "11111111-1111-4111-8111-111111111111";

const pendingQuestion = create(pb.AttentionNotificationSchema, {
  id: { kind: pb.AttentionNotificationKind.QUESTION, uuid: "step-1" },
  kind: pb.AttentionNotificationKind.QUESTION,
  occurredAt: { seconds: 1n, nanos: 0 },
  revision: 1n,
  state: {
    case: "question",
    value: {
      preparedAskIds: ["ask-1"],
      materializedAskIds: ["ask-1"],
      currentUnresolvedAskIds: ["ask-1"],
      skippedAskIds: [],
      preview: "Question from agent",
      displayCount: 1,
      materializedCount: 1,
    },
  },
  target: {
    kind: pb.AttentionNotificationTargetKind.ATTENTION_NOTIFICATION_TARGET_WORKFLOW_TASK,
    target: {
      case: "workflowTask",
      value: {
        projectId: "project-1",
        workflowId: workflowID,
        taskId: "task-1",
        taskShortId: "KT-1",
        taskTitle: "Needs answer",
        sessionId: "session-1",
        currentNodeId: "node-1",
        focus: {
          kind: pb.AttentionNotificationFocusKind.ATTENTION_NOTIFICATION_FOCUS_QUESTION,
          focus: { case: "question", value: { askIds: ["ask-1"] } },
        },
      },
    },
  },
});

const pendingQuestionEvent = create(pb.AttentionNotificationEventSchema, {
  type: pb.AttentionNotificationEventType.ATTENTION_NOTIFICATION_EVENT_PENDING,
  sequence: 1n,
  payload: { case: "pending", value: pendingQuestion },
});

export const questionAttentionItem = create(pb.AttentionItemSchema, {
  id: "question:node-1:ask-1",
  kind: pb.AttentionItemKind.QUESTION,
  projectId: "project-1",
  workflowId: workflowID,
  taskId: "task-1",
  taskShortId: "KT-1",
  taskTitle: "Needs answer",
  detail: {
    case: "question",
    value: {
      currentNode: { nodeId: "node-1" },
      sessionName: "Session one",
      message: "Question from agent",
      question: {
        sessionId: "session-1",
        stepId: "22222222-2222-4222-8222-222222222222",
        toolCallId: "ask-1",
        kind: pb.AttentionQuestionKind.ORDINARY,
        prompt: { case: "ordinary", value: {} },
      },
    },
  },
  occurredAt: { seconds: 0n, nanos: 1_000_000 },
});

export function attentionEventsFixture(transport: FakeRpcTransport) {
  const service = pb.AttentionNotificationService.method;
  const emit = (event: pb.AttentionNotificationEvent) =>
    { transport.emitDescriptor(service.subscribe, service.event, event); };
  return {
    get activeCount() {
      return transport.descriptorSubscriptions.filter((descriptor) => descriptor === service.subscribe)
        .length;
    },
    pendingTaskQuestion() {
      emit(pendingQuestionEvent);
    },
    resolveQuestion() {
      emit(
        create(pb.AttentionNotificationEventSchema, {
          type: pb.AttentionNotificationEventType.ATTENTION_NOTIFICATION_EVENT_RESOLVED,
          sequence: 1n,
          payload: {
            case: "resolved",
            value: {
              id: pendingQuestion.id,
              kind: pb.AttentionNotificationKind.QUESTION,
              occurredAt: { seconds: 1n, nanos: 0 },
            },
          },
        }),
      );
    },
    pendingSessionQuestion() {
      emit(
        create(pb.AttentionNotificationEventSchema, {
          type: pb.AttentionNotificationEventType.ATTENTION_NOTIFICATION_EVENT_PENDING,
          sequence: 1n,
          payload: {
            case: "pending",
            value: create(pb.AttentionNotificationSchema, {
              ...pendingQuestion,
              target: create(pb.AttentionNotificationTargetSchema, {
                kind: pb.AttentionNotificationTargetKind.ATTENTION_NOTIFICATION_TARGET_SESSION_PROMPT,
                target: { case: "sessionPrompt", value: { projectId: "project-1", sessionId: "session-1" } },
              }),
            }),
          },
        }),
      );
    },
  };
}
