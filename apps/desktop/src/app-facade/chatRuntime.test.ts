import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";

import type { ChatGoalObservationHandler, ChatMainView, ChatMainViewRead, ChatTranscriptPage } from "@/api";
import {
  changedIdentity,
  deferred,
  goalStatus,
  hydration,
  hydrationWithCursor,
  incompatibleHydration,
  mainViewRead,
  requireValue,
  runtimeApi,
  runtimeHost,
  runtimeUnavailablePayload,
  runtimeUpdate,
  seededProjection,
  target,
  transcriptPage,
  worktreeOutcome,
} from "@/test-support/chat-runtime";
import { row } from "@/test-support/transcript-window";

import {
  ChatRuntimeOwner,
  chatMainViewQueryOptions,
  emptyChatProjectionState,
  reduceChatProjection,
} from "./chatRuntime";
import { ChatGoalDestinationController } from "./chatGoalDestination";
import { executeChatTranscriptPage } from "./chatTranscriptHost";
import { queryKeys } from "./queryKeys";

describe("Chat Main View admission", () => {
  it("splits Goal facts from the goal-free Main View cache and applies hydration replacement rules", () => {
    const read = mainViewRead();
    const authoritative = reduceChatProjection(emptyChatProjectionState(), {
      kind: "authoritative-read",
      read,
      metadataRevisionAtStart: 0,
      goalGenerationAtStart: 0,
      currentGoalGeneration: 0,
    });
    const hydrated = reduceChatProjection(authoritative.state, {
      kind: "hydration",
      hydration: hydration(),
    });

    expect(authoritative.state.view).toEqual(read.mainView);
    expect(authoritative.goalFact).toEqual({ goal: null, availability: "available" });
    expect(hydrated.state.view).toMatchObject({
      sessionName: null,
      executionTarget: { workspaceID: "workspace-1" },
      activity: { state: "running" },
      status: {
        reviewerFrequency: "edits",
        conversationFreshness: 1,
        contextUsage: {
          usedTokens: 0,
          windowTokens: 0,
          cacheHitPercent: 0,
          hasCacheHitPercentage: false,
        },
      },
    });
    expect(hydrated.goalFact).toEqual({ goal: null, availability: "agent_capability_missing" });
  });

  it("admits newer activity across generations and rejects stale activity", () => {
    let state = seededProjection(3);
    state = reduceChatProjection(state, {
      kind: "event",
      event: runtimeUpdate(4, 1, "closing"),
    }).state;
    expect(state.view?.activity.state).toBe("closing");

    const stale = reduceChatProjection(state, {
      kind: "event",
      event: runtimeUpdate(2, 1, "running"),
    });
    const forward = reduceChatProjection(state, {
      kind: "event",
      event: runtimeUpdate(1, 2, "registered_idle"),
    });
    expect(stale.state).toBe(state);
    expect(forward.state.view).toMatchObject({
      version: { epoch: "epoch-1", generation: 2, sequence: 1 },
      activity: { state: "registered_idle" },
    });
  });

  it("protects newer transcript metadata and Goal authority from a late read", () => {
    const seeded = seededProjection();
    const view = seeded.view;
    if (view === null) throw new Error("Seeded Main View is missing.");
    const updated = reduceChatProjection(seeded, {
      kind: "event",
      event: {
        sequence: 2,
        kind: "session_identity",
        payload: {
          SessionID: view.sessionID,
          SessionName: "newer",
          ConversationFreshness: 1,
          ExecutionTarget: null,
        },
      },
    }).state;
    const late = reduceChatProjection(updated, {
      kind: "authoritative-read",
      read: {
        ...mainViewRead(3),
        mainView: { ...mainViewRead(3).mainView, sessionName: "stale" },
      },
      metadataRevisionAtStart: 0,
      goalGenerationAtStart: 0,
      currentGoalGeneration: 1,
    });

    expect(late.state.view?.sessionName).toBe("newer");
    expect(late.state.view?.version.sequence).toBe(3);
    expect(late.goalFact).toBeNull();
  });

  it("applies transcript metadata that arrives before the first Main View read", () => {
    const observed = reduceChatProjection(emptyChatProjectionState(), {
      kind: "hydration",
      hydration: hydration(),
    }).state;
    const admitted = reduceChatProjection(observed, {
      kind: "authoritative-read",
      read: mainViewRead(),
      metadataRevisionAtStart: 0,
      goalGenerationAtStart: 0,
      currentGoalGeneration: 1,
    });

    expect(admitted.state.view).toMatchObject({
      sessionName: null,
      activity: { state: "running" },
      status: { reviewerFrequency: "edits", conversationFreshness: 1 },
    });
    expect(admitted.goalFact).toBeNull();
  });

  it("emits interruptions and Worktree outcomes without mutating Main View", () => {
    const state = seededProjection();
    const interrupted = reduceChatProjection(state, {
      kind: "event",
      event: {
        sequence: 2,
        kind: "human_input_interrupted",
        payload: { Items: [{ QueueItemID: "queue-1", Text: "hello" }] },
      },
    });
    const outcome = reduceChatProjection(state, {
      kind: "event",
      event: {
        sequence: 3,
        kind: "worktree_transition_outcome",
        payload: worktreeOutcome(),
      },
    });

    expect(interrupted.effects).toEqual([
      {
        kind: "human-input-interrupted",
        items: [{ QueueItemID: "queue-1", Text: "hello" }],
      },
    ]);
    expect(outcome.state).toBe(state);
    expect(outcome.effects).toHaveLength(1);
  });
});

