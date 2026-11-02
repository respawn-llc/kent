import { create } from "@app/server-api-contract";
import * as pb from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import type { FakeRpcTransport } from "./index";

const service = pb.AttentionNotificationService.method;
export function attentionSubscriptionRoute() {
  return {
    subscriptionDescriptor: service.subscribe,
    startResult: create(pb.AttentionNotificationStartResultSchema, {
      outcome: { case: "success", value: {} },
    }),
  };
}

export function emitQuestionBatchRevision(transport: FakeRpcTransport, revision: number): void {
  transport.emitDescriptor(
    service.subscribe,
    service.event,
    create(pb.AttentionNotificationEventSchema, {
      sequence: BigInt(revision),
      type: pb.AttentionNotificationEventType.ATTENTION_NOTIFICATION_EVENT_PENDING,
      payload: {
        case: "pending",
        value: {
          id: { kind: pb.AttentionNotificationKind.QUESTION, uuid: "batch-1" },
          kind: pb.AttentionNotificationKind.QUESTION,
          revision: BigInt(revision),
          occurredAt: { seconds: 1_789_578_000n, nanos: 0 },
          target: {
            kind: pb.AttentionNotificationTargetKind.ATTENTION_NOTIFICATION_TARGET_SESSION_PROMPT,
            target: {
              case: "sessionPrompt",
              value: { projectId: "project-1", sessionId: "11111111-1111-4111-8111-111111111111" },
            },
          },
          state: {
            case: "question",
            value: {
              preparedAskIds: ["ask-1"],
              materializedAskIds: ["ask-1"],
              currentUnresolvedAskIds: ["ask-1"],
              displayCount: 1,
              materializedCount: 1,
            },
          },
        },
      },
    }),
  );
}
