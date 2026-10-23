import { transcriptSource, type ChatTranscriptHandler } from "@/test-support/chat-runtime";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, render, screen, waitFor } from "@testing-library/react";
import { Component, StrictMode, type ReactElement } from "react";
import { ContractError } from "@/api";
import { RegistryProvider } from "@effect/atom-react";
import { expect, it, vi } from "bun:test";

import {
  deferred,
  hydration,
  mainViewRead,
  runtimeHost,
  target,
  transcriptPage,
} from "@/test-support/chat-runtime";

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
    subscribeTranscript: vi.fn(() => transcriptSource(() => ({ close: subscriptionClose }))),
  };
  const api: ChatRuntimeProviderApi = {
    chat,
  };
  const view = render(
    <RegistryProvider>
      <QueryClientProvider client={new QueryClient()}>
        <ChatRuntimeProvider api={api} target={target} host={runtimeHost()}>
          <Probe id="first" />
          <Probe id="second" />
        </ChatRuntimeProvider>
      </QueryClientProvider>
    </RegistryProvider>,
  );

  await waitFor(() => {
    expect(screen.getByTestId("first")).toHaveTextContent("current:observed");
  });
  expect(screen.getByTestId("second")).toHaveTextContent("current:observed");
  expect(getMainView).toHaveBeenCalledTimes(1);

  view.unmount();
  await waitFor(() => {
    expect(subscriptionClose).toHaveBeenCalledTimes(1);
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
    subscribeTranscript: vi.fn(() =>
      transcriptSource((handler) => {
        handlers.push(handler);
        return { close: vi.fn() };
      }),
    ),
  };
  const api: ChatRuntimeProviderApi = { chat };
  const view = render(
    <RegistryProvider>
      <QueryClientProvider client={new QueryClient()}>
        <ChatRuntimeProvider api={api} target={target} host={runtimeHost()}>
          <Probe id="reconnect" />
        </ChatRuntimeProvider>
      </QueryClientProvider>
    </RegistryProvider>,
  );
  await waitFor(() => {
    expect(getMainView).toHaveBeenCalledTimes(1);
  });
  expect(handlers).toHaveLength(1);
  expect(getTranscriptPage).toHaveBeenCalledTimes(1);

  act(() => handlers[0]?.onTransportLoss?.());
  await Promise.resolve();
  expect(getMainView).toHaveBeenCalledTimes(1);
  expect(handlers).toHaveLength(1);

  expect(getTranscriptPage).toHaveBeenCalledTimes(1);
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
      subscribeTranscript: vi.fn(() => transcriptSource(() => ({ close: subscriptionClose }))),
    },
  };
  const view = render(
    <StrictMode>
      <RegistryProvider>
        <QueryClientProvider client={new QueryClient()}>
          <ChatRuntimeProvider api={api} target={target} host={runtimeHost()}>
            <Probe id="strict" />
          </ChatRuntimeProvider>
        </QueryClientProvider>
      </RegistryProvider>
    </StrictMode>,
  );

  await waitFor(() => {
    expect(screen.getByTestId("strict")).toHaveTextContent("strict:observed");
  });
  expect(getMainView).toHaveBeenCalledTimes(1);
  expect(subscriptionClose).not.toHaveBeenCalled();

  view.unmount();
  await waitFor(() => {
    expect(subscriptionClose).toHaveBeenCalledTimes(1);
  });
});

it("switches Session observations without admitting late reads or events from the departed Session", async () => {
  const previousRead = deferred<ReturnType<typeof mainViewRead>>();
  const previousPage = deferred<ReturnType<typeof transcriptPage>>();
  const nextTarget = { ...target, sessionID: "223e4567-e89b-42d3-a456-426614174000" };
  const handlers: ChatTranscriptHandler[] = [];
  const closed = vi.fn();
  const chat: ChatRuntimeApi = {
    getMainView: vi
      .fn()
      .mockReturnValueOnce(previousRead.promise)
      .mockResolvedValue({
        ...mainViewRead(),
        mainView: { ...namedMainView("next", 2), sessionID: nextTarget.sessionID },
      }),
    getTranscriptPage: vi
      .fn()
      .mockReturnValueOnce(previousPage.promise)
      .mockResolvedValue(transcriptPage(null)),
    subscribeTranscript: vi.fn(() =>
      transcriptSource((handler) => {
        handlers.push(handler);
        return { close: closed };
      }),
    ),
  };
  const client = new QueryClient();
  const host = runtimeHost();
  const content = (selected: typeof target) => (
    <RegistryProvider>
      <QueryClientProvider client={client}>
        <ChatRuntimeProvider api={{ chat }} target={selected} host={host}>
          <Probe id="session" />
        </ChatRuntimeProvider>
      </QueryClientProvider>
    </RegistryProvider>
  );
  const view = render(content(target));
  await waitFor(() => {
    expect(chat.getMainView).toHaveBeenCalledTimes(1);
  });
  view.rerender(content(nextTarget));
  await waitFor(() => {
    expect(screen.getByTestId("session")).toHaveTextContent("next:observed");
  });
  await waitFor(() => {
    expect(closed).toHaveBeenCalledTimes(1);
  });
  await act(async () => {
    previousRead.resolve({ ...mainViewRead(), mainView: namedMainView("departed", 1) });
    previousPage.resolve(transcriptPage(null));
    handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
  });
  expect(screen.getByTestId("session")).toHaveTextContent("next:observed");
  expect(chat.getTranscriptPage).toHaveBeenCalledTimes(2);
  view.unmount();
  await waitFor(() => {
    expect(closed).toHaveBeenCalledTimes(2);
  });
});

it("surfaces a debug transcript integrity failure at the mounted boundary", async () => {
  globalThis.__KENT_TEST_ENV__.KENT_DEBUG = "true";
  const consoleError = vi.spyOn(console, "error").mockImplementation(() => undefined);
  const caught = vi.fn<(error: Error) => void>();
  const transcriptError = vi.fn();
  const chat: ChatRuntimeApi = {
    getMainView: vi.fn().mockResolvedValue(mainViewRead()),
    getTranscriptPage: vi.fn().mockResolvedValue({ ...transcriptPage(null), hasMoreAbove: true }),
    subscribeTranscript: vi.fn(() => transcriptSource(() => ({ close: vi.fn() }))),
  };
  class Boundary extends Component<{ content: ReactElement }, { failed: boolean }> {
    override state = { failed: false };
    static getDerivedStateFromError() {
      return { failed: true };
    }
    override componentDidCatch(error: Error) {
      caught(error);
    }
    override render() {
      return this.state.failed ? null : this.props.content;
    }
  }
  try {
    const view = render(
      <RegistryProvider>
        <QueryClientProvider client={new QueryClient()}>
          <Boundary
            content={
              <ChatRuntimeProvider
                api={{ chat }}
                target={target}
                host={runtimeHost({ onTranscriptError: transcriptError })}
              >
                <Probe id="debug" />
              </ChatRuntimeProvider>
            }
          />
        </QueryClientProvider>
      </RegistryProvider>,
    );
    await waitFor(() => {
      expect(caught).toHaveBeenCalledTimes(1);
    });
    expect(caught.mock.calls[0]?.[0].cause).toBeInstanceOf(ContractError);
    expect(transcriptError).not.toHaveBeenCalled();
    view.unmount();
  } finally {
    consoleError.mockRestore();
    globalThis.__KENT_TEST_ENV__.KENT_DEBUG = "false";
  }
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
