import { create } from "@app/server-api-contract";
import * as pb from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import { StreamCompletionSchema } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import type {
  AttentionNotification,
  AttentionNotificationEvent,
  AttentionNotificationLifecycle,
  AttentionNotificationID,
  AttentionNotificationTarget,
  AttentionNotificationTaskDetailFocus,
} from "./attentionNotifications";
import { enumValue } from "./chatWire";
import { interruptionDiagnosticJSON } from "./clientAttention";
import { timestampMillis } from "./clientTime";
import { ContractError } from "./errors";
import { requireUnarySuccess, streamCompletionFailure } from "./protobufRpc";
import type { RpcTransport } from "./transport";
import { subscriptionStream } from "./subscriptionStream";

export function attentionNotifications(transport: RpcTransport, reportOverflow: () => Promise<void>) {
  const method = pb.AttentionNotificationService.method.subscribe;
  return subscriptionStream<AttentionNotificationLifecycle>(
    (emit) =>
      transport.subscribeDescriptor({
        method,
        request: create(method.input),
        eventDescriptor: pb.AttentionNotificationEventSchema,
        completionDescriptor: StreamCompletionSchema,
        onStart(result) {
          requireUnarySuccess(method, result);
        },
        handler: {
          onOpen: () => {
            emit({ kind: "open" });
          },
          onError: (error) => {
            emit({ kind: "error", error });
          },
          onComplete(completion) {
            emit({ kind: "complete", code: completion.code ?? null, message: completion.message ?? null });
            return streamCompletionFailure(completion);
          },
          onEvent(event) {
            emit({ kind: "event", event: attentionEvent(event) });
          },
        },
      }),
    reportOverflow,
  );
}

function attentionID(value: pb.AttentionNotificationID): AttentionNotificationID {
  return {
    uuid: value.uuid,
    kind: enumValue(value.kind, {
      [pb.AttentionNotificationKind.QUESTION]: "question",
      [pb.AttentionNotificationKind.APPROVAL]: "approval",
      [pb.AttentionNotificationKind.WORKFLOW_APPROVAL]: "workflow_approval",
      [pb.AttentionNotificationKind.INTERRUPTED_CURRENT_NODE]: "interrupted_current_node",
    }),
  };
}

function attentionEvent(event: pb.AttentionNotificationEventValid): AttentionNotificationEvent {
  const sequence = Number(event.sequence);
  switch (event.payload.case) {
    case "sessionTurnEnd": {
      const completion = event.payload.value;
      return {
        type: "session_turn_end",
        sequence,
        shortID: completion.shortId === undefined ? null : Number(completion.shortId),
        sessionName: completion.sessionName ?? null,
        assistantContent: completion.assistantContent,
        completedAt: timestampMillis(completion.completedAt),
        target: { kind: "session_prompt", projectID: completion.projectId, sessionID: completion.sessionId },
      };
    }
    case "sessionNamingFailure": {
      const failure = event.payload.value;
      return {
        type: "session_naming_failure",
        sequence,
        shortID: failure.shortId === undefined ? null : Number(failure.shortId),
        target: { kind: "session_prompt", projectID: failure.projectId, sessionID: failure.sessionId },
        error: { cause: failure.error.cause ?? null },
      };
    }
    case "pending":
      return { type: "pending", sequence, pending: attentionNotification(event.payload.value) };
    case "resolved": {
      const value = event.payload.value;
      const id = attentionID(value.id);
      return {
        type: "resolved",
        sequence,
        id,
        kind: id.kind,
        occurredAt: new Date(timestampMillis(value.occurredAt)).toISOString(),
      };
    }
    case undefined:
      throw new ContractError("Attention notification has no event.");
  }
}

function attentionNotification(value: pb.AttentionNotificationValid): AttentionNotification {
  const id = attentionID(value.id);
  const base: AttentionNotification = {
    id,
    kind: id.kind,
    occurredAt: new Date(timestampMillis(value.occurredAt)).toISOString(),
    revision: Number(value.revision),
    target: attentionTarget(value.target),
    question: null,
    approval: null,
    workflowApproval: null,
    interruptedCurrentNode: null,
  };
  switch (value.state.case) {
    case "question": {
      const state = value.state.value;
      return {
        ...base,
        question: {
          preparedAskIDs: state.preparedAskIds,
          materializedAskIDs: state.materializedAskIds,
          currentUnresolvedAskIDs: state.currentUnresolvedAskIds,
          skippedAskIDs: state.skippedAskIds,
          preview: state.preview,
          displayCount: state.displayCount,
          materializedCount: state.materializedCount,
        },
      };
    }
    case "approval":
      return {
        ...base,
        approval: {
          message: value.state.value.message,
          accessTargets: value.state.value.accessTargets.map((target) => ({
            requestedPath: target.requestedPath,
            resolvedPath: target.resolvedPath,
          })),
        },
      };
    case "workflowApproval":
      return {
        ...base,
        workflowApproval: {
          approvalID: value.state.value.approvalId,
          message: value.state.value.message,
        },
      };
    case "interruptedCurrentNode": {
      const state = value.state.value;
      return {
        ...base,
        interruptedCurrentNode: {
          message: state.message,
          reason: state.reason,
          detailJSON: state.details === undefined ? undefined : interruptionDiagnosticJSON(state.details),
        },
      };
    }
    case undefined:
      throw new ContractError("Attention notification has no state.");
  }
}

function attentionTarget(value: pb.AttentionNotificationTargetValid): AttentionNotificationTarget {
  switch (value.target.case) {
    case "sessionPrompt":
      return {
        kind: "session_prompt",
        projectID: value.target.value.projectId,
        sessionID: value.target.value.sessionId,
      };
    case "workflowTask": {
      const target = value.target.value;
      return {
        kind: "workflow_task",
        projectID: target.projectId,
        workflowID: target.workflowId,
        taskID: target.taskId,
        taskShortID: target.taskShortId,
        taskTitle: target.taskTitle,
        sessionID: target.sessionId,
        currentNodeID: target.currentNodeId,
        currentNodeBranchKey: target.currentNodeBranchKey,
        focus: attentionFocus(target.focus),
      };
    }
    case undefined:
      throw new ContractError("Attention notification has no target.");
  }
}

function attentionFocus(value: pb.AttentionNotificationTaskFocus): AttentionNotificationTaskDetailFocus {
  switch (value.focus.case) {
    case "question":
      return { kind: "question", askIDs: value.focus.value.askIds };
    case "approval":
      return { kind: "approval", approvalID: value.focus.value.approvalId };
    case "interruptedCurrentNode":
      return { kind: "interrupted_current_node" };
    case undefined:
      throw new ContractError("Attention notification has no focus.");
  }
}
