import { create } from "@app/server-api-contract";
import {
  AttentionReadService,
  type AttentionItem as GeneratedAttentionItem,
  type InterruptedCurrentNodeDetails,
} from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import { required } from "./chatWire";
import { timestampMillis } from "./clientTime";
import { taskCurrentNode } from "./clientTaskProjection";
import { ContractError } from "./errors";
import { approvalDecision } from "./promptPresentation";
import { requireUnarySuccess } from "./protobufRpc";
import { taskUnavailableTargetCause, workflowExecutionTargetMode } from "./workflowProtoValues";
import type { AttentionItem } from "./attention";
import type { AttentionPage, TaskAttention } from "./models";
import type { DescriptorRpcTransport } from "./transport";
import { requireTaskBoundItems } from "./clientParse";

export async function listAttention(transport: DescriptorRpcTransport, pageToken: string): Promise<AttentionPage> {
  const method = AttentionReadService.method.list;
  const response = requireUnarySuccess(method, await transport.callDescriptor(method, create(method.input, {
    pageSize: 40, pageToken: pageToken === "" ? undefined : pageToken,
  })));
  return {
    items: response.items.map(attentionItem),
    nextPageToken: response.nextPageToken ?? "",
    generatedAt: timestampMillis(response.generatedAt),
  };
}

export async function listTaskAttention(transport: DescriptorRpcTransport, taskId: string): Promise<TaskAttention> {
  const method = AttentionReadService.method.listTask;
  const response = requireUnarySuccess(method, await transport.callDescriptor(method, create(method.input, { taskId })));
  const items = response.items.map(attentionItem);
  requireTaskBoundItems(taskId, items);
  return { items, generatedAt: timestampMillis(response.generatedAt) };
}

export function attentionItem(value: GeneratedAttentionItem): AttentionItem {
  const base = {
    id: value.id, projectID: value.projectId, workflowID: value.workflowId,
    taskID: value.taskId, taskShortID: value.taskShortId, taskTitle: value.taskTitle,
    occurredAt: timestampMillis(value.occurredAt),
  };
  switch (value.detail.case) {
    case "question": {
      const detail = value.detail.value;
      const question = required(detail.question);
      const identity = { sessionID: question.sessionId, stepID: question.stepId, toolCallID: question.toolCallId };
      const item = {
        ...base, kind: "question" as const, message: detail.message ?? null,
        currentNode: taskCurrentNode(required(detail.currentNode)), sessionName: detail.sessionName ?? null,
      };
      switch (question.prompt.case) {
        case "ordinary":
          return { ...item, question: {
            ...identity, kind: "ordinary", suggestions: question.prompt.value.suggestions,
            recommendedOptionIndex: question.prompt.value.recommendedOptionIndex ?? null,
          } };
        case "approval":
          return { ...item, question: {
            ...identity, kind: "approval", approvalDecisions: question.prompt.value.approvalDecisions.map(approvalDecision),
            accessTargets: question.prompt.value.accessTargets.map((target) => ({
              requestedPath: target.requestedPath, resolvedPath: target.resolvedPath,
            })),
          } };
        default: throw new ContractError("Attention question has no prompt.");
      }
    }
    case "approval": {
      const detail = value.detail.value;
      const snapshot = required(detail.approvalSnapshot);
      return {
        ...base, kind: "approval", approvalID: detail.approvalId, message: detail.message ?? null,
        approvalSnapshot: {
          sourceNodeName: snapshot.sourceNodeDisplayName,
          targets: snapshot.targets.map((target) => ({ displayName: target.displayName })),
          commentary: snapshot.commentary ?? "", version: Number(snapshot.workflowRevisionSeen),
          outputValues: Object.fromEntries(snapshot.outputValues.map((output) => [output.name, output.value])),
        },
      };
    }
    case "interruptedCurrentNode": {
      const detail = value.detail.value;
      return {
        ...base, kind: "interrupted_current_node", message: detail.message ?? null,
        currentNode: taskCurrentNode(required(detail.currentNode)), sessionID: detail.sessionId ?? null,
        detailJSON: detail.details === undefined ? null : interruptionDiagnosticJSON(detail.details),
      };
    }
    default: throw new ContractError("Attention item has no detail.");
  }
}

export function interruptionDiagnosticJSON(value: InterruptedCurrentNodeDetails): string {
  const base = { Code: value.code, Fields: Object.fromEntries(value.fields.map((field) => [field.name, field.value])) };
  switch (value.detail.case) {
    case "generic": return JSON.stringify(base);
    case "configuredExecutionTargetUnavailable": {
      const detail = value.detail.value;
      return JSON.stringify({ ...base, configured_execution_target_unavailable: {
        mode: workflowExecutionTargetMode.decode(detail.mode), requested_ref: detail.requestedRef,
        cause: taskUnavailableTargetCause.decode(detail.cause),
      } });
    }
    case "originalExecutionTargetUnavailable":
      return JSON.stringify({ ...base, original_execution_target_unavailable: { cause: value.detail.value.cause } });
    default: throw new ContractError("Interruption has no detail.");
  }
}
