import { expect, it, vi } from "vitest";

import type {
  ApiSubscription,
  ChatApi,
  ChatSessionTarget,
  ChatTranscriptHandler,
  ChatTranscriptMessage,
} from "@/api";
import { ContractError } from "@/api";
import { row } from "@/test-support/transcript-window";

import { ChatTranscriptPhysicalObservation } from "./chatTranscriptObservation";
import { ChatTranscriptObservation } from "./chatTranscriptObservation";

const target: ChatSessionTarget = {
  projectID: "project-1",
  workspace: { workspaceID: "workspace-1" },
  sessionID: "123e4567-e89b-42d3-a456-426614174000",
};

function unavailableActivity(): ChatTranscriptMessage {
  return {
    sequence: 2,
    kind: "runtime_read_model_update",
    payload: {
      Version: { Epoch: "epoch-1", Generation: 1, Sequence: 2 },
      Activity: {
        State: "unavailable",
        ActiveStep: null,
        Reviewer: "inactive",
        QueueAccepting: false,
        DiagnosticRecovery: false,
      },
    },
  };
}

it("reopens one physical subscription after terminal Runtime unavailable and normal completion", () => {
  const handlers: ChatTranscriptHandler[] = [];
  const closes: ReturnType<typeof vi.fn>[] = [];
  const api: Pick<ChatApi, "subscribeTranscript"> = {
    subscribeTranscript(_target, handler): ApiSubscription {
      handlers.push(handler);
      const close = vi.fn();
      closes.push(close);
      return { close };
    },
  };
  const events: ChatTranscriptMessage[] = [];
  const completions: unknown[] = [];
  const errors: Error[] = [];
  const observation = new ChatTranscriptPhysicalObservation(api, target, {
    onEvent: (event) => events.push(event),
    onComplete: (completion) => completions.push(completion),
    onError: (error) => errors.push(error),
  });

  observation.start();
  expect(handlers).toHaveLength(1);
  handlers[0]?.onEvent(unavailableActivity());
  handlers[0]?.onComplete({ code: 0, message: "", reason: null });

  expect(events).toHaveLength(1);
  expect(handlers).toHaveLength(2);
  expect(completions).toEqual([]);
  expect(errors).toEqual([]);

  handlers[0]?.onComplete({ code: 0, message: "", reason: null });
  expect(handlers).toHaveLength(2);

  observation.close();
  expect(closes[1]).toHaveBeenCalledOnce();
  handlers[1]?.onEvent(unavailableActivity());
  expect(events).toHaveLength(1);
});

it("starts one bounded recovery on a sequence gap and exposes Error after replacement failure", () => {
  const handlers: ChatTranscriptHandler[] = [];
  const api: Pick<ChatApi, "subscribeTranscript"> = {
    subscribeTranscript(_target, handler) {
      handlers.push(handler);
      return { close: vi.fn() };
    },
  };
  const hydrationKinds: string[] = [];
  const recoveryBegin = vi.fn();
  const forceMainView = vi.fn();
  const integrityFailures = vi.fn();
  const errors: Error[] = [];
  const observation = new ChatTranscriptObservation(api, target, {
    onHydration: (kind) => hydrationKinds.push(kind),
    onEvent: () => undefined,
    onRecoveryBegin: recoveryBegin,
    onForceMainViewRead: forceMainView,
    onIntegrityFailure: (error, recover) => {
      integrityFailures(error);
      recover();
    },
    onError: (error) => errors.push(error),
  });

  observation.start();
  handlers[0]?.onOpen?.();
  handlers[0]?.onEvent(hydrationMessage());
  handlers[0]?.onEvent({ ...unavailableActivity(), sequence: 3 });
  expect(handlers).toHaveLength(2);
  expect(recoveryBegin).toHaveBeenCalledOnce();
  expect(forceMainView).toHaveBeenCalledOnce();
  expect(integrityFailures).toHaveBeenCalledOnce();
  expect(observation.state.kind).toBe("recovering");
  handlers[0]?.onEvent(unavailableActivity());
  handlers[0]?.onComplete({ code: 0, message: "", reason: null });
  expect(handlers).toHaveLength(2);

  handlers[1]?.onError(new Error("replacement failed"));
  expect(observation.state.kind).toBe("error");
  expect(errors).toHaveLength(1);
  expect(integrityFailures).toHaveBeenCalledOnce();
  observation.retry();
  expect(handlers).toHaveLength(3);
  expect(recoveryBegin).toHaveBeenCalledTimes(2);
  expect(forceMainView).toHaveBeenCalledOnce();
  handlers[2]?.onOpen?.();
  handlers[2]?.onEvent(hydrationMessage());
  expect(hydrationKinds).toEqual(["initial", "scratch"]);
  expect(observation.state.kind).toBe("observing");
});