describe("Chat Main View query and page execution", () => {
  it("overrides QueryClient retries and autonomous refetch", async () => {
    const fixture = runtimeApi({
      reads: [Promise.reject(new Error("unavailable"))],
    });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: 1, staleTime: 4_000 } },
    });
    const options = chatMainViewQueryOptions(fixture.api, target, (read) => read.mainView);

    await expect(queryClient.fetchQuery(options)).rejects.toThrow("unavailable");
    expect(fixture.getMainView).toHaveBeenCalledOnce();
    expect(options).toMatchObject({
      enabled: false,
      staleTime: 0,
      retry: false,
      refetchOnMount: false,
      refetchOnWindowFocus: false,
      refetchOnReconnect: false,
    });
  });
});

describe("mounted Chat Runtime owner", () => {
  it("keeps Chat and mounted Goal destination projections independent", async () => {
    const goalHandlers: ChatGoalObservationHandler[] = [];
    const destination = new ChatGoalDestinationController(
      {
        subscribeGoal(_target, handler) {
          goalHandlers.push(handler);
          return { close: vi.fn() };
        },
      },
      target,
    );
    const fixture = runtimeApi();
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), runtimeHost());
    owner.start();
    destination.start();
    await vi.waitFor(() => {
      expect(owner.snapshot.goal).toMatchObject({
        kind: "observed",
        value: { availability: "available" },
      });
      expect(fixture.handlers).toHaveLength(1);
    });
    goalHandlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      fact: { goal: null, availability: "agent_capability_missing" },
    });
    const handle = destination.begin({ kind: "clear" });
    destination.succeed(handle, {
      kind: "authoritative_clear",
      fact: { goal: null, availability: null },
    });

    expect(owner.snapshot.goal).toMatchObject({
      kind: "observed",
      value: { availability: "available" },
    });
    fixture.handlers[0]?.onOpen?.();
    fixture.handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
    expect(destination.snapshot).toMatchObject({
      authority: { kind: "observed", value: { availability: null } },
      presentation: { kind: "authority" },
    });

    destination.dispose();
    await owner.dispose();
  });

  it("retries only failed opening-page work while observation remains dormant", async () => {
    const opening = deferred<ChatTranscriptPage>();
    const fixture = runtimeApi({
      reads: [Promise.resolve(mainViewRead())],
      pages: [opening.promise, Promise.resolve(transcriptPage(null))],
    });
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), runtimeHost());
    owner.start();
    await vi.waitFor(() => {
      expect(fixture.getMainView).toHaveBeenCalledOnce();
      expect(fixture.handlers).toHaveLength(1);
    });
    opening.reject(new Error("Opening page unavailable"));
    await vi.waitFor(() => {
      expect(owner.snapshot.transcript.opening.kind).toBe("error");
    });

    owner.transcript.dispatch({ kind: "opening-retry" });

    await vi.waitFor(() => {
      expect(owner.snapshot.transcript.opening.kind).toBe("ready");
    });
    expect(fixture.getTranscriptPage).toHaveBeenCalledTimes(2);
    expect(fixture.getMainView).toHaveBeenCalledOnce();
    expect(fixture.handlers).toHaveLength(1);
    expect(owner.snapshot.observation.kind).toBe("loading");
    await owner.dispose();
  });

  it("starts independent branches and detaches an unresolved Main View before Retry", async () => {
    const first = deferred<ChatMainViewRead>();
    const second = deferred<ChatMainViewRead>();
    const fixture = runtimeApi({
      reads: [first.promise, second.promise],
      pages: [Promise.resolve(transcriptPage(null))],
    });
    const activateRuntime = vi.fn();
    const releaseRuntime = vi.fn();
    const api = { ...fixture.api, activateRuntime, releaseRuntime };
    const queryClient = new QueryClient();
    const owner = new ChatRuntimeOwner(api, target, queryClient, runtimeHost());

    owner.start();
    await vi.waitFor(() => {
      expect(fixture.getMainView).toHaveBeenCalledOnce();
    });
    expect(fixture.getTranscriptPage).toHaveBeenCalledOnce();
    expect(fixture.subscribeTranscript).toHaveBeenCalledOnce();
    await vi.waitFor(() => {
      expect(owner.snapshot.transcript.opening.kind).toBe("ready");
    });
    expect(activateRuntime).not.toHaveBeenCalled();
    expect(releaseRuntime).not.toHaveBeenCalled();

    await owner.retryMainView();
    expect(fixture.getMainView).toHaveBeenCalledTimes(2);
    second.resolve({
      ...mainViewRead(5),
      mainView: { ...mainViewRead(5).mainView, sessionName: "newer" },
    });
    await vi.waitFor(() => {
      expect(queryClient.getQueryData(queryKeys.chatMainView(target.sessionID))).toMatchObject({
        sessionName: "newer",
        version: { sequence: 5 },
      });
    });
    first.resolve({
      ...mainViewRead(2),
      mainView: { ...mainViewRead(2).mainView, sessionName: "stale" },
    });
    await Promise.resolve();
    expect(queryClient.getQueryData(queryKeys.chatMainView(target.sessionID))).toMatchObject({
      sessionName: "newer",
      version: { sequence: 5 },
    });
    await owner.dispose();
    expect(owner.snapshot.transcript.opening.kind).toBe("disposed");
  });

  it("keeps recovery branches independent and forwards ordered host facts", async () => {
    const replacementPage = deferred<ChatTranscriptPage>();
    const fixture = runtimeApi({
      reads: [
        Promise.resolve(mainViewRead()),
        Promise.resolve(mainViewRead(2)),
        Promise.resolve(mainViewRead(3)),
      ],
      pages: [Promise.resolve(transcriptPage(17)), replacementPage.promise],
    });
    const outcome = vi.fn();
    const interrupted = vi.fn();
    const host = runtimeHost({
      onWorktreeTransitionOutcome: outcome,
      onHumanInputInterrupted: interrupted,
    });
    const queryClient = new QueryClient();
    const owner = new ChatRuntimeOwner(fixture.api, target, queryClient, host);
    owner.start();
    await vi.waitFor(() => {
      expect(fixture.handlers).toHaveLength(1);
      expect(queryClient.getQueryData(queryKeys.chatMainView(target.sessionID))).toBeDefined();
    });
    const initial = requireValue(fixture.handlers[0]);
    initial.onOpen?.();
    initial.onEvent({ sequence: 1, kind: "hydration", payload: hydrationWithCursor(17) });
    initial.onEvent({
      sequence: 2,
      kind: "session_identity",
      payload: changedIdentity(),
    });
    initial.onEvent({
      sequence: 3,
      kind: "worktree_transition_outcome",
      payload: worktreeOutcome(),
    });
    initial.onEvent({ sequence: 4, kind: "goal_status", payload: goalStatus() });
    initial.onEvent({
      sequence: 5,
      kind: "human_input_interrupted",
      payload: { Items: [{ QueueItemID: "queue-1", Text: "restore me" }] },
    });
    expect(queryClient.getQueryData<ChatMainView>(queryKeys.chatMainView(target.sessionID))).toMatchObject({
      sessionName: null,
      executionTarget: { workspaceID: "workspace-2", workspaceAvailability: "missing" },
    });
    expect(outcome).toHaveBeenCalledOnce();
    expect(interrupted).toHaveBeenCalledOnce();
    expect(owner.snapshot.goal).toMatchObject({
      kind: "observed",
      value: { goal: { id: "goal-2" }, availability: null },
    });

    initial.onEvent({
      sequence: 7,
      kind: "session_status",
      payload: hydration().SessionStatus,
    });
    await vi.waitFor(() => {
      expect(fixture.handlers).toHaveLength(2);
      expect(fixture.getMainView).toHaveBeenCalledTimes(2);
    });
    expect(owner.snapshot.observation.kind).toBe("recovering");
    expect(fixture.getTranscriptPage).toHaveBeenCalledOnce();

    const explicitPage = executeChatTranscriptPage(fixture.api, target, {
      kind: "older",
      cursor: 17,
    });
    expect(fixture.getTranscriptPage).toHaveBeenCalledTimes(2);
    const replacement = requireValue(fixture.handlers[1]);
    replacement.onError(new Error("replacement failed"));
    expect(owner.snapshot.observation.kind).toBe("error");
    owner.retryTranscriptObservation();
    expect(fixture.handlers).toHaveLength(3);
    expect(fixture.getMainView).toHaveBeenCalledTimes(2);
    const retry = requireValue(fixture.handlers[2]);
    retry.onOpen?.();
    retry.onEvent({ sequence: 1, kind: "hydration", payload: hydrationWithCursor(29) });
    expect(owner.snapshot.transcript.older).toEqual({ kind: "idle", cursor: 29 });
    replacementPage.resolve(transcriptPage(null));
    await expect(explicitPage).resolves.toEqual(transcriptPage(null));
    await owner.dispose();
  });

  it("ignores delayed failures from an invalidated observation after reconnect", async () => {
    const logging = deferred<undefined>();
    const fixture = runtimeApi({
      reads: [
        Promise.resolve(mainViewRead()),
        Promise.resolve(mainViewRead(2)),
        Promise.resolve(mainViewRead(3)),
      ],
      pages: [Promise.resolve(transcriptPage(null))],
    });
    const logger = { append: vi.fn(async () => logging.promise) };
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), { logger });
    owner.start();
    await vi.waitFor(() => {
      expect(fixture.handlers).toHaveLength(1);
      expect(fixture.getMainView).toHaveBeenCalledOnce();
    });
    const initial = requireValue(fixture.handlers[0]);
    initial.onOpen?.();
    initial.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
    initial.onEvent({
      sequence: 3,
      kind: "session_status",
      payload: hydration().SessionStatus,
    });
    initial.onEvent({
      sequence: 4,
      kind: "session_status",
      payload: hydration().SessionStatus,
    });

    expect(owner.snapshot.observation.kind).toBe("recovering");
    expect(logger.append).toHaveBeenCalledOnce();
    expect(fixture.handlers).toHaveLength(2);
    await vi.waitFor(() => {
      expect(fixture.getMainView).toHaveBeenCalledTimes(2);
    });

    owner.controlReconnected();
    await vi.waitFor(() => {
      expect(fixture.handlers).toHaveLength(3);
      expect(fixture.getMainView).toHaveBeenCalledTimes(3);
    });
    const replacement = requireValue(fixture.handlers[2]);
    replacement.onOpen?.();
    replacement.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
    expect(owner.snapshot.observation.kind).toBe("observing");

    logging.resolve(undefined);
    await Promise.resolve();
    await Promise.resolve();

    expect(owner.snapshot.observation.kind).toBe("observing");
    expect(fixture.handlers).toHaveLength(3);
    await owner.dispose();
  });

  it("does not start production recovery for autonomous resident failure in debug mode", async () => {
    vi.stubEnv("KENT_DEBUG", "true");
    const logging = deferred<undefined>();
    const fixture = runtimeApi({
      reads: [Promise.resolve(mainViewRead()), Promise.resolve(mainViewRead(2))],
      pages: [
        Promise.resolve({
          ...transcriptPage(null),
          hasMoreAbove: true,
        }),
      ],
    });
    const logger = { append: vi.fn(async () => logging.promise) };
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), { logger });
    try {
      owner.start();
      await vi.waitFor(() => {
        expect(logger.append).toHaveBeenCalledOnce();
      });

      expect(owner.snapshot.observation.kind).toBe("recovering");
      expect(fixture.handlers).toHaveLength(1);
      expect(fixture.getMainView).toHaveBeenCalledOnce();
    } finally {
      await owner.dispose();
      vi.unstubAllEnvs();
    }
  });

  it("keeps rejected hydration outside projection state and the replacement success budget", async () => {
    const fixture = runtimeApi({
      reads: [Promise.resolve(mainViewRead()), Promise.resolve(mainViewRead(2))],
      pages: [
        Promise.resolve({
          ...transcriptPage(null),
          entries: [row(10)],
        }),
      ],
    });
    const logger = { append: vi.fn().mockResolvedValue(undefined) };
    const queryClient = new QueryClient();
    const owner = new ChatRuntimeOwner(fixture.api, target, queryClient, { logger });
    owner.start();
    await vi.waitFor(() => {
      expect(owner.snapshot.transcript.opening.kind).toBe("ready");
      expect(fixture.handlers).toHaveLength(1);
      expect(queryClient.getQueryData(queryKeys.chatMainView(target.sessionID))).toBeDefined();
    });

    const initial = requireValue(fixture.handlers[0]);
    initial.onOpen?.();
    initial.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: incompatibleHydration("rejected-initial", "incompatible initial"),
    });
    await vi.waitFor(() => {
      expect(fixture.handlers).toHaveLength(2);
    });

    const replacement = requireValue(fixture.handlers[1]);
    replacement.onOpen?.();
    replacement.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: incompatibleHydration("rejected-replacement", "incompatible replacement"),
    });

    await vi.waitFor(() => {
      expect(owner.snapshot.observation.kind).toBe("error");
    });
    expect(fixture.handlers).toHaveLength(2);
    expect(
      queryClient.getQueryData<ChatMainView>(queryKeys.chatMainView(target.sessionID))?.sessionName,
    ).toBe("Session");
    expect(owner.snapshot.goal).toMatchObject({
      kind: "observed",
      value: { availability: "available" },
    });
    expect(logger.append).toHaveBeenCalledTimes(2);
    await owner.dispose();
  });

  it("silently discards provisional transcript state on socket loss without invalidating page work", async () => {
    const olderPage = deferred<ChatTranscriptPage>();
    const fixture = runtimeApi({
      reads: [Promise.resolve(mainViewRead())],
      pages: [
        Promise.resolve({
          ...transcriptPage(17),
          entries: [row(10)],
        }),
        olderPage.promise,
      ],
    });
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), runtimeHost());
    owner.start();
    await vi.waitFor(() => {
      expect(owner.snapshot.transcript.opening.kind).toBe("ready");
      expect(fixture.handlers).toHaveLength(1);
    });
    const initial = requireValue(fixture.handlers[0]);
    const hydrated = hydration();
    initial.onOpen?.();
    initial.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: {
        ...hydrated,
        TailSegment: {
          Entries: [row(10)],
          HasMoreAbove: true,
          OlderCursor: 17,
        },
      },
    });
    initial.onEvent({
      sequence: 2,
      kind: "assistant_delta",
      payload: {
        StepID: "step",
        StreamID: "stream",
        Phase: "commentary",
        Delta: "Draft",
      },
    });
    owner.transcript.dispatch({
      kind: "edge-visit",
      direction: "older",
      older: true,
      newer: false,
    });
    expect(owner.snapshot.transcript.items.some((item) => !("row" in item))).toBe(true);
    expect(owner.snapshot.transcript.older).toEqual({ kind: "loading", cursor: 17 });

    initial.onTransportLoss?.();

    expect(owner.snapshot.transcript.items.every((item) => "row" in item)).toBe(true);
    expect(owner.snapshot.transcript.older).toEqual({ kind: "loading", cursor: 17 });
    expect(owner.snapshot.observation.kind).toBe("observing");
    expect(fixture.handlers).toHaveLength(1);
    expect(fixture.getMainView).toHaveBeenCalledOnce();

    olderPage.resolve({
      ...transcriptPage(null),
      entries: [row(5)],
      newerCursor: 17,
      hasMoreBelow: true,
    });
    await vi.waitFor(() => {
      expect(owner.snapshot.transcript.older).toEqual({ kind: "idle", cursor: null });
    });
    await owner.dispose();
  });

  it("keeps a directional page current across normal Runtime stop", async () => {
    const olderPage = deferred<ChatTranscriptPage>();
    const fixture = runtimeApi({
      reads: [Promise.resolve(mainViewRead())],
      pages: [Promise.resolve(transcriptPage(17)), olderPage.promise],
    });
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), runtimeHost());
    owner.start();
    await vi.waitFor(() => {
      expect(fixture.handlers).toHaveLength(1);
      expect(fixture.getMainView).toHaveBeenCalledOnce();
    });
    const initial = requireValue(fixture.handlers[0]);
    initial.onOpen?.();
    initial.onEvent({ sequence: 1, kind: "hydration", payload: hydrationWithCursor(17) });
    const pendingPage = executeChatTranscriptPage(fixture.api, target, {
      kind: "older",
      cursor: 17,
    });
    initial.onEvent({
      sequence: 2,
      kind: "runtime_read_model_update",
      payload: runtimeUnavailablePayload(),
    });
    initial.onComplete({ code: 0, message: "", reason: null });

    expect(fixture.handlers).toHaveLength(2);
    expect(owner.snapshot.observation.kind).toBe("observing");
    expect(fixture.getMainView).toHaveBeenCalledOnce();
    olderPage.resolve({
      ...transcriptPage(null),
      newerCursor: 17,
      hasMoreBelow: true,
    });
    await expect(pendingPage).resolves.toMatchObject({ newerCursor: 17 });
    const reattached = requireValue(fixture.handlers[1]);
    reattached.onOpen?.();
    reattached.onEvent({ sequence: 1, kind: "hydration", payload: hydrationWithCursor(17) });
    expect(owner.snapshot.transcript.older).toEqual({ kind: "idle", cursor: 17 });
    expect(fixture.getTranscriptPage).toHaveBeenCalledTimes(2);
    await owner.dispose();
  });

  it("isolates identical Sessions across QueryClients", async () => {
    const fixture = runtimeApi({
      reads: [
        Promise.resolve({
          mainView: { ...mainViewRead().mainView, sessionName: "window-a" },
          goal: { goal: null, availability: "available" },
        }),
        Promise.resolve({
          mainView: { ...mainViewRead().mainView, sessionName: "window-b" },
          goal: { goal: null, availability: "available" },
        }),
      ],
      pages: [Promise.resolve(transcriptPage(null)), Promise.resolve(transcriptPage(null))],
    });
    const firstClient = new QueryClient();
    const secondClient = new QueryClient();
    const first = new ChatRuntimeOwner(fixture.api, target, firstClient, runtimeHost());
    const second = new ChatRuntimeOwner(fixture.api, target, secondClient, runtimeHost());
    first.start();
    second.start();
    await vi.waitFor(() => {
      expect(
        firstClient.getQueryData<ChatMainView>(queryKeys.chatMainView(target.sessionID))?.sessionName,
      ).toBe("window-a");
      expect(
        secondClient.getQueryData<ChatMainView>(queryKeys.chatMainView(target.sessionID))?.sessionName,
      ).toBe("window-b");
    });
    await first.dispose();
    await second.dispose();
  });
});
