import { create } from "@app/server-api-contract";
import {
  ProjectEventSchema,
  ProjectSubscriptionService,
  WorkflowSubscriptionService,
  type ProjectEvent,
} from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import { StreamCompletionSchema, type StreamCompletion } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import { timestampMillis } from "./clientTime";
import { required } from "./chatWire";
import { requireUnarySuccess } from "./protobufRpc";
import { workflowEventAction, workflowEventResource } from "./workflowProtoValues";
import type { DescriptorRpcTransport, DescriptorSubscriptionHandler } from "./transport";

export type WorkflowProjectEventResource = ReturnType<typeof workflowEventResource.decode>;
export type WorkflowProjectEventAction = ReturnType<typeof workflowEventAction.decode>;

export type WorkflowProjectEvent = Readonly<{
  action: WorkflowProjectEventAction;
  occurredAtUnixMs: number;
  primaryEntityID: string;
  projectID: string | null;
  relatedIDs: readonly string[];
  resource: WorkflowProjectEventResource;
  workflowID: string | null;
}>;

export type WorkflowProjectEventHandler = Readonly<{
  onOpen?(): void;
  onEvent(event: WorkflowProjectEvent): void;
  onComplete(code: number, message: string): void;
  onError(error: Error): void;
}>;

export function subscribeWorkflow(transport: DescriptorRpcTransport, workflowId: string, handler: WorkflowProjectEventHandler) {
  const method = WorkflowSubscriptionService.method.subscribe;
  return transport.subscribeDescriptor({
    method, request: create(method.input, { workflowId }),
    eventDescriptor: ProjectEventSchema, completionDescriptor: StreamCompletionSchema,
    onStart(result) { requireUnarySuccess(method, result); },
    handler: workflowEventHandler(handler),
  });
}

export function subscribeWorkflowProject(transport: DescriptorRpcTransport, projectId: string, handler: WorkflowProjectEventHandler) {
  const method = ProjectSubscriptionService.method.subscribe;
  return transport.subscribeDescriptor({
    method, request: create(method.input, { projectId }),
    eventDescriptor: ProjectEventSchema, completionDescriptor: StreamCompletionSchema,
    onStart(result) { requireUnarySuccess(method, result); },
    handler: workflowEventHandler(handler),
  });
}

function workflowEventHandler(handler: WorkflowProjectEventHandler): DescriptorSubscriptionHandler<ProjectEvent, StreamCompletion> {
  return {
    ...(handler.onOpen === undefined ? {} : { onOpen: handler.onOpen }),
    onError: handler.onError,
    onComplete(completion) {
      handler.onComplete(completion.code ?? 0, completion.message ?? "");
      return undefined;
    },
    onEvent(event) {
      handler.onEvent({
        action: workflowEventAction.decode(event.action), resource: workflowEventResource.decode(event.resource),
        occurredAtUnixMs: timestampMillis(required(event.occurredAt)), primaryEntityID: event.primaryEntityId,
        projectID: event.projectId ?? null, workflowID: event.workflowId ?? null, relatedIDs: event.relatedIds,
      });
    },
  };
}
