import { create } from "@app/server-api-contract";
import * as pb from "@app/server-api-contract/gen/kent/api/workflow_task/attention_pb";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
import { ApiClient } from "./client";
import type { AttentionNotificationEvent } from "./attentionNotifications";
import { act, renderHook, waitFor } from "@testing-library/react";
import { RegistryProvider, useAtomSuspense } from "@effect/atom-react";
import { createElement, type ReactNode } from "react";
import * as Atom from "effect/reactivity/Atom";
import * as Stream from "effect/Stream";
import * as Effect from "effect/Effect";

const service = pb.AttentionNotificationService.method;
const occurredAt = { seconds: 1_789_578_000n, nanos: 0 };
const sessionTarget = create(pb.AttentionNotificationTargetSchema, {
  kind: pb.AttentionNotificationTargetKind.ATTENTION_NOTIFICATION_TARGET_SESSION_PROMPT,
  target: { case: "sessionPrompt", value: { projectId: "project-1", sessionId: "session-1" } },
});
const question = create(pb.AttentionNotificationSchema, {
  id: { kind: pb.AttentionNotificationKind.QUESTION, uuid: "batch-1" },
  kind: pb.AttentionNotificationKind.QUESTION,
  occurredAt,
  revision: 1n,
  target: sessionTarget,
  state: {
    case: "question",
    value: {
      preparedAskIds: ["ask-1", "ask-2"],
      materializedAskIds: ["ask-1"],
      currentUnresolvedAskIds: ["ask-1"],
      displayCount: 2,
      materializedCount: 1,
    },
  },
});

async function observe() {
  const transport = new FakeRpcTransport([
    {
      subscriptionDescriptor: service.subscribe,
      startResult: create(pb.AttentionNotificationStartResultSchema, {
        outcome: { case: "success", value: {} },
      }),
    },
  ]);
  const events: AttentionNotificationEvent[] = [];
  const errors: Error[] = [];
  const observation = Atom.make(
    new ApiClient(transport, unexpectedProjectOverflow)
      .subscribeAttentionNotifications(async () => {
        throw new Error("Unexpected attention overflow");
      })
      .pipe(
        Stream.runForEach((value) =>
          Effect.sync(() => {
            if (value.kind === "event") events.push(value.event);
            else if (value.kind === "error") errors.push(value.error);
          }),
        ),
        Effect.as(null),
      ),
    { initialValue: null },
  );
  const view = renderHook(() => useAtomSuspense(observation), {
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) =>
      createElement(RegistryProvider, { children }),
  });
  await act(async () => undefined);
  return { transport, events, errors, view };
}

function pending(transport: FakeRpcTransport, value: pb.AttentionNotification) {
  transport.emitDescriptor(
    service.subscribe,
    service.event,
    create(pb.AttentionNotificationEventSchema, {
      sequence: 1n,
      type: pb.AttentionNotificationEventType.ATTENTION_NOTIFICATION_EVENT_PENDING,
      payload: { case: "pending", value },
    }),
  );
}

function taskTarget(focus: pb.AttentionNotificationTaskFocus) {
  return create(pb.AttentionNotificationTargetSchema, {
    kind: pb.AttentionNotificationTargetKind.ATTENTION_NOTIFICATION_TARGET_WORKFLOW_TASK,
    target: {
      case: "workflowTask",
      value: {
        projectId: "project-1",
        workflowId: "11111111-1111-4111-8111-111111111111",
        taskId: "task-1",
        taskShortId: "T-1",
        currentNodeId: "node-1",
        focus,
      },
    },
  });
}

