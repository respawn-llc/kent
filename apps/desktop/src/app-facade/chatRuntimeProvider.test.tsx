import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { expect, it, vi } from "vitest";

import type { ApiConnectionSource, ChatSessionTarget, ChatTranscriptHandler } from "@/api";
import { mainViewRead, runtimeHost, target, transcriptPage } from "@/test-support/chat-runtime";

import type { ChatRuntimeApi } from "./chatRuntime";
import { useChatMainViewState, useChatRuntimeSnapshot } from "./chatRuntimeHooks";
import { ChatRuntimeProvider, type ChatRuntimeProviderApi } from "./chatRuntimeProvider";

it("shares one mounted query authority across provider consumers", async () => {
  const getMainView = vi.fn().mockResolvedValue({
    mainView: namedMainView("current", 2),
    goal: { goal: null, availability: "available" },
  });
  const subscriptionClose = vi.fn();
  const chat: ChatRuntimeApi = {
    getMainView,
    getTranscriptPage: vi.fn().mockResolvedValue(transcriptPage(null)),
    subscribeTranscript: vi.fn().mockReturnValue({ close: subscriptionClose }),
  };
  const api: ChatRuntimeProviderApi = {
    connection: staticConnection(),
    chat,
  };
  const view = render(
    <QueryClientProvider client={new QueryClient()}>
      <ChatRuntimeProvider api={api} target={target} host={runtimeHost()}>
        <Probe id="first" />
        <Probe id="second" />
      </ChatRuntimeProvider>
    </QueryClientProvider>,
  );

  await waitFor(() => {
    expect(screen.getByTestId("first")).toHaveTextContent("current:observed");
  });
  expect(screen.getByTestId("second")).toHaveTextContent("current:observed");
  expect(getMainView).toHaveBeenCalledOnce();

  view.unmount();
  await waitFor(() => {
    expect(subscriptionClose).toHaveBeenCalledOnce();
  });
});

it("recovers only after disconnected to connected and starts no recovery newest page", async () => {
  const connection = mutableConnection("connected");
  const handlers: ChatTranscriptHandler[] = [];
  const getMainView = vi.fn().mockResolvedValue({
    mainView: namedMainView("current", 1),
    goal: { goal: null, availability: "available" },
  });
  const getTranscriptPage = vi.fn().mockResolvedValue(transcriptPage(null));
  const chat: ChatRuntimeApi = {
    getMainView,
    getTranscriptPage,
    subscribeTranscript: vi.fn((_target: ChatSessionTarget, handler: ChatTranscriptHandler) => {
      handlers.push(handler);
      return { close: vi.fn() };
    }),
  };
  const api: ChatRuntimeProviderApi = { connection, chat };
  const view = render(
    <QueryClientProvider client={new QueryClient()}>
      <ChatRuntimeProvider api={api} target={target} host={runtimeHost()}>
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
  view.unmount();
});

it("keeps the mounted owner active through StrictMode effect replay", async () => {
  const subscriptionClose = vi.fn();
  const getMainView = vi.fn().mockResolvedValue({
    mainView: namedMainView("strict", 1),
    goal: { goal: null, availability: "available" },
  });
  const api: ChatRuntimeProviderApi = {
    connection: staticConnection(),
    chat: {
      getMainView,
      getTranscriptPage: vi.fn().mockResolvedValue(transcriptPage(null)),
      subscribeTranscript: vi.fn().mockReturnValue({ close: subscriptionClose }),
    },
  };
  const view = render(
    <StrictMode>
      <QueryClientProvider client={new QueryClient()}>
        <ChatRuntimeProvider api={api} target={target} host={runtimeHost()}>
          <Probe id="strict" />
        </ChatRuntimeProvider>
      </QueryClientProvider>
    </StrictMode>,
  );

  await waitFor(() => {
    expect(screen.getByTestId("strict")).toHaveTextContent("strict:observed");
  });
  expect(getMainView).toHaveBeenCalledOnce();
  expect(subscriptionClose).not.toHaveBeenCalled();

  view.unmount();
  await waitFor(() => {
    expect(subscriptionClose).toHaveBeenCalledOnce();
  });
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

function namedMainView(sessionName: string, sequence: number) {
  return { ...mainViewRead(sequence).mainView, sessionName };
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
