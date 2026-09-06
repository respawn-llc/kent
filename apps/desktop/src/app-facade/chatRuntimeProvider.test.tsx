import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { expect, it, vi } from "vitest";

import type {
  ApiConnectionSource,
  ChatMainView,
  ChatMainViewRead,
  ChatSessionTarget,
  ChatTranscriptHandler,
  ChatTranscriptPage,
} from "@/api";

import { queryKeys } from "./queryKeys";
import type { ChatRuntimeApi, ChatRuntimeHost } from "./chatRuntime";
import { useChatMainViewState, useChatRuntimeSnapshot } from "./chatRuntimeHooks";
import { ChatRuntimeProvider, type ChatRuntimeProviderApi } from "./chatRuntimeProvider";
import type { ChatTranscriptSink } from "./chatTranscriptHost";

const target: ChatSessionTarget = {
  projectID: "project-1",
  workspace: { workspaceID: "workspace-1" },
  sessionID: "123e4567-e89b-42d3-a456-426614174000",
};

it("shares one mounted query authority and keeps fresh cache visible during detached Retry", async () => {
  const first = deferred<ChatMainViewRead>();
  const second = deferred<ChatMainViewRead>();
  const getMainView = vi.fn().mockReturnValueOnce(first.promise).mockReturnValueOnce(second.promise);
  const subscriptionClose = vi.fn();
  const activateRuntime = vi.fn();
  const releaseRuntime = vi.fn();
  const chat = {
    getMainView,
    getTranscriptPage: vi.fn().mockResolvedValue(transcriptPage()),
    subscribeTranscript: vi.fn().mockReturnValue({ close: subscriptionClose }),
    activateRuntime,
    releaseRuntime,
  };
  const api: ChatRuntimeProviderApi = {
    connection: staticConnection(),
    chat,
  };
  const queryClient = new QueryClient({
    defaultOptions: { queries: { staleTime: 4_000, retry: 1 } },
  });
  queryClient.setQueryData(queryKeys.chatMainView(target.sessionID), fixtureMainView("cached", 1));

  const view = render(
    <QueryClientProvider client={queryClient}>
      <ChatRuntimeProvider api={api} target={target} host={runtimeHost()}>
        <Probe id="first" />
        <Probe id="second" />
        <RetryProbe />
      </ChatRuntimeProvider>
    </QueryClientProvider>,
  );

  expect(screen.getByTestId("first")).toHaveTextContent("cached:unobserved");
  expect(screen.getByTestId("second")).toHaveTextContent("cached:unobserved");
  await waitFor(() => {
    expect(getMainView).toHaveBeenCalledOnce();
  });
  fireEvent.click(screen.getByRole("button", { name: "Retry Main View" }));
  await waitFor(() => {
    expect(getMainView).toHaveBeenCalledTimes(2);
  });
  expect(activateRuntime).not.toHaveBeenCalled();
  expect(releaseRuntime).not.toHaveBeenCalled();

  second.resolve({
    mainView: fixtureMainView("current", 2),
    goal: { goal: null, availability: "available" },
  });
  await waitFor(() => expect(screen.getByTestId("first")).toHaveTextContent("current:observed"));
  expect(screen.getByTestId("second")).toHaveTextContent("current:observed");
  first.resolve({
    mainView: fixtureMainView("stale", 1),
    goal: { goal: null, availability: null },
  });
  await Promise.resolve();
  expect(screen.getByTestId("first")).toHaveTextContent("current:observed");

  view.unmount();
  expect(subscriptionClose).toHaveBeenCalledOnce();
  expect(releaseRuntime).not.toHaveBeenCalled();
});

