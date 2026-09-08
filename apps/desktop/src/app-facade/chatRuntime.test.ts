import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";

import type {
  ChatMainView,
  ChatMainViewRead,
  ChatSessionTarget,
  ChatTranscriptHandler,
  ChatTranscriptPage,
  ChatTranscriptPayloadByKind,
} from "@/api";
import { row } from "@/test-support/transcript-window";

import {
  ChatRuntimeOwner,
  chatMainViewQueryOptions,
  emptyChatProjectionState,
  reduceChatProjection,
  type ChatProjectionState,
  type ChatRuntimeApi,
  type ChatRuntimeHost,
} from "./chatRuntime";
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

  it("admits same-generation activity, rejects stale activity, and demands forward authority", () => {
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
    expect(forward.state).toBe(state);
    expect(forward.requiredAuthority).toEqual({
      epoch: "epoch-1",
      generation: 2,
      sequence: 1,
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

  it("executes newest and directional pages exactly once with opaque cursors", async () => {
    const fixture = runtimeApi({
      pages: [
        Promise.resolve(transcriptPage(null)),
        Promise.resolve(transcriptPage(null)),
        Promise.resolve(transcriptPage(null)),
      ],
    });
    await executeChatTranscriptPage(fixture.api, target, { kind: "newest" });
    await executeChatTranscriptPage(fixture.api, target, { kind: "older", cursor: 17 });
    await executeChatTranscriptPage(fixture.api, target, { kind: "newer", cursor: 29 });

    expect(fixture.getTranscriptPage.mock.calls).toEqual([
      [target],
      [target, { direction: "older", value: 17 }],
      [target, { direction: "newer", value: 29 }],
    ]);
  });
});

describe("mounted Chat Runtime owner", () => {
  it("owns the resident transcript window and executes its directional page effects", async () => {
    const fixture = runtimeApi({
      reads: [Promise.resolve(mainViewRead())],
      pages: [Promise.resolve(transcriptPage(17)), Promise.resolve(transcriptPage(null))],
    });
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), runtimeHost());

    owner.start();
    await vi.waitFor(() => {
      expect(owner.snapshot.transcript.opening.kind).toBe("ready");
    });
    owner.transcript.dispatch({
      kind: "edge-visit",
      direction: "older",
      older: true,
      newer: false,
    });
    await vi.waitFor(() => {
      expect(fixture.getTranscriptPage).toHaveBeenCalledTimes(2);
    });

    expect(fixture.getTranscriptPage.mock.calls).toEqual([
      [target],
      [target, { direction: "older", value: 17 }],
    ]);
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

  it("serializes tuple demand with one stale-success follow-up", async () => {
    const first = deferred<ChatMainViewRead>();
    const second = deferred<ChatMainViewRead>();
    const fixture = runtimeApi({ reads: [first.promise, second.promise] });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: 1, staleTime: 4_000 } },
    });
    const owner = new ChatRuntimeOwner(fixture.api, target, queryClient, runtimeHost());
    owner.start();
    await vi.waitFor(() => {
      expect(fixture.getMainView).toHaveBeenCalledOnce();
    });

    owner.requireMainViewAuthority({ epoch: "epoch-1", generation: 2, sequence: 2 });
    owner.requireMainViewAuthority({ epoch: "epoch-1", generation: 2, sequence: 7 });
    expect(fixture.getMainView).toHaveBeenCalledOnce();
    first.resolve(mainViewRead(1));
    await vi.waitFor(() => {
      expect(fixture.getMainView).toHaveBeenCalledTimes(2);
    });
    second.resolve({
      ...mainViewRead(7),
      mainView: {
        ...mainViewRead(7).mainView,
        version: { epoch: "epoch-1", generation: 2, sequence: 7 },
      },
    });
    await vi.waitFor(() => {
      expect(queryClient.getQueryData(queryKeys.chatMainView(target.sessionID))).toMatchObject({
        version: { generation: 2, sequence: 7 },
      });
    });
    expect(fixture.getMainView).toHaveBeenCalledTimes(2);
    await owner.dispose();
  });

  it("consumes failed demand and starts one coalesced later refresh", async () => {
    const reads = [
      deferred<ChatMainViewRead>(),
      deferred<ChatMainViewRead>(),
      deferred<ChatMainViewRead>(),
      deferred<ChatMainViewRead>(),
    ];
    const fixture = runtimeApi({ reads: reads.map(async (read) => read.promise) });
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: 1, staleTime: 4_000 } },
    });
    const owner = new ChatRuntimeOwner(fixture.api, target, queryClient, runtimeHost());
    owner.start();
    await vi.waitFor(() => {
      expect(fixture.getMainView).toHaveBeenCalledOnce();
    });
    reads[0]?.reject(new Error("initial failure"));
    await vi.waitFor(() => {
      expect(queryClient.getQueryState(queryKeys.chatMainView(target.sessionID))?.status).toBe("error");
    });
    expect(fixture.getMainView).toHaveBeenCalledOnce();

    owner.requireMainViewAuthority({ epoch: "epoch-1", generation: 2, sequence: 1 });
    await vi.waitFor(() => {
      expect(fixture.getMainView).toHaveBeenCalledTimes(2);
    });
    owner.requireMainViewAuthority({ epoch: "epoch-1", generation: 3, sequence: 1 });
    owner.requireMainViewAuthority({ epoch: "epoch-1", generation: 4, sequence: 1 });
    reads[1]?.reject(new Error("coalescing failure"));
    await vi.waitFor(() => {
      expect(fixture.getMainView).toHaveBeenCalledTimes(3);
    });
    reads[2]?.reject(new Error("follow-up failure"));
    await vi.waitFor(() => {
      expect(queryClient.getQueryState(queryKeys.chatMainView(target.sessionID))?.status).toBe("error");
    });
    expect(fixture.getMainView).toHaveBeenCalledTimes(3);

    owner.requireMainViewAuthority({ epoch: "epoch-1", generation: 5, sequence: 1 });
    await vi.waitFor(() => {
      expect(fixture.getMainView).toHaveBeenCalledTimes(4);
    });
    reads[3]?.resolve({
      ...mainViewRead(1),
      mainView: {
        ...mainViewRead(1).mainView,
        version: { epoch: "epoch-1", generation: 5, sequence: 1 },
      },
    });
    await vi.waitFor(() => {
      expect(queryClient.getQueryData(queryKeys.chatMainView(target.sessionID))).toMatchObject({
        version: { generation: 5 },
      });
    });
    await owner.dispose();
  });

  it("keeps recovery branches independent and forwards ordered host facts", async () => {
    const replacementPage = deferred<ChatTranscriptPage>();
    const fixture = runtimeApi({
      reads: [Promise.resolve(mainViewRead()), Promise.resolve(mainViewRead(2))],
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

  it("routes resident transcript invariant failures through diagnostics and recovery", async () => {
    const fixture = runtimeApi({
      reads: [Promise.resolve(mainViewRead()), Promise.resolve(mainViewRead(2))],
      pages: [Promise.resolve(transcriptPage(null))],
    });
    const logger = { append: vi.fn().mockResolvedValue(undefined) };
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), { logger });
    owner.start();
    await vi.waitFor(() => {
      expect(fixture.handlers).toHaveLength(1);
    });
    const initial = requireValue(fixture.handlers[0]);
    initial.onOpen?.();
    initial.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
    initial.onEvent({
      sequence: 2,
      kind: "assistant_delta",
      payload: {
        StepID: "step-1",
        StreamID: "stream",
        Phase: "commentary",
        Delta: "first",
      },
    });
    initial.onEvent({
      sequence: 3,
      kind: "assistant_delta",
      payload: {
        StepID: "step-2",
        StreamID: "stream",
        Phase: "commentary",
        Delta: "second",
      },
    });

    await vi.waitFor(() => {
      expect(logger.append).toHaveBeenCalledOnce();
      expect(fixture.handlers).toHaveLength(2);
    });
    expect(owner.snapshot.observation.kind).toBe("recovering");
    await owner.dispose();
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

  it("executes resident-window Scratch Rehydration without corrupting replacement sequencing", async () => {
    const fixture = runtimeApi({
      reads: [Promise.resolve(mainViewRead()), Promise.resolve(mainViewRead(2))],
      pages: [Promise.resolve(transcriptPage(null))],
    });
    const logger = { append: vi.fn().mockResolvedValue(undefined) };
    const owner = new ChatRuntimeOwner(fixture.api, target, new QueryClient(), { logger });
    owner.start();
    await vi.waitFor(() => {
      expect(fixture.handlers).toHaveLength(1);
    });
    const initial = requireValue(fixture.handlers[0]);
    initial.onOpen?.();
    initial.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
    initial.onEvent({
      sequence: 2,
      kind: "runtime_read_model_update",
      payload: {
        Version: { Epoch: "epoch-1", Generation: 1, Sequence: 3 },
        Activity: {
          State: "running",
          ActiveStep: {
            RunID: "run",
            StepID: "step",
            ActiveKind: "compaction",
          },
          Reviewer: "inactive",
          QueueAccepting: false,
          DiagnosticRecovery: false,
        },
      },
    });
    initial.onEvent({
      sequence: 3,
      kind: "compaction_status",
      payload: {
        StepID: "step",
        RequestID: "request",
        State: "started",
        Mode: "manual",
        Count: 0,
        Diagnostic: null,
      },
    });
    initial.onEvent({
      sequence: 4,
      kind: "compaction_status",
      payload: {
        StepID: "step",
        RequestID: "request",
        State: "completed",
        Mode: "manual",
        Count: 3,
        Diagnostic: null,
      },
    });
    await vi.waitFor(() => {
      expect(fixture.handlers).toHaveLength(2);
    });
    expect(logger.append).not.toHaveBeenCalled();
    const replacement = requireValue(fixture.handlers[1]);
    replacement.onOpen?.();
    replacement.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: {
        ...hydration(),
        SessionStatus: { ...hydration().SessionStatus, CompactionCount: 3 },
      },
    });

    expect(owner.snapshot.observation).toEqual({ kind: "observing" });
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

const target: ChatSessionTarget = {
  projectID: "project-1",
  workspace: { workspaceID: "workspace-1" },
  sessionID: "123e4567-e89b-42d3-a456-426614174000",
};

function mainViewRead(sequence = 1): ChatMainViewRead {
  return {
    mainView: {
      version: { epoch: "epoch-1", generation: 1, sequence },
      sessionID: target.sessionID,
      sessionName: "Session",
      executionTarget: {
        workspaceID: "workspace-1",
        workspaceName: "Workspace",
        workspaceRoot: "/workspace",
        workspaceAvailability: "available",
        worktree: null,
        cwdRelpath: ".",
        effectiveWorkdir: "/workspace",
      },
      activity: runtimeActivity("registered_idle"),
      status: {
        reviewerFrequency: "off",
        reviewerEnabled: false,
        autoCompactionEnabled: true,
        questionsEnabled: true,
        fastModeAvailable: false,
        fastModeEnabled: false,
        conversationFreshness: 0,
        previousSessionID: null,
        parentAgentSessionID: null,
        navigationTargetSessionID: null,
        lastCommittedAssistantFinalAnswer: null,
        thinkingLevel: "medium",
        compactionMode: "local",
        contextUsage: {
          usedTokens: 10,
          windowTokens: 100,
          cacheHitPercent: 0,
          hasCacheHitPercentage: false,
        },
        compactionCount: 0,
        workflowSession: null,
      },
    },
    goal: { goal: null, availability: "available" },
  };
}

function hydration(): ChatTranscriptPayloadByKind["hydration"] {
  return {
    SessionIdentity: {
      SessionID: target.sessionID,
      SessionName: null,
      ConversationFreshness: 1,
      ExecutionTarget: null,
    },
    SessionStatus: {
      ReviewerFrequency: "edits",
      ReviewerEnabled: true,
      AutoCompactionEnabled: false,
      QuestionsEnabled: false,
      FastModeAvailable: true,
      FastModeEnabled: true,
      ThinkingLevel: "high",
      CompactionMode: "native",
      CompactionCount: 2,
      PreviousSessionID: null,
      ParentAgentSessionID: null,
      NavigationTargetSessionID: null,
      Workflow: null,
    },
    RuntimeReadModelUpdate: {
      Version: { Epoch: "epoch-1", Generation: 1, Sequence: 2 },
      Activity: {
        State: "running",
        ActiveStep: {
          RunID: "223e4567-e89b-42d3-a456-426614174000",
          StepID: "323e4567-e89b-42d3-a456-426614174000",
          ActiveKind: "user_turn",
        },
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
    GoalStatus: {
      Goal: null,
      Availability: "agent_capability_missing",
    },
  };
}

function hydrationWithCursor(cursor: number): ChatTranscriptPayloadByKind["hydration"] {
  return {
    ...hydration(),
    TailSegment: { OlderCursor: cursor, HasMoreAbove: true, Entries: [] },
  };
}

function incompatibleHydration(sessionName: string, text: string): ChatTranscriptPayloadByKind["hydration"] {
  const payload = hydration();
  return {
    ...payload,
    SessionIdentity: { ...payload.SessionIdentity, SessionName: sessionName },
    TailSegment: {
      Entries: [{ ...row(10), User: { Text: text } }],
      OlderCursor: null,
      HasMoreAbove: false,
    },
  };
}

function seededProjection(sequence = 1): ChatProjectionState {
  return reduceChatProjection(emptyChatProjectionState(), {
    kind: "authoritative-read",
    read: mainViewRead(sequence),
    metadataRevisionAtStart: 0,
    goalGenerationAtStart: 0,
    currentGoalGeneration: 0,
  }).state;
}

function runtimeUpdate(
  sequence: number,
  generation: number,
  state: "registered_idle" | "running" | "closing",
) {
  return {
    sequence,
    kind: "runtime_read_model_update",
    payload: {
      Version: { Epoch: "epoch-1", Generation: generation, Sequence: sequence },
      Activity: {
        State: state,
        ActiveStep:
          state === "running"
            ? {
                RunID: "223e4567-e89b-42d3-a456-426614174000",
                StepID: "323e4567-e89b-42d3-a456-426614174000",
                ActiveKind: "user_turn",
              }
            : null,
        Reviewer: "inactive",
        QueueAccepting: state !== "closing",
        DiagnosticRecovery: false,
      },
    },
  } as const;
}

function runtimeActivity(state: "registered_idle") {
  return {
    state,
    activeStep: null,
    reviewer: "inactive" as const,
    queueAccepting: true,
    diagnosticRecovery: false,
  };
}

function runtimeUnavailablePayload(): ChatTranscriptPayloadByKind["runtime_read_model_update"] {
  return {
    Version: { Epoch: "epoch-1", Generation: 1, Sequence: 3 },
    Activity: {
      State: "unavailable",
      ActiveStep: null,
      Reviewer: "inactive",
      QueueAccepting: false,
      DiagnosticRecovery: false,
    },
  };
}

function changedIdentity(): ChatTranscriptPayloadByKind["session_identity"] {
  return {
    SessionID: target.sessionID,
    SessionName: null,
    ConversationFreshness: 1,
    ExecutionTarget: {
      WorkspaceID: "workspace-2",
      WorkspaceName: "Workspace 2",
      WorkspaceRoot: "/workspace-2",
      WorkspaceAvailability: "missing",
      Worktree: {
        ID: "worktree-2",
        Name: "Detached",
        Root: "/workspace-2/worktree",
        Availability: "missing",
      },
      CwdRelpath: "src",
      EffectiveWorkdir: "/workspace-2/worktree/src",
    },
  };
}

function goalStatus(): ChatTranscriptPayloadByKind["goal_status"] {
  return {
    Goal: {
      id: "goal-2",
      objective: "new Goal",
      status: "active",
      created_at: "2026-09-04T10:00:00Z",
      updated_at: "2026-09-04T10:00:00Z",
      Suspended: true,
    },
    Availability: null,
  };
}

function worktreeOutcome(): ChatTranscriptPayloadByKind["worktree_transition_outcome"] {
  return {
    OperationID: "operation-1",
    Transition: "enter",
    State: "completed",
    Failure: null,
    SelectorError: null,
    DeletePrecondition: null,
  };
}

function transcriptPage(olderCursor: number | null): ChatTranscriptPage {
  return {
    sessionID: target.sessionID,
    sessionName: null,
    conversationFreshness: 0,
    olderCursor,
    hasMoreAbove: olderCursor !== null,
    newerCursor: null,
    hasMoreBelow: false,
    latestRollbackCandidate: null,
    entries: [],
  };
}

function runtimeHost(effects: Omit<ChatRuntimeHost, "logger"> = {}): ChatRuntimeHost {
  return {
    logger: { append: vi.fn().mockResolvedValue(undefined) },
    ...effects,
  };
}

function runtimeApi({
  reads = [Promise.resolve(mainViewRead())],
  pages = [Promise.resolve(transcriptPage(null))],
}: Readonly<{
  reads?: readonly Promise<ChatMainViewRead>[];
  pages?: readonly Promise<ChatTranscriptPage>[];
}> = {}) {
  let readIndex = 0;
  let pageIndex = 0;
  const handlers: ChatTranscriptHandler[] = [];
  const getMainView = vi.fn(async () => {
    const read = reads[readIndex++];
    return read ?? Promise.reject(new Error("Unexpected Main View call."));
  });
  const getTranscriptPage = vi.fn(async () => {
    const page = pages[pageIndex++];
    return page ?? Promise.reject(new Error("Unexpected transcript page call."));
  });
  const subscribeTranscript = vi.fn((_target: ChatSessionTarget, handler: ChatTranscriptHandler) => {
    handlers.push(handler);
    return { close: vi.fn() };
  });
  const api: ChatRuntimeApi = { getMainView, getTranscriptPage, subscribeTranscript };
  return { api, getMainView, getTranscriptPage, subscribeTranscript, handlers };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((resolvePromise, rejectPromise) => {
    resolve = resolvePromise;
    reject = rejectPromise;
  });
  return { promise, resolve, reject };
}

function requireValue<T>(value: T | undefined): T {
  if (value === undefined) throw new Error("Required test value is missing.");
  return value;
}