it("closes malformed-event observation before replacement and closes replacement on disposal", () => {
  const handlers: ChatTranscriptHandler[] = [];
  const closes: ReturnType<typeof vi.fn>[] = [];
  const api: Pick<ChatApi, "subscribeTranscript"> = {
    subscribeTranscript(_target, handler) {
      handlers.push(handler);
      const close = vi.fn();
      closes.push(close);
      return { close };
    },
  };
  const observation = new ChatTranscriptObservation(api, target, {
    onHydration: () => undefined,
    onEvent: () => undefined,
    onRecoveryBegin: () => undefined,
    onForceMainViewRead: () => undefined,
    onError: () => undefined,
  });

  observation.start();
  handlers[0]?.onError(new ContractError("Malformed transcript event."));

  expect(closes[0]).toHaveBeenCalledOnce();
  expect(handlers).toHaveLength(2);
  observation.close();
  expect(closes[1]).toHaveBeenCalledOnce();
});

it("replaces a waiting recovery and forces fresh authority on confirmed reconnect", () => {
  const handlers: ChatTranscriptHandler[] = [];
  const closes: ReturnType<typeof vi.fn>[] = [];
  const api: Pick<ChatApi, "subscribeTranscript"> = {
    subscribeTranscript(_target, handler) {
      handlers.push(handler);
      const close = vi.fn();
      closes.push(close);
      return { close };
    },
  };
  const recoveryBegin = vi.fn();
  const forceMainView = vi.fn();
  const observation = new ChatTranscriptObservation(api, target, {
    onHydration: () => undefined,
    onEvent: () => undefined,
    onTransportLoss: () => undefined,
    onRecoveryBegin: recoveryBegin,
    onForceMainViewRead: forceMainView,
    onError: () => undefined,
  });

  observation.start();
  handlers[0]?.onOpen?.();
  handlers[0]?.onEvent(hydrationMessage());
  handlers[0]?.onEvent({ ...unavailableActivity(), sequence: 3 });
  expect(observation.state.kind).toBe("recovering");

  observation.replaceForReconnect();

  expect(closes[1]).toHaveBeenCalledOnce();
  expect(handlers).toHaveLength(3);
  expect(recoveryBegin).toHaveBeenCalledTimes(2);
  expect(forceMainView).toHaveBeenCalledTimes(2);
  expect(observation.state.kind).toBe("recovering");
});

it("rejects a live committed event that does not advance beyond hydration", () => {
  const handlers: ChatTranscriptHandler[] = [];
  const recoveryBegin = vi.fn();
  const api: Pick<ChatApi, "subscribeTranscript"> = {
    subscribeTranscript(_target, handler) {
      handlers.push(handler);
      return { close: vi.fn() };
    },
  };
  const observation = new ChatTranscriptObservation(api, target, {
    onHydration: () => undefined,
    onEvent: () => undefined,
    onRecoveryBegin: recoveryBegin,
    onForceMainViewRead: () => undefined,
    onError: () => undefined,
  });
  const hydration = hydrationMessage();
  observation.start();
  handlers[0]?.onOpen?.();
  handlers[0]?.onEvent({
    ...hydration,
    payload: {
      ...hydration.payload,
      TailSegment: {
        Entries: [row(10)],
        HasMoreAbove: false,
        OlderCursor: null,
      },
    },
  });
  handlers[0]?.onEvent({
    sequence: 2,
    kind: "committed_row",
    payload: {
      ...row(10),
      Locator: { event_sequence: 10, row_ordinal: 2 },
    },
  });

  expect(recoveryBegin).toHaveBeenCalledOnce();
  expect(handlers).toHaveLength(2);
  expect(observation.state.kind).toBe("recovering");
});

