import { create } from "@app/server-api-contract";
import * as pb from "@app/server-api-contract/gen/kent/api/workflow_definition/workflow_definition_pb";
import { StreamCompletionSchema } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import { RegistryProvider, useAtomSuspense } from "@effect/atom-react";
import { act, renderHook, waitFor } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import * as Effect from "effect/Effect";
import * as Stream from "effect/Stream";
import * as Atom from "effect/reactivity/Atom";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
import type { ApiService } from "./apiService";
import { ApiClient } from "./client";
import type { WorkflowProjectEvent } from "./workflowProjectEvents";

const workflowID = "11111111-1111-4111-8111-111111111111";
const projectService = pb.ProjectSubscriptionService.method;
const workflowService = pb.WorkflowSubscriptionService.method;
const event = create(pb.ProjectEventSchema, {
  action: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_QUESTION_WAITING,
  resource: pb.ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_TASK,
  occurredAt: { seconds: 0n, nanos: 1_000_000 },
  primaryEntityId: "task-1",
  projectId: "project-1",
  workflowId: workflowID,
  relatedIds: ["session-1", "ask-1"],
});
const projectedEvent: WorkflowProjectEvent = {
  action: "question_waiting",
  resource: "task",
  occurredAtUnixMs: 1,
  primaryEntityID: "task-1",
  projectID: "project-1",
  workflowID,
  relatedIDs: ["session-1", "ask-1"],
};

function transport() {
  return new FakeRpcTransport([
    {
      subscriptionDescriptor: projectService.subscribe,
      startResult: create(pb.ProjectSubscriptionStartResultSchema, {
        outcome: { case: "success", value: {} },
      }),
    },
    {
      subscriptionDescriptor: workflowService.subscribe,
      startResult: create(pb.WorkflowSubscriptionStartResultSchema, {
        outcome: { case: "success", value: {} },
      }),
    },
  ]);
}

