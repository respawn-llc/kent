import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ChatTranscriptHandler } from "@/api";
import { ChatRuntimeProvider, useChatRuntimeOwner } from "@/app-facade";
import { appI18n } from "@/i18n";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import {
  deferred,
  hydration,
  mainViewRead,
  runtimeHost,
  target,
  transcriptPage,
} from "@/test-support/chat-runtime";
import { SessionChatContext } from "./SessionChatContext";
import { ChatComposerSurface } from "./ChatComposerSurface";
import { ChatComposer } from "./ChatComposer";
import { useChatComposer } from "./useChatComposer";

it("presents admitted usage and policy updates from the ordinary observation", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getMainView").mockResolvedValue(mainViewRead());
  vi.spyOn(services.api.chat, "getTranscriptPage").mockResolvedValue(transcriptPage(null));
  const handlers: ChatTranscriptHandler[] = [];
  vi.spyOn(services.api.chat, "subscribeTranscript").mockImplementation((_target, handler) => {
    handlers.push(handler);
    return { close: vi.fn() };
  });
  render(
    <TestAppProviders services={services}>
      <ChatRuntimeProvider api={services.api} target={target} host={runtimeHost()}>
        <SessionChatContext compact={vi.fn()} />
      </ChatRuntimeProvider>
    </TestAppProviders>,
  );
  expect(await screen.findByRole("button", { name: "10%" })).toBeInTheDocument();
  const initial = hydration();
  act(() =>
    handlers[0]?.onEvent({
      sequence: 1,
      kind: "hydration",
      payload: {
        ...initial,
        ContextUsage: { UsedTokens: 192000, WindowTokens: 186000, CacheHitPercent: null },
      },
    }),
  );
  expect(await screen.findByRole("button", { name: "103%" })).toBeInTheDocument();
  act(() =>
    handlers[0]?.onEvent({
      sequence: 2,
      kind: "context_usage",
      payload: { UsedTokens: 93000, WindowTokens: 186000, CacheHitPercent: null },
    }),
  );
  expect(await screen.findByRole("button", { name: "50%" })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "50%" }));
  await waitFor(() =>
    expect(
      screen.getByRole("button", {
        name: appI18n.t("chatComposer.context.compact"),
      }),
    ).toBeEnabled(),
  );
  act(() =>
    handlers[0]?.onEvent({
      sequence: 3,
      kind: "session_status",
      payload: {
        ...initial.SessionStatus,
        CompactionMode: "disabled",
      },
    }),
  );
  await waitFor(() =>
    expect(
      screen.getByRole("button", {
        name: appI18n.t("chatComposer.context.compact"),
      }),
    ).toBeDisabled(),
  );
  act(() => {
    handlers[0]?.onEvent({ sequence: 4, kind: "session_status", payload: initial.SessionStatus });
    handlers[0]?.onEvent({
      sequence: 5,
      kind: "runtime_read_model_update",
      payload: {
        Version: { Epoch: "epoch-1", Generation: 1, Sequence: 3 },
        Activity: {
          ...initial.RuntimeReadModelUpdate.Activity,
          ActiveStep: { RunID: "run", StepID: "compact", ActiveKind: "pre_submit_compaction" },
        },
      },
    });
  });
  expect(
    await screen.findByRole("button", { name: appI18n.t("chatComposer.context.compacting") }),
  ).toBeInTheDocument();
  expect(screen.getByRole("button", { name: appI18n.t("chatComposer.context.compact") })).toBeDisabled();
});

it("retains admitted usage during a later Main View load", async () => {
  const services = createTestServices([]);
  const later = deferred<ReturnType<typeof mainViewRead>>();
  vi.spyOn(services.api.chat, "getMainView")
    .mockResolvedValueOnce(mainViewRead())
    .mockReturnValue(later.promise);
  vi.spyOn(services.api.chat, "getTranscriptPage").mockResolvedValue(transcriptPage(null));
  vi.spyOn(services.api.chat, "subscribeTranscript").mockReturnValue({ close: vi.fn() });
  function Context() {
    const owner = useChatRuntimeOwner();
    return (
      <>
        <button
          onClick={() => {
            void owner.forceMainViewRead();
          }}
        >
          Reload
        </button>
        <SessionChatContext compact={vi.fn()} />
      </>
    );
  }
  render(
    <TestAppProviders services={services}>
      <ChatRuntimeProvider api={services.api} target={target} host={runtimeHost()}>
        <Context />
      </ChatRuntimeProvider>
    </TestAppProviders>,
  );
  expect(await screen.findByRole("button", { name: "10%" })).toBeInTheDocument();
  fireEvent.click(screen.getByRole("button", { name: "Reload" }));
  await waitFor(() => {
    expect(services.api.chat.getMainView).toHaveBeenCalledTimes(2);
  });
  expect(screen.getByRole("button", { name: "10%" })).toBeInTheDocument();
  await act(async () => {
    later.resolve(mainViewRead());
  });
});

it("does not mount Session Context before New Chat creation", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("");
  const context = vi.spyOn(services.api.chat, "getContext");
  function Composer() {
    const composer = useChatComposer({
      kind: "new_chat",
      projectID: target.projectID,
      workspace: target.workspace,
    });
    return (
      <ChatComposerSurface composer={composer}>
        <ChatComposer availableHeight={null} onHeightChange={vi.fn()} />
      </ChatComposerSurface>
    );
  }
  render(
    <TestAppProviders services={services}>
      <Composer />
    </TestAppProviders>,
  );
  expect(await screen.findByRole("textbox")).toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "0%" })).not.toBeInTheDocument();
  expect(context).not.toHaveBeenCalled();
});
