import { act, waitFor } from "@testing-library/react";
import { RuntimeHarness } from "@/test-support/chat-runtime";
import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "bun:test";

import type { ChatMainView, ChatMainViewRead, ChatTranscriptPage } from "@/api";
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

import { chatMainViewQueryOptions, emptyChatProjectionState, reduceChatProjection } from "./chatRuntime";
import { executeChatTranscriptPage } from "./chatTranscriptHost";
import { queryKeys } from "./queryKeys";

describe("Chat Main View admission", () => {
  it("counts active Session processes from hydration and lifecycle events without page reads", async () => {
    const fixture = runtimeApi();
    const owner = new RuntimeHarness(fixture.api, target, new QueryClient(), runtimeHost());
    owner.start();
    const process = {
      ActivityID: "activity-1",
      ProcessID: "process-1",
      OwnerStepID: "step-1",
      Lifecycle: "backgrounded" as const,
      Command: "sleep 10",
      Workdir: "/workspace",
      UserRequestedKill: false,
      NoticeSuppressed: false,
    };
    await act(async () => {
      fixture.handlers[0]?.onEvent({
        sequence: 1,
        kind: "hydration",
        payload: {
          ...hydration(),
          BackgroundActivities: [process],
        },
      });
    });
    expect(owner.snapshot.activeProcessCount).toBe(1);
    await act(async () => {
      fixture.handlers[0]?.onEvent({ sequence: 2, kind: "background_activity", payload: process });
    });
    expect(owner.snapshot.activeProcessCount).toBe(1);
    await act(async () => {
      fixture.handlers[0]?.onEvent({
        sequence: 3,
        kind: "background_activity",
        payload: {
          ...process,
          ActivityID: "activity-2",
          ProcessID: "process-2",
        },
      });
    });
    expect(owner.snapshot.activeProcessCount).toBe(2);
    await act(async () => {
      fixture.handlers[0]?.onEvent({
        sequence: 4,
        kind: "background_activity",
        payload: {
          ...process,
          Lifecycle: "completed",
        },
      });
    });
    expect(owner.snapshot.activeProcessCount).toBe(1);
    await act(async () => {
      fixture.handlers[0]?.onEvent({
        sequence: 5,
        kind: "background_activity",
        payload: {
          ...process,
          ActivityID: "activity-2",
          ProcessID: "process-2",
          Lifecycle: "killed",
        },
      });
    });
    expect(owner.snapshot.activeProcessCount).toBe(0);
    owner.dispose();
  });
  it("publishes pending prompt hydration through the mounted owner", async () => {
    const fixture = runtimeApi();
    const owner = new RuntimeHarness(fixture.api, target, new QueryClient(), runtimeHost());
    owner.start();
    const prompt = {
      kind: "ordinary" as const,
      toolCallID: "question-one",
      sessionID: target.sessionID,
      stepID: "323e4567-e89b-42d3-a456-426614174000",
      question: "Choose?",
      suggestions: ["one", "two"],
      recommendedOptionIndex: 2,
      createdAt: "2026-09-12T00:00:00.000Z",
    };
    await act(async () => {
      fixture.handlers[0]?.onEvent({
        sequence: 1,
        kind: "hydration",
        payload: { ...hydration(), PendingPrompts: [{ state: "pending", prompt }] },
      });
    });
    expect(owner.snapshot.pendingPrompts).toEqual([prompt]);
    const later = { ...prompt, toolCallID: "question-two", createdAt: "2026-09-12T00:00:01.000Z" };
    await act(async () => {
      fixture.handlers[0]?.onEvent({
        sequence: 2,
        kind: "prompt",
        payload: { state: "pending", prompt: later },
      });
    });
    await act(async () => {
      fixture.handlers[0]?.onEvent({
        sequence: 3,
        kind: "prompt",
        payload: { state: "pending", prompt: { ...prompt, question: "Updated?" } },
      });
    });
    expect(owner.snapshot.pendingPrompts.map((item) => item.toolCallID)).toEqual([
      "question-one",
      "question-two",
    ]);
    expect(owner.snapshot.pendingPrompts[0]?.question).toBe("Updated?");
    await act(async () => {
      fixture.handlers[0]?.onEvent({
        sequence: 4,
        kind: "prompt",
        payload: { state: "resolved", toolCallID: "question-one" },
      });
    });
    expect(owner.snapshot.pendingPrompts).toEqual([later]);
    await act(async () => {
      fixture.handlers[0]?.onEvent({
        sequence: 5,
        kind: "prompt",
        payload: { state: "pending", prompt },
      });
    });
    expect(owner.snapshot.pendingPrompts.map((item) => item.toolCallID)).toEqual([
      "question-one",
      "question-two",
    ]);
    owner.dispose();
  });

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
          Category: "main",
          ShortID: null,
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

    expect(queryClient.fetchQuery(options)).rejects.toThrow("unavailable");
    expect(fixture.getMainView).toHaveBeenCalledTimes(1);
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
  it("retries only failed opening-page work while observation remains dormant", async () => {
    const opening = deferred<ChatTranscriptPage>();
    const fixture = runtimeApi({
      reads: [Promise.resolve(mainViewRead())],
      pages: [opening.promise, Promise.resolve(transcriptPage(null))],
    });
    const queryClient = new QueryClient();
    const owner = new RuntimeHarness(fixture.api, target, queryClient, runtimeHost());
    owner.start();
    expect(fixture.handlers).toHaveLength(1);
    await onQueryData(queryClient, queryKeys.chatMainView(target.sessionID), (view) => view !== undefined);
    expect(fixture.getMainView).toHaveBeenCalledTimes(1);
    opening.reject(new Error("Opening page unavailable"));
    await onOwnerSnapshot(owner, (snapshot) => snapshot.transcript.opening.kind === "error");

    owner.transcript.dispatch({ kind: "opening-retry" });

    await onOwnerSnapshot(owner, (snapshot) => snapshot.transcript.opening.kind === "ready");
    expect(fixture.getTranscriptPage).toHaveBeenCalledTimes(2);
    expect(fixture.getMainView).toHaveBeenCalledTimes(1);
    expect(fixture.handlers).toHaveLength(1);
    expect(owner.snapshot.observation.kind).toBe("loading");
    owner.dispose();
  });

  it("starts independent branches and detaches an unresolved Main View before Retry", async () => {
    const first = deferred<ChatMainViewRead>();
    const second = deferred<ChatMainViewRead>();
    const fixture = runtimeApi({
      reads: [first.promise, second.promise],
      pages: [Promise.resolve(transcriptPage(null))],
    });
    const activateRuntime = vi.fn();
    const api = { ...fixture.api, activateRuntime };
    const queryClient = new QueryClient();
    const owner = new RuntimeHarness(api, target, queryClient, runtimeHost());

    const initialReadStarted = new Promise<void>((resolve) => {
      const unsubscribe = queryClient.getQueryCache().subscribe((event) => {
        if (
          event.query !==
          queryClient
            .getQueryCache()
            .find({ queryKey: queryKeys.chatMainView(target.sessionID), exact: true })
        )
          return;
        if (event.type !== "updated" || event.action.type !== "fetch") return;
        unsubscribe();
        resolve();
      });
    });
    owner.start();
    await initialReadStarted;
    expect(fixture.getMainView).toHaveBeenCalledTimes(1);
    expect(fixture.getTranscriptPage).toHaveBeenCalledTimes(1);
    expect(fixture.subscribeTranscript).toHaveBeenCalledTimes(1);
    await onOwnerSnapshot(owner, (snapshot) => snapshot.transcript.opening.kind === "ready");
    expect(activateRuntime).not.toHaveBeenCalled();

    await act(async () => {
      owner.retryMainView();
    });
    expect(fixture.getMainView).toHaveBeenCalledTimes(2);
    second.resolve({
      ...mainViewRead(5),
      mainView: { ...mainViewRead(5).mainView, sessionName: "newer" },
    });
    await onQueryData(
      queryClient,
      queryKeys.chatMainView(target.sessionID),
      (view) => view?.sessionName === "newer",
    );
    first.resolve({
      ...mainViewRead(2),
      mainView: { ...mainViewRead(2).mainView, sessionName: "stale" },
    });
    await first.promise;
    const currentMainView = queryClient.getQueryData<ChatMainView>(queryKeys.chatMainView(target.sessionID));
    expect(currentMainView).toMatchObject({
      sessionName: "newer",
      version: { sequence: 5 },
    });
    owner.dispose();
  });

  it("keeps transcript failure and Retry independent and forwards ordered host facts", async () => {
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
    const owner = new RuntimeHarness(fixture.api, target, queryClient, host);
    owner.start();
    expect(fixture.handlers).toHaveLength(1);
    await onQueryData(queryClient, queryKeys.chatMainView(target.sessionID), (view) => view !== undefined);
    const initial = requireValue(fixture.handlers[0]);
    await act(async () => {
      initial.onOpen?.();
    });
    await act(async () => {
      initial.onEvent({ sequence: 1, kind: "hydration", payload: hydrationWithCursor(17) });
    });
    await act(async () => {
      initial.onEvent({
        sequence: 2,
        kind: "session_identity",
        payload: changedIdentity(),
      });
    });
    await act(async () => {
      initial.onEvent({
        sequence: 3,
        kind: "worktree_transition_outcome",
        payload: worktreeOutcome(),
      });
    });
    await act(async () => {
      initial.onEvent({ sequence: 4, kind: "goal_status", payload: goalStatus() });
    });
    await act(async () => {
      initial.onEvent({
        sequence: 5,
        kind: "human_input_interrupted",
        payload: { Items: [{ QueueItemID: "queue-1", Text: "restore me" }] },
      });
    });
    expect(queryClient.getQueryData<ChatMainView>(queryKeys.chatMainView(target.sessionID))).toMatchObject({
      sessionName: null,
      executionTarget: { workspaceID: "workspace-2", workspaceAvailability: "missing" },
    });
    expect(outcome).toHaveBeenCalledTimes(1);
    expect(interrupted).toHaveBeenCalledTimes(1);
    expect(owner.snapshot.goal).toMatchObject({
      kind: "observed",
      value: { goal: { id: "goal-2" }, availability: null },
    });

    await act(async () => {
      initial.onEvent({
        sequence: 7,
        kind: "session_status",
        payload: hydration().SessionStatus,
      });
    });
    expect(owner.snapshot.observation.kind).toBe("error");
    expect(fixture.getTranscriptPage).toHaveBeenCalledTimes(1);

    const explicitPage = executeChatTranscriptPage(fixture.api, target, {
      kind: "older",
      cursor: 17,
    });
    expect(fixture.getTranscriptPage).toHaveBeenCalledTimes(2);
    expect(owner.snapshot.observation.kind).toBe("error");
    await act(async () => {
      owner.retryTranscriptObservation();
    });
    expect(fixture.handlers).toHaveLength(2);
    expect(fixture.getMainView).toHaveBeenCalledTimes(1);
    const retry = requireValue(fixture.handlers[1]);
    await act(async () => {
      retry.onOpen?.();
    });
    await act(async () => {
      retry.onEvent({ sequence: 1, kind: "hydration", payload: hydrationWithCursor(29) });
    });
    expect(owner.snapshot.transcript.older).toEqual({ kind: "idle", cursor: 29 });
    replacementPage.resolve(transcriptPage(null));
    expect(explicitPage).resolves.toEqual(transcriptPage(null));
    owner.dispose();
  });

  it("ignores delayed diagnostics from a failed observation after explicit Retry", async () => {
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
    const queryClient = new QueryClient();
    const owner = new RuntimeHarness(fixture.api, target, queryClient, { logger });
    owner.start();
    expect(fixture.handlers).toHaveLength(1);
    await onQueryData(queryClient, queryKeys.chatMainView(target.sessionID), (view) => view !== undefined);
    const initial = requireValue(fixture.handlers[0]);
    await act(async () => {
      initial.onOpen?.();
    });
    await act(async () => {
      initial.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
    });
    await act(async () => {
      initial.onEvent({
        sequence: 3,
        kind: "session_status",
        payload: hydration().SessionStatus,
      });
    });
    await act(async () => {
      initial.onEvent({
        sequence: 4,
        kind: "session_status",
        payload: hydration().SessionStatus,
      });
    });

    expect(owner.snapshot.observation.kind).toBe("error");
    expect(logger.append).toHaveBeenCalledTimes(1);
    expect(fixture.handlers).toHaveLength(1);

    await act(async () => {
      owner.retryTranscriptObservation();
    });
    expect(fixture.handlers).toHaveLength(2);
    const replacement = requireValue(fixture.handlers[1]);
    await act(async () => {
      replacement.onOpen?.();
    });
    await act(async () => {
      replacement.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
    });
    expect(owner.snapshot.observation.kind).toBe("observing");

    logging.resolve(undefined);
    await Promise.resolve();
    await Promise.resolve();

    expect(owner.snapshot.observation.kind).toBe("observing");
    expect(fixture.handlers).toHaveLength(2);
    owner.dispose();
  });

  it("does not start production recovery for autonomous resident failure in debug mode", async () => {
    globalThis.__KENT_TEST_ENV__.KENT_DEBUG = "true";
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
    const queryClient = new QueryClient();
    const owner = new RuntimeHarness(fixture.api, target, queryClient, { logger });
    owner.start();
    await Promise.all([
      onOwnerSnapshot(owner, (snapshot) => snapshot.observation.kind === "error"),
      onQueryData(queryClient, queryKeys.chatMainView(target.sessionID), (view) => view !== undefined),
    ]);
    expect(logger.append).toHaveBeenCalledTimes(1);
    expect(owner.snapshot.observation.kind).toBe("error");
    expect(fixture.handlers).toHaveLength(1);
    expect(fixture.getMainView).toHaveBeenCalledTimes(1);
    owner.dispose();
  });

  it("keeps rejected hydration outside projection state across explicit attempts", async () => {
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
    const owner = new RuntimeHarness(fixture.api, target, queryClient, { logger });
    owner.start();
    expect(fixture.handlers).toHaveLength(1);
    await Promise.all([
      onOwnerSnapshot(owner, (snapshot) => snapshot.transcript.opening.kind === "ready"),
      onQueryData(queryClient, queryKeys.chatMainView(target.sessionID), (view) => view !== undefined),
    ]);

    const initial = requireValue(fixture.handlers[0]);
    await act(async () => {
      initial.onOpen?.();
    });
    await act(async () => {
      initial.onEvent({
        sequence: 1,
        kind: "hydration",
        payload: incompatibleHydration("rejected-initial", "incompatible initial"),
      });
    });
    expect(owner.snapshot.observation.kind).toBe("error");
    expect(fixture.handlers).toHaveLength(1);
    await act(async () => {
      owner.retryTranscriptObservation();
    });
    expect(fixture.handlers).toHaveLength(2);

    const replacement = requireValue(fixture.handlers[1]);
    await act(async () => {
      replacement.onOpen?.();
    });
    await act(async () => {
      replacement.onEvent({
        sequence: 1,
        kind: "hydration",
        payload: incompatibleHydration("rejected-replacement", "incompatible replacement"),
      });
    });

    expect(owner.snapshot.observation.kind).toBe("error");
    expect(fixture.handlers).toHaveLength(2);
    expect(
      queryClient.getQueryData<ChatMainView>(queryKeys.chatMainView(target.sessionID))?.sessionName,
    ).toBe("Session");
    expect(owner.snapshot.goal).toMatchObject({
      kind: "observed",
      value: { availability: "available" },
    });
    expect(logger.append).toHaveBeenCalledTimes(2);
    owner.dispose();
  });

  it("reports socket loss and discards provisional transcript state without invalidating page work", async () => {
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
    const queryClient = new QueryClient();
    const owner = new RuntimeHarness(fixture.api, target, queryClient, runtimeHost());
    owner.start();
    expect(fixture.handlers).toHaveLength(1);
    await Promise.all([
      onOwnerSnapshot(owner, (snapshot) => snapshot.transcript.opening.kind === "ready"),
      onQueryData(queryClient, queryKeys.chatMainView(target.sessionID), (view) => view !== undefined),
    ]);
    const initial = requireValue(fixture.handlers[0]);
    const hydrated = hydration();
    await act(async () => {
      initial.onOpen?.();
    });
    await act(async () => {
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
    });
    await act(async () => {
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
    });
    owner.transcript.dispatch({
      kind: "edge-visit",
      direction: "older",
      older: true,
      newer: false,
    });
    expect(owner.snapshot.transcript.items.some((item) => !("row" in item))).toBe(true);
    expect(owner.snapshot.transcript.older).toEqual({ kind: "loading", cursor: 17 });

    await act(async () => {
      initial.onTransportLoss?.();
    });

    expect(owner.snapshot.transcript.items.every((item) => "row" in item)).toBe(true);
    expect(owner.snapshot.transcript.older).toEqual({ kind: "loading", cursor: 17 });
    expect(owner.snapshot.observation.kind).toBe("error");
    expect(fixture.handlers).toHaveLength(1);
    expect(fixture.getMainView).toHaveBeenCalledTimes(1);

    olderPage.resolve({
      ...transcriptPage(null),
      entries: [row(5)],
      newerCursor: 17,
      hasMoreBelow: true,
    });
    await onOwnerSnapshot(owner, (snapshot) => snapshot.transcript.older.kind === "idle");
    owner.dispose();
  });

  it("keeps a directional page current across normal Runtime stop", async () => {
    const olderPage = deferred<ChatTranscriptPage>();
    const fixture = runtimeApi({
      reads: [Promise.resolve(mainViewRead())],
      pages: [Promise.resolve(transcriptPage(17)), olderPage.promise],
    });
    const queryClient = new QueryClient();
    const owner = new RuntimeHarness(fixture.api, target, queryClient, runtimeHost());
    owner.start();
    expect(fixture.handlers).toHaveLength(1);
    await onQueryData(queryClient, queryKeys.chatMainView(target.sessionID), (view) => view !== undefined);
    const initial = requireValue(fixture.handlers[0]);
    await act(async () => {
      initial.onOpen?.();
    });
    await act(async () => {
      initial.onEvent({ sequence: 1, kind: "hydration", payload: hydrationWithCursor(17) });
    });
    const pendingPage = executeChatTranscriptPage(fixture.api, target, {
      kind: "older",
      cursor: 17,
    });
    await act(async () => {
      initial.onEvent({
        sequence: 2,
        kind: "runtime_read_model_update",
        payload: runtimeUnavailablePayload(),
      });
    });
    await act(async () => {
      initial.onComplete({ code: null, message: null, reason: null });
    });

    expect(fixture.handlers).toHaveLength(2);
    expect(owner.snapshot.observation.kind).toBe("observing");
    expect(fixture.getMainView).toHaveBeenCalledTimes(1);
    olderPage.resolve({
      ...transcriptPage(null),
      newerCursor: 17,
      hasMoreBelow: true,
    });
    expect(pendingPage).resolves.toMatchObject({ newerCursor: 17 });
    const reattached = requireValue(fixture.handlers[1]);
    await act(async () => {
      reattached.onOpen?.();
    });
    await act(async () => {
      reattached.onEvent({ sequence: 1, kind: "hydration", payload: hydrationWithCursor(17) });
    });
    expect(owner.snapshot.transcript.older).toEqual({ kind: "idle", cursor: 17 });
    expect(fixture.getTranscriptPage).toHaveBeenCalledTimes(2);
    owner.dispose();
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
    const first = new RuntimeHarness(fixture.api, target, firstClient, runtimeHost());
    const second = new RuntimeHarness(fixture.api, target, secondClient, runtimeHost());
    first.start();
    second.start();
    await Promise.all([
      onQueryData(
        firstClient,
        queryKeys.chatMainView(target.sessionID),
        (view) => view?.sessionName === "window-a",
      ),
      onQueryData(
        secondClient,
        queryKeys.chatMainView(target.sessionID),
        (view) => view?.sessionName === "window-b",
      ),
    ]);
    first.dispose();
    second.dispose();
  });
});

async function onOwnerSnapshot(
  owner: RuntimeHarness,
  predicate: (snapshot: RuntimeHarness["snapshot"]) => boolean,
): Promise<void> {
  await waitFor(() => {
    expect(predicate(owner.snapshot)).toBe(true);
  });
}

async function onQueryData(
  queryClient: QueryClient,
  queryKey: readonly unknown[],
  predicate: (value: ChatMainView | undefined) => boolean,
): Promise<void> {
  if (predicate(queryClient.getQueryData<ChatMainView>(queryKey))) return Promise.resolve();
  return new Promise((resolve) => {
    const unsubscribe = queryClient.getQueryCache().subscribe(() => {
      if (!predicate(queryClient.getQueryData<ChatMainView>(queryKey))) return;
      unsubscribe();
      resolve();
    });
  });
}
