import { describe, expect, it } from "vitest";

import type { ApiSubscription, ChatApi, ChatGoalObservationHandler, ChatSessionTarget } from "@/api";

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
  it("keeps newer observed authority when an older mutation result settles", () => {
    const { api, handlers } = observationApi();
    const controller = new ChatGoalDestinationController(api, target);
    controller.start();
    handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "available" },
    });
    const handle = controller.begin(intent);
    const newerFact = {
      goal: {
        id: "goal-new",
        objective: "new authority",
        status: "active" as const,
        createdAt: "2026-09-04T10:00:00Z",
        updatedAt: "2026-09-04T10:00:00Z",
      },
      availability: null,
    };
    handlers[0]?.onEvent({ sequence: 2, kind: "update", fact: newerFact });

    expect(
      controller.succeed(handle, {
        kind: "authoritative_clear",
        fact: { goal: null, availability: "available" },
      }),
    ).toBe(true);
    expect(controller.snapshot.authority).toEqual({ kind: "observed", value: newerFact });
    expect(controller.snapshot.presentation).toEqual({ kind: "authority" });
  });

  it("permanently disposes stale callbacks while a reopened destination is independent", () => {
    const { api, handlers } = observationApi();
    const disposed = new ChatGoalDestinationController(api, target);
    disposed.start();
    handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "available" },
    });
    const staleHandle = disposed.begin(intent);
    disposed.dispose();

    handlers[0]?.onEvent({
      sequence: 2,
      kind: "update",
      fact: { goal: null, availability: "agent_capability_missing" },
    });
    expect(disposed.snapshot.observation).toEqual({ kind: "disposed" });
    expect(
      disposed.succeed(staleHandle, {
        kind: "authoritative_goal",
        fact: {
          goal: {
            id: "stale",
            objective: "stale",
            status: "active",
            createdAt: "2026-09-04T10:00:00Z",
            updatedAt: "2026-09-04T10:00:00Z",
          },
          availability: "available",
        },
      }),
    ).toBe(false);

    const reopened = new ChatGoalDestinationController(api, target);
    reopened.start();
    handlers[1]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "available" },
    });
    const reopenedHandle = reopened.begin({ kind: "clear" });
    expect(
      reopened.succeed(reopenedHandle, {
        kind: "authoritative_clear",
        fact: { goal: null, availability: null },
      }),
    ).toBe(true);
    expect(reopened.snapshot.authority).toEqual({
      kind: "observed",
      value: { goal: null, availability: null },
    });
  });
});
