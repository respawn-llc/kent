import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { StrictMode } from "react";
import { expect, it, vi } from "vitest";

import type { ChatSessionTarget, ChatTranscriptHandler } from "@/api";
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

it("keeps transcript failure local without another Main View or newest-page read", async () => {
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
  const api: ChatRuntimeProviderApi = { chat };
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

  act(() => handlers[0]?.onTransportLoss?.());
  await Promise.resolve();
  expect(getMainView).toHaveBeenCalledOnce();
  expect(handlers).toHaveLength(1);

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