it("recovers only after disconnected to connected and starts no recovery newest page", async () => {
  const connection = mutableConnection("connected");
  const handlers: ChatTranscriptHandler[] = [];
  const getMainView = vi.fn().mockResolvedValue({
    mainView: fixtureMainView("current", 1),
    goal: { goal: null, availability: "available" },
  });
  const getTranscriptPage = vi.fn().mockResolvedValue(transcriptPage());
  const chat: ChatRuntimeApi = {
    getMainView,
    getTranscriptPage,
    subscribeTranscript: vi.fn((_target: ChatSessionTarget, handler: ChatTranscriptHandler) => {
      handlers.push(handler);
      return { close: vi.fn() };
    }),
  };
  const api: ChatRuntimeProviderApi = { connection, chat };
  const transcript = transcriptSink();
  const view = render(
    <QueryClientProvider client={new QueryClient()}>
      <ChatRuntimeProvider api={api} target={target} host={runtimeHost(transcript)}>
        <Probe id="reconnect" />
      </ChatRuntimeProvider>
    </QueryClientProvider>,
  );
  await waitFor(() => {
    expect(getMainView).toHaveBeenCalledOnce();
  });
  expect(handlers).toHaveLength(1);
  expect(getTranscriptPage).toHaveBeenCalledOnce();

  connection.set("connecting");
  connection.set("connected");
  await Promise.resolve();
  expect(getMainView).toHaveBeenCalledOnce();
  expect(handlers).toHaveLength(1);

  connection.set("disconnected");
  connection.set("connecting");
  connection.set("connected");
  await waitFor(() => {
    expect(getMainView).toHaveBeenCalledTimes(2);
  });
  expect(handlers).toHaveLength(2);
  expect(getTranscriptPage).toHaveBeenCalledOnce();
  expect(transcript.recoveryStarted).toHaveBeenCalledOnce();
  view.unmount();
});

function Probe({ id }: Readonly<{ id: string }>) {
  const mainView = useChatMainViewState();
  const runtime = useChatRuntimeSnapshot();
  return (
    <div data-testid={id}>
      {mainView.data?.sessionName ?? "none"}:{runtime.goal.kind}
    </div>
  );
}

function RetryProbe() {
  const mainView = useChatMainViewState();
  return <button onClick={() => void mainView.retry()}>Retry Main View</button>;
}

function fixtureMainView(sessionName: string, sequence: number): ChatMainView {
  return {
    version: { epoch: "epoch-1", generation: 1, sequence },
    sessionID: target.sessionID,
    sessionName,
    executionTarget: {
      workspaceID: "workspace-1",
      workspaceName: "Workspace",
      workspaceRoot: "/workspace",
      workspaceAvailability: "available",
      worktree: null,
      cwdRelpath: ".",
      effectiveWorkdir: "/workspace",
    },
    activity: {
      state: "registered_idle",
      activeStep: null,
      reviewer: "inactive",
      queueAccepting: true,
      diagnosticRecovery: false,
    },
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
        usedTokens: 0,
        windowTokens: 100,
        cacheHitPercent: 0,
        hasCacheHitPercentage: false,
      },
      compactionCount: 0,
      workflowSession: null,
    },
  };
}

function transcriptPage(): ChatTranscriptPage {
  return {
    sessionID: target.sessionID,
    sessionName: null,
    conversationFreshness: 0,
    olderCursor: null,
    hasMoreAbove: false,
    newerCursor: null,
    hasMoreBelow: false,
    latestRollbackCandidate: null,
    entries: [],
  };
}

function transcriptSink() {
  return {
    openingStarted: vi.fn(),
    openingSucceeded: vi.fn(),
    openingFailed: vi.fn(),
    hydration: vi.fn(),
    event: vi.fn(),
    recoveryStarted: vi.fn(),
    dispose: vi.fn(),
  } satisfies ChatTranscriptSink;
}

function runtimeHost(transcript = transcriptSink()): ChatRuntimeHost {
  return { transcript };
}

function staticConnection(): ApiConnectionSource {
  return {
    snapshot: () => ({ phase: "connected", lastError: null, generation: 1 }),
    subscribe: () => () => undefined,
  };
}

function mutableConnection(initial: ReturnType<ApiConnectionSource["snapshot"]>["phase"]) {
  let snapshot: ReturnType<ApiConnectionSource["snapshot"]> = {
    phase: initial,
    lastError: null,
    generation: 0,
  };
  const listeners = new Set<() => void>();
  return {
    snapshot: () => snapshot,
    subscribe(listener: () => void) {
      listeners.add(listener);
      return () => listeners.delete(listener);
    },
    set(phase: typeof snapshot.phase) {
      snapshot = { phase, lastError: null, generation: snapshot.generation + 1 };
      for (const listener of listeners) listener();
    },
  };
}

function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((resolvePromise) => {
    resolve = resolvePromise;
  });
  return { promise, resolve };
}