it.each([
  {
    name: "first-row ordinal gap",
    locators: [{ eventSequence: 11, rowOrdinal: 2 }],
  },
  {
    name: "repeated locator",
    locators: [
      { eventSequence: 11, rowOrdinal: 1 },
      { eventSequence: 11, rowOrdinal: 1 },
    ],
  },
  {
    name: "same-event ordinal gap",
    locators: [
      { eventSequence: 11, rowOrdinal: 1 },
      { eventSequence: 11, rowOrdinal: 3 },
    ],
  },
  {
    name: "event regression",
    locators: [
      { eventSequence: 11, rowOrdinal: 1 },
      { eventSequence: 10, rowOrdinal: 1 },
    ],
  },
])("rejects $name in live committed-row progression", ({ locators }) => {
  const handlers: ChatTranscriptHandler[] = [];
  const recoveryBegin = vi.fn();
  const api: Pick<ChatApi, "subscribeTranscript"> = {
    subscribeTranscript(_target, handler) {
      handlers.push(handler);
      return { close: vi.fn() };
    },
  };
  const observation = new ChatTranscriptObservation(api, target, {
    onHydration: () => undefined,
    onEvent: () => undefined,
    onRecoveryBegin: recoveryBegin,
    onForceMainViewRead: () => undefined,
    onError: () => undefined,
  });
  const hydration = hydrationMessage();
  observation.start();
  handlers[0]?.onOpen?.();
  handlers[0]?.onEvent({
    ...hydration,
    payload: {
      ...hydration.payload,
      TailSegment: {
        Entries: [row(10)],
        HasMoreAbove: false,
        OlderCursor: null,
      },
    },
  });
  locators.forEach((locator, index) => {
    handlers[0]?.onEvent({
      sequence: index + 2,
      kind: "committed_row",
      payload: {
        ...row(locator.eventSequence),
        Locator: {
          event_sequence: locator.eventSequence,
          row_ordinal: locator.rowOrdinal,
        },
      },
    });
  });

  expect(recoveryBegin).toHaveBeenCalledOnce();
  expect(handlers).toHaveLength(2);
});

it("admits sequence-1 reattachment after socket retry or normal Runtime stop without recovery", () => {
  const handlers: ChatTranscriptHandler[] = [];
  const api: Pick<ChatApi, "subscribeTranscript"> = {
    subscribeTranscript(_target, handler) {
      handlers.push(handler);
      return { close: vi.fn() };
    },
  };
  const hydrationKinds: string[] = [];
  const recoveryBegin = vi.fn();
  const observation = new ChatTranscriptObservation(api, target, {
    onHydration: (kind) => hydrationKinds.push(kind),
    onEvent: () => undefined,
    onRecoveryBegin: recoveryBegin,
    onForceMainViewRead: () => undefined,
    onError: () => undefined,
  });

  observation.start();
  handlers[0]?.onOpen?.();
  handlers[0]?.onEvent(hydrationMessage());
  handlers[0]?.onOpen?.();
  handlers[0]?.onEvent(hydrationMessage());
  handlers[0]?.onEvent(unavailableActivity());
  handlers[0]?.onComplete({ code: 0, message: "", reason: null });
  handlers[1]?.onOpen?.();
  handlers[1]?.onEvent(hydrationMessage());

  expect(hydrationKinds).toEqual(["initial", "reattachment", "reattachment"]);
  expect(recoveryBegin).not.toHaveBeenCalled();
  expect(observation.state.kind).toBe("observing");
});

