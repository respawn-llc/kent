import { describe, expect, it } from "vitest";

import type {
  ApiSubscription,
  ChatApi,
  ChatGoalMutationResult,
  ChatGoalObservationHandler,
  ChatSessionTarget,
} from "@/api";

import { ChatGoalDestinationController, type ChatGoalMutationIntent } from "./chatGoalDestination";

const target: ChatSessionTarget = {
  projectID: "project-1",
  workspace: { workspaceID: "workspace-1" },
  sessionID: "123e4567-e89b-42d3-a456-426614174000",
};
const intent: ChatGoalMutationIntent = {
  kind: "goal",
  preview: { objective: "ship", status: "paused" },
};

function observationApi() {
  const handlers: ChatGoalObservationHandler[] = [];
  const api: Pick<ChatApi, "subscribeGoal"> = {
    subscribeGoal(_target, handler): ApiSubscription {
      handlers.push(handler);
      return { close: vi.fn() };
    },
  };
  return { api, handlers };
}

describe("Chat Goal destination controller", () => {
  it("notifies destination subscribers once for each accepted observation", () => {
    const { api, handlers } = observationApi();
    const controller = new ChatGoalDestinationController(api, target);
    const listener = vi.fn();
    controller.subscribe(listener);
    controller.start();
    listener.mockClear();

    handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "available" },
    });

    expect(listener).toHaveBeenCalledOnce();
  });

  it("admits a validated authoritative result while hydration is pending", () => {
    const { api, handlers } = observationApi();
    const controller = new ChatGoalDestinationController(api, target);
    controller.start();
    const fact = {
      goal: {
        id: "goal-1",
        objective: "ship",
        status: "active",
        createdAt: "2026-09-12T10:00:00Z",
        updatedAt: "2026-09-12T10:00:00Z",
      },
      availability: "available",
    } satisfies Extract<ChatGoalMutationResult, { kind: "authoritative_goal" }>["fact"];
    const result: ChatGoalMutationResult = { kind: "authoritative_goal", fact };

    expect(controller.admitAuthoritativeResult(result)).toBe(true);
    expect(controller.snapshot).toMatchObject({
      authority: { kind: "observed", value: fact },
      observation: { kind: "loading" },
    });

    handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "available" },
    });
    expect(controller.snapshot.authority).toMatchObject({
      kind: "observed",
      value: { goal: null, availability: "available" },
    });
    expect(controller.admitAuthoritativeResult(result)).toBe(false);
  });

  it("keeps newer observed authority when an older authoritative mutation result settles", () => {
    const { api, handlers } = observationApi();
    const controller = new ChatGoalDestinationController(api, target);
    controller.start();
    handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "available" },
    });
    const handle = controller.begin(intent);
    handlers[0]?.onEvent({
      sequence: 2,
      kind: "update",
      fact: {
        goal: {
          id: "goal-new",
          objective: "new authority",
          status: "active",
          createdAt: "2026-09-04T10:00:00Z",
          updatedAt: "2026-09-04T10:00:00Z",
        },
        availability: null,
      },
    });

    expect(
      controller.succeed(handle, {
        kind: "authoritative_clear",
        fact: { goal: null, availability: "available" },
      }),
    ).toBe(true);
    expect(controller.snapshot.authority).toMatchObject({
      kind: "observed",
      value: { goal: { id: "goal-new" }, availability: null },
    });
    expect(controller.snapshot.presentation.kind).toBe("authority");
  });

  it("applies an authoritative result and releases mutation single-flight", () => {
    const { api, handlers } = observationApi();
    const controller = new ChatGoalDestinationController(api, target);
    controller.start();
    handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "available" },
    });
    const first = controller.begin(intent);
    controller.succeed(first, {
      kind: "authoritative_goal",
      fact: {
        goal: {
          id: "goal-1",
          objective: "ship",
          status: "paused",
          createdAt: "2026-09-04T10:00:00Z",
          updatedAt: "2026-09-04T10:00:00Z",
        },
        availability: null,
      },
    });
    expect(controller.snapshot.authority).toMatchObject({
      kind: "observed",
      value: { goal: { id: "goal-1", status: "paused" }, availability: null },
    });

    const secondIntent: ChatGoalMutationIntent = {
      kind: "goal",
      preview: { objective: "ship again", status: "active" },
    };
    const second = controller.begin(secondIntent);

    expect(controller.snapshot.presentation).toEqual({
      kind: "unresolved",
      intent: secondIntent,
    });
    expect(controller.fail(second)).toBe(true);
    expect(controller.snapshot.presentation.kind).toBe("authority");
  });

  it("retains destination state across transport replacement and rejects stale callbacks", () => {
    const { api, handlers } = observationApi();
    const controller = new ChatGoalDestinationController(api, target);
    controller.start();
    handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "available" },
    });
    const handle = controller.begin(intent);
    controller.replaceObservation();
    expect(handlers).toHaveLength(2);
    expect(controller.snapshot.presentation.kind).toBe("unresolved");

    handlers[0]?.onEvent({
      sequence: 2,
      kind: "update",
      fact: { goal: null, availability: "agent_capability_missing" },
    });
    handlers[1]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "available" },
    });
    expect(controller.snapshot.authority).toMatchObject({
      kind: "observed",
      value: { availability: "available" },
    });
    expect(controller.fail(handle)).toBe(true);

    controller.dispose();
    handlers[1]?.onEvent({
      sequence: 2,
      kind: "update",
      fact: { goal: null, availability: null },
    });
    expect(controller.snapshot.observation.kind).toBe("disposed");
  });

  it("replaces a failed observed transport once while retaining authority and mutation", () => {
    const { api, handlers } = observationApi();
    const controller = new ChatGoalDestinationController(api, target);
    controller.start();
    handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "available" },
    });
    controller.begin(intent);
    handlers[0]?.onError(new Error("socket lost"));

    expect(handlers).toHaveLength(2);
    expect(controller.snapshot.authority).toMatchObject({
      kind: "observed",
      value: { availability: "available" },
    });
    expect(controller.snapshot.presentation.kind).toBe("unresolved");
    handlers[1]?.onError(new Error("replacement failed"));
    expect(controller.snapshot.observation.kind).toBe("error");
    expect(handlers).toHaveLength(2);
  });

  it("exposes initial Goal observation failure and retries only on request", () => {
    const { api, handlers } = observationApi();
    const controller = new ChatGoalDestinationController(api, target);
    controller.start();

    handlers[0]?.onError(new Error("Goal unavailable"));

    expect(controller.snapshot.observation.kind).toBe("error");
    expect(handlers).toHaveLength(1);
    controller.replaceObservation();
    expect(controller.snapshot.observation.kind).toBe("loading");
    expect(handlers).toHaveLength(2);
  });

  it("disposes old mutation callbacks while a reopened destination begins independently", () => {
    const { api, handlers } = observationApi();
    const oldController = new ChatGoalDestinationController(api, target);
    oldController.start();
    handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "available" },
    });
    const oldHandle = oldController.begin(intent);
    oldController.dispose();

    const reopened = new ChatGoalDestinationController(api, target);
    reopened.start();
    handlers[1]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "agent_capability_missing" },
    });
    const newHandle = reopened.begin({ kind: "clear" });
    expect(
      oldController.succeed(oldHandle, {
        kind: "authoritative_goal",
        fact: {
          goal: {
            id: "goal-old",
            objective: "old",
            status: "active",
            createdAt: "2026-09-04T10:00:00Z",
            updatedAt: "2026-09-04T10:00:00Z",
          },
          availability: "available",
        },
      }),
    ).toBe(false);
    expect(
      reopened.succeed(newHandle, {
        kind: "authoritative_clear",
        fact: { goal: null, availability: null },
      }),
    ).toBe(true);
    expect(reopened.snapshot.presentation.kind).toBe("authority");
    expect(reopened.snapshot.authority).toMatchObject({
      kind: "observed",
      value: { goal: null, availability: null },
    });
    expect(
      reopened.succeed(newHandle, { kind: "authoritative_clear", fact: { goal: null, availability: null } }),
    ).toBe(false);
  });
});
