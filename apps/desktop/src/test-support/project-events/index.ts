import { create } from "@app/server-api-contract";
import * as pb from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import { StreamCompletionSchema } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import type { FakeRpcTransport } from "../api";
import { vi } from "vitest";

export function recordProjectObservers(
  transport: FakeRpcTransport,
  opened: (handler: Readonly<{ onError(error: Error): void }>) => void,
  closed: () => void,
) {
  const subscribe = transport.subscribeDescriptor.bind(transport);
  vi.spyOn(transport, "subscribeDescriptor").mockImplementation((input) => {
    opened(input.handler);
    const subscription = subscribe(input);
    return {
      close() {
        closed();
        subscription.close();
      },
    };
  });
}

type EventAction =
  | "updated"
  | "moved"
  | "dependencies_changed"
  | "comment_added"
  | "labels_changed"
  | "question_waiting"
  | "question_cleared"
  | "renamed"
  | "graph_saved"
  | "linked";
type EventResource = "task" | "label" | "workflow" | "workflow_link";

const actions: Record<EventAction, pb.ProjectEventAction> = {
  updated: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_UPDATED,
  moved: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_MOVED,
  dependencies_changed: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_DEPENDENCIES_CHANGED,
  comment_added: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_COMMENT_ADDED,
  labels_changed: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_LABELS_CHANGED,
  question_waiting: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_QUESTION_WAITING,
  question_cleared: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_QUESTION_CLEARED,
  renamed: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_RENAMED,
  graph_saved: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_GRAPH_SAVED,
  linked: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_LINKED,
};

const resources: Record<EventResource, pb.ProjectEventResource> = {
  task: pb.ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_TASK,
  label: pb.ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_LABEL,
  workflow: pb.ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_WORKFLOW,
  workflow_link: pb.ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_WORKFLOW_LINK,
};

export function projectEventsFixture(transport: FakeRpcTransport) {
  const service = pb.ProjectSubscriptionService.method;
  return {
    get activeCount() {
      return transport.descriptorSubscriptions.filter((descriptor) => descriptor === service.subscribe)
        .length;
    },
    get startCount() {
      return transport.descriptorSubscriptionStarts.filter((entry) => entry.descriptor === service.subscribe)
        .length;
    },
    fail(error: Error) {
      transport.failDescriptor(service.subscribe, error);
    },
    open() {
      transport.openDescriptor(service.subscribe);
    },
    completeWithGap() {
      transport.completeDescriptor(
        service.subscribe,
        service.complete,
        create(StreamCompletionSchema, { code: -32010, message: "stream gap" }),
      );
    },
    emit(
      input: Readonly<{
        action: EventAction;
        entityID?: string;
        resource?: EventResource;
        projectID?: string;
        workflowID?: string;
        relatedIDs?: readonly string[];
      }>,
    ) {
      const resource = input.resource ?? "task";
      transport.emitDescriptor(
        service.subscribe,
        service.event,
        create(pb.ProjectEventSchema, {
          resource: resources[resource],
          action: actions[input.action],
          primaryEntityId: input.entityID ?? "task-1",
          projectId: input.projectID ?? "project-1",
          workflowId:
            resource === "label" ? undefined : (input.workflowID ?? "11111111-1111-4111-8111-111111111111"),
          relatedIds: [...(input.relatedIDs ?? [])],
          occurredAt: { seconds: 1n, nanos: 0 },
        }),
      );
    },
  };
}