describe("attention notification API", () => {
  it("releases the binary subscription when the mounted observation departs", async () => {
    const { transport, view } = await observe();
    expect(transport.descriptorSubscriptions).toEqual([service.subscribe]);
    view.unmount();
    await waitFor(() => expect(transport.descriptorSubscriptions).toEqual([]));
  });

  it("delivers Session Questions and resolution with Project and Session navigation identity", async () => {
    const { transport, events, errors } = await observe();
    pending(transport, question);
    transport.emitDescriptor(
      service.subscribe,
      service.event,
      create(pb.AttentionNotificationEventSchema, {
        sequence: 2n,
        type: pb.AttentionNotificationEventType.ATTENTION_NOTIFICATION_EVENT_RESOLVED,
        payload: { case: "resolved", value: { id: question.id, kind: question.kind, occurredAt } },
      }),
    );
    await waitFor(() => expect(events).toHaveLength(2));
    expect(errors).toEqual([]);
    expect(events).toMatchObject([
      {
        type: "pending",
        sequence: 1,
        pending: {
          revision: 1,
          question: { skippedAskIDs: [], currentUnresolvedAskIDs: ["ask-1"] },
          target: { kind: "session_prompt", projectID: "project-1", sessionID: "session-1" },
        },
      },
      { type: "resolved", sequence: 2, id: { kind: "question", uuid: "batch-1" } },
    ]);
    expect(() => {
      pending(
        transport,
        create(pb.AttentionNotificationSchema, {
          ...question,
          target: create(pb.AttentionNotificationTargetSchema, {
            kind: pb.AttentionNotificationTargetKind.ATTENTION_NOTIFICATION_TARGET_SESSION_PROMPT,
            target: { case: "sessionPrompt", value: { sessionId: "session-1" } },
          }),
        }),
      );
    }).toThrow();
  });

  it("delivers Task Question batches and reports malformed binary events", async () => {
    const { transport, events, errors } = await observe();
    pending(
      transport,
      create(pb.AttentionNotificationSchema, {
        ...question,
        target: taskTarget(
          create(pb.AttentionNotificationTaskFocusSchema, {
            kind: pb.AttentionNotificationFocusKind.ATTENTION_NOTIFICATION_FOCUS_QUESTION,
            focus: { case: "question", value: { askIds: ["ask-2", "ask-1"] } },
          }),
        ),
      }),
    );
    await waitFor(() => expect(events).toHaveLength(1));
    expect(events).toMatchObject([
      {
        type: "pending",
        pending: {
          question: { displayCount: 2, materializedCount: 1 },
          target: { kind: "workflow_task", focus: { kind: "question", askIDs: ["ask-2", "ask-1"] } },
        },
      },
    ]);
    transport.emitDescriptorBytes(service.subscribe, new Uint8Array([0xff]));
    await waitFor(() => expect(errors).toHaveLength(1));
    expect(events).toHaveLength(1);
  });

  it("preserves structured Approval access targets without inventing a message", async () => {
    const { transport, events } = await observe();
    pending(
      transport,
      create(pb.AttentionNotificationSchema, {
        id: { kind: pb.AttentionNotificationKind.APPROVAL, uuid: "approval-1" },
        kind: pb.AttentionNotificationKind.APPROVAL,
        occurredAt,
        revision: 1n,
        target: sessionTarget,
        state: {
          case: "approval",
          value: {
            accessTargets: [
              { requestedPath: " /alias/a ", resolvedPath: " /real/file " },
              { requestedPath: "/alias/b", resolvedPath: "/real/file" },
            ],
          },
        },
      }),
    );
    await waitFor(() => expect(events).toHaveLength(1));
    expect(events).toMatchObject([
      {
        type: "pending",
        pending: {
          approval: {
            message: undefined,
            accessTargets: [
              { requestedPath: " /alias/a ", resolvedPath: " /real/file " },
              { requestedPath: "/alias/b", resolvedPath: "/real/file" },
            ],
          },
          target: { kind: "session_prompt", projectID: "project-1", sessionID: "session-1" },
        },
      },
    ]);
  });

  it("keeps Workflow Approval and interrupted Current Node states distinct", async () => {
    const { transport, events } = await observe();
    pending(
      transport,
      create(pb.AttentionNotificationSchema, {
        id: { kind: pb.AttentionNotificationKind.WORKFLOW_APPROVAL, uuid: "approval-1" },
        kind: pb.AttentionNotificationKind.WORKFLOW_APPROVAL,
        occurredAt,
        revision: 1n,
        state: { case: "workflowApproval", value: { approvalId: "approval-1" } },
        target: taskTarget(
          create(pb.AttentionNotificationTaskFocusSchema, {
            kind: pb.AttentionNotificationFocusKind.ATTENTION_NOTIFICATION_FOCUS_APPROVAL,
            focus: { case: "approval", value: { approvalId: "approval-1" } },
          }),
        ),
      }),
    );
    pending(
      transport,
      create(pb.AttentionNotificationSchema, {
        id: { kind: pb.AttentionNotificationKind.INTERRUPTED_CURRENT_NODE, uuid: "node-1" },
        kind: pb.AttentionNotificationKind.INTERRUPTED_CURRENT_NODE,
        occurredAt,
        revision: 1n,
        state: { case: "interruptedCurrentNode", value: { reason: "workflow_runtime_failed" } },
        target: taskTarget(
          create(pb.AttentionNotificationTaskFocusSchema, {
            kind: pb.AttentionNotificationFocusKind.ATTENTION_NOTIFICATION_FOCUS_INTERRUPTED_CURRENT_NODE,
            focus: { case: "interruptedCurrentNode", value: {} },
          }),
        ),
      }),
    );
    await waitFor(() => expect(events).toHaveLength(2));
    expect(events).toMatchObject([
      {
        type: "pending",
        pending: {
          approval: null,
          workflowApproval: { approvalID: "approval-1" },
          target: { focus: { kind: "approval", approvalID: "approval-1" } },
        },
      },
      {
        type: "pending",
        pending: {
          question: null,
          interruptedCurrentNode: { reason: "workflow_runtime_failed" },
          target: { focus: { kind: "interrupted_current_node" } },
        },
      },
    ]);
  });

  it("rejects a Task Question focus that differs from the prepared batch", async () => {
    const { transport, events } = await observe();
    expect(() => {
      pending(
        transport,
        create(pb.AttentionNotificationSchema, {
          ...question,
          target: taskTarget(
            create(pb.AttentionNotificationTaskFocusSchema, {
              kind: pb.AttentionNotificationFocusKind.ATTENTION_NOTIFICATION_FOCUS_QUESTION,
              focus: { case: "question", value: { askIds: ["other-ask"] } },
            }),
          ),
        }),
      );
    }).toThrow();
    expect(events).toEqual([]);
  });
});