it("resets committed-row progression from replacement hydration", () => {
  const handlers: ChatTranscriptHandler[] = [];
  const admitted = vi.fn();
  const recoveryBegin = vi.fn();
  const api: Pick<ChatApi, "subscribeTranscript"> = {
    subscribeTranscript(_target, handler) {
      handlers.push(handler);
      return { close: vi.fn() };
    },
  };
  const observation = new ChatTranscriptObservation(api, target, {
    onHydration: () => undefined,
    onEvent: admitted,
    onRecoveryBegin: recoveryBegin,
    onForceMainViewRead: () => undefined,
    onError: () => undefined,
  });
  const initial = hydrationMessage();
  observation.start();
  handlers[0]?.onOpen?.();
  handlers[0]?.onEvent({
    ...initial,
    payload: {
      ...initial.payload,
      TailSegment: { Entries: [row(10)], HasMoreAbove: false, OlderCursor: null },
    },
  });
  handlers[0]?.onEvent({ sequence: 2, kind: "committed_row", payload: row(11) });
  handlers[0]?.onOpen?.();
  handlers[0]?.onEvent({
    ...initial,
    payload: {
      ...initial.payload,
      TailSegment: { Entries: [row(5)], HasMoreAbove: false, OlderCursor: null },
    },
  });
  handlers[0]?.onEvent({ sequence: 2, kind: "committed_row", payload: row(6) });

  expect(admitted).toHaveBeenCalledTimes(2);
  expect(recoveryBegin).not.toHaveBeenCalled();
  expect(observation.state.kind).toBe("observing");
});

it.each([
  {
    name: "repeated hydration",
    trigger: (handler: ChatTranscriptHandler) => {
      handler.onEvent({ ...hydrationMessage(), sequence: 2 });
    },
  },
  {
    name: "abnormal completion",
    trigger: (handler: ChatTranscriptHandler) => {
      handler.onComplete({ code: 0, message: "", reason: null });
    },
  },
  {
    name: "buffered stream failure",
    trigger: (handler: ChatTranscriptHandler) => {
      handler.onComplete({ code: 17, message: "overflow", reason: "subscriber_overflow" });
    },
  },
])("starts one replacement for $name", (testCase) => {
  const handlers: ChatTranscriptHandler[] = [];
  const api: Pick<ChatApi, "subscribeTranscript"> = {
    subscribeTranscript(_target, handler) {
      handlers.push(handler);
      return { close: vi.fn() };
    },
  };
  const recoveryBegin = vi.fn();
  const forceMainView = vi.fn();
  const observation = new ChatTranscriptObservation(api, target, {
    onHydration: () => undefined,
    onEvent: () => undefined,
    onRecoveryBegin: recoveryBegin,
    onForceMainViewRead: forceMainView,
    onError: () => undefined,
  });
  observation.start();
  handlers[0]?.onOpen?.();
  handlers[0]?.onEvent(hydrationMessage());
  const first = handlers[0];
  if (first === undefined) throw new Error("Transcript handler missing.");
  testCase.trigger(first);

  expect(handlers).toHaveLength(2);
  expect(recoveryBegin).toHaveBeenCalledOnce();
  expect(forceMainView).toHaveBeenCalledOnce();
  expect(observation.state.kind).toBe("recovering");
});

function hydrationMessage(): Extract<ChatTranscriptMessage, { kind: "hydration" }> {
  return {
    sequence: 1,
    kind: "hydration",
    payload: {
      SessionIdentity: {
        SessionID: target.sessionID,
        SessionName: null,
        ConversationFreshness: 0,
        ExecutionTarget: null,
      },
      SessionStatus: {
        ReviewerFrequency: "off",
        ReviewerEnabled: false,
        AutoCompactionEnabled: true,
        QuestionsEnabled: true,
        FastModeAvailable: false,
        FastModeEnabled: false,
        ThinkingLevel: "medium",
        CompactionMode: "local",
        CompactionCount: 0,
        PreviousSessionID: null,
        ParentAgentSessionID: null,
        NavigationTargetSessionID: null,
        Workflow: null,
      },
      RuntimeReadModelUpdate: {
        Version: { Epoch: "epoch-1", Generation: 1, Sequence: 1 },
        Activity: {
          State: "registered_idle",
          ActiveStep: null,
          Reviewer: "inactive",
          QueueAccepting: true,
          DiagnosticRecovery: false,
        },
      },
      TailSegment: { OlderCursor: null, HasMoreAbove: false, Entries: [] },
      ActiveAssistant: null,
      ActiveThinkingStatus: null,
      ActiveReasoningTraces: [],
      ActiveStep: null,
      ActiveCompaction: null,
      InFlightTools: [],
      PendingPrompts: [],
      BackgroundActivities: [],
      ContextUsage: null,
      GoalStatus: null,
    },
  };
}
