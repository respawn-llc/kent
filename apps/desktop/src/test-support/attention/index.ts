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
  occurredAt: { seconds: 1n, nanos: 0 },
  revision: 1n,
  state: {
    case: "question",
    value: {
      preview: "Question from agent",
      displayCount: 1,
    },
  },
  target: {
    target: {
      case: "workflowTask",
      value: {
        projectId: "project-1",
        workflowId: workflowID,
        taskId: "task-1",
        taskShortId: "KT-1",
        taskTitle: "Needs answer",
        sessionId: "11111111-1111-4111-8111-111111111111",
        currentNodeId: "node-1",
        focus: {
          focus: { case: "question", value: { askIds: ["ask-1"] } },
        },
      },
    },
  },
});

const pendingQuestionEvent = create(pb.AttentionNotificationEventSchema, {
  sequence: 1n,
  payload: { case: "pending", value: pendingQuestion },
});

export const questionAttentionItem = create(pb.AttentionItemSchema, {
  id: "question:node-1:ask-1",
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
      question: {
        question: {
          case: "ask",
          value: {
            sessionId: "11111111-1111-4111-8111-111111111111",
            stepId: "22222222-2222-4222-8222-222222222222",
            toolCallId: "ask-1",
            question: "Question from agent",
            createdAt: { seconds: 0n, nanos: 1_000_000 },
          },
        },
      },
    },
  },
  occurredAt: { seconds: 0n, nanos: 1_000_000 },
});

export function attentionEventsFixture(transport: FakeRpcTransport) {
  const service = pb.AttentionNotificationService.method;
  const emit = (event: pb.AttentionNotificationEvent) => {
    transport.emitDescriptor(service.subscribe, service.event, event);
  };
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
          sequence: 1n,
          payload: {
            case: "resolved",
            value: {
              id: pendingQuestion.id,
              occurredAt: { seconds: 1n, nanos: 0 },
            },
          },
        }),
      );
    },
    pendingSessionQuestion() {
      emit(
        create(pb.AttentionNotificationEventSchema, {
          sequence: 1n,
          payload: {
            case: "pending",
            value: create(pb.AttentionNotificationSchema, {
              ...pendingQuestion,
              target: create(pb.AttentionNotificationTargetSchema, {
                target: {
                  case: "sessionPrompt",
                  value: {
                    projectId: "project-1",
                    sessionId: "11111111-1111-4111-8111-111111111111",
                  },
                },
              }),
            }),
          },
        }),
      );
    },
  };
}