describe("ApiClient workflow subscriptions", () => {
  it("adapts project events and dependency change pairs before feature code receives them", async () => {
    const rpc = transport();
    const client: ApiService = new ApiClient(rpc, unexpectedProjectOverflow);
    const events: WorkflowProjectEvent[] = [];
    observeProject(client, events);
    await act(async () => {
      rpc.emitDescriptor(projectService.subscribe, projectService.event, event);
      rpc.emitDescriptor(
        projectService.subscribe,
        projectService.event,
        create(pb.ProjectEventSchema, {
          ...event,
          action: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_DEPENDENCIES_CHANGED,
          relatedIds: ["task-2"],
        }),
      );
    });
    expect(events).toEqual([
      projectedEvent,
      {
        ...projectedEvent,
        action: "dependencies_changed",
        relatedIDs: ["task-2"],
      },
    ]);
  });

  it("adapts workflow-only events without delivering them to a Project subscription", async () => {
    const rpc = transport();
    const client = new ApiClient(rpc, unexpectedProjectOverflow);
    const workflowEvents: WorkflowProjectEvent[] = [];
    const projectEvents: WorkflowProjectEvent[] = [];
    observeProject(client, projectEvents);
    client.subscribeWorkflow(workflowID, eventCollector(workflowEvents));
    await act(async () => {
      rpc.emitDescriptor(
        workflowService.subscribe,
        workflowService.event,
        create(pb.ProjectEventSchema, {
          resource: pb.ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_WORKFLOW,
          action: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_GRAPH_SAVED,
          workflowId: workflowID,
          primaryEntityId: workflowID,
          occurredAt: event.occurredAt,
        }),
      );
    });
    expect(workflowEvents).toMatchObject([{ resource: "workflow", action: "graph_saved", projectID: null }]);
    expect(projectEvents).toEqual([]);
  });

  it("adapts Label catalog and Task assignment events through typed scopes", async () => {
    const rpc = transport();
    const client = new ApiClient(rpc, unexpectedProjectOverflow);
    const events: WorkflowProjectEvent[] = [];
    observeProject(client, events);
    rpc.emitDescriptor(
      projectService.subscribe,
      projectService.event,
      create(pb.ProjectEventSchema, {
        action: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_RENAMED,
        resource: pb.ProjectEventResource.WORKFLOW_PROJECT_EVENT_RESOURCE_LABEL,
        projectId: "project-1",
        primaryEntityId: "label-1",
        occurredAt: event.occurredAt,
      }),
    );
    rpc.emitDescriptor(
      projectService.subscribe,
      projectService.event,
      create(pb.ProjectEventSchema, {
        ...event,
        action: pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_LABELS_CHANGED,
        relatedIds: [],
      }),
    );
    await waitFor(() => {
      expect(events).toHaveLength(2);
    });
    expect(events).toMatchObject([
      { action: "renamed", resource: "label", workflowID: null, relatedIDs: [] },
      { action: "labels_changed", resource: "task", workflowID, relatedIDs: [] },
    ]);
  });

  it("surfaces malformed binary events without delivering an event", () => {
    const rpc = transport();
    const client = new ApiClient(rpc, unexpectedProjectOverflow);
    const events: WorkflowProjectEvent[] = [];
    const errors: Error[] = [];
    client.subscribeWorkflow(workflowID, eventCollector(events, errors));
    rpc.emitDescriptorBytes(workflowService.subscribe, new Uint8Array([0xff]));
    expect(events).toEqual([]);
    expect(errors).toHaveLength(1);
  });

  it("rejects actions that do not belong to the event resource", () => {
    const rpc = transport();
    const client = new ApiClient(rpc, unexpectedProjectOverflow);
    const events: WorkflowProjectEvent[] = [];
    client.subscribeWorkflow(workflowID, eventCollector(events));
    for (const action of [
      pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_LINKED,
      pb.ProjectEventAction.WORKFLOW_PROJECT_EVENT_ACTION_CANCELED,
    ]) {
      expect(() => {
        rpc.emitDescriptor(
          workflowService.subscribe,
          workflowService.event,
          create(pb.ProjectEventSchema, { ...event, action }),
        );
      }).toThrow();
    }
    expect(events).toEqual([]);
  });

  it("surfaces a failed start and delivers successful start and completion outcomes", () => {
    const rpc = new FakeRpcTransport([
      {
        subscriptionDescriptor: workflowService.subscribe,
        startResult: create(pb.WorkflowSubscriptionStartResultSchema, {
          outcome: {
            case: "error",
            value: {
              code: "workflow_not_found",
              detail: { case: "workflowNotFound", value: { workflowId: workflowID } },
            },
          },
        }),
      },
    ]);
    const errors: Error[] = [];
    new ApiClient(rpc, unexpectedProjectOverflow).subscribeWorkflow(workflowID, eventCollector([], errors));
    rpc.openDescriptor(workflowService.subscribe);
    expect(errors).toHaveLength(1);

    const success = transport();
    const opened = vi.fn();
    const completed = vi.fn();
    const subscription = new ApiClient(success, unexpectedProjectOverflow).subscribeWorkflow(workflowID, {
      ...eventCollector([]),
      onOpen: opened,
      onComplete: completed,
    });
    success.openDescriptor(workflowService.subscribe);
    expect(opened).toHaveBeenCalledOnce();
    success.completeDescriptor(
      workflowService.subscribe,
      workflowService.complete,
      create(StreamCompletionSchema, { code: -32000, message: "stream stopped" }),
    );
    expect(completed).toHaveBeenCalledWith(-32000, "stream stopped");
    subscription.close();
    expect(success.descriptorSubscriptions).toEqual([]);
  });
});

function observeProject(client: ApiService, events: WorkflowProjectEvent[], errors: Error[] = []) {
  const observation = Atom.make(
    Stream.runForEach(client.subscribeProject("project-1"), (value) =>
      Effect.sync(() => {
        if (value.kind === "event") events.push(value.event);
        if (value.kind === "error") errors.push(value.error);
      }),
    ).pipe(Effect.as(null)),
    { initialValue: null },
  );
  return renderHook(() => useAtomSuspense(observation), {
    wrapper: ({ children }: { children: ReactNode }) => createElement(RegistryProvider, { children }),
  });
}

function eventCollector(events: WorkflowProjectEvent[], errors: Error[] = []) {
  return {
    onEvent(event: WorkflowProjectEvent) {
      events.push(event);
    },
    onComplete() {
      return;
    },
    onError(error: Error) {
      errors.push(error);
    },
  };
}
