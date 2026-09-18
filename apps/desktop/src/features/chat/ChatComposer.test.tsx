import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import type { ChatInputMutationResult, ChatTranscriptHandler, InitialChatSettings } from "@/api";
import { ChatRuntimeProvider, queryKeys } from "@/app-facade";
import { mainViewRead, runtimeHost, transcriptPage } from "@/test-support/chat-runtime";
import { ChatComposerSurface, useComposerSurface } from "./ChatComposerSurface";
import { parseCompactionRequestID, parsePendingWorkItemID } from "@/api";

import { createTestServices } from "@/test-support/app-services";
import { createWorktreeCommand } from "./worktreeCommand";
import { useComposerKeyboard } from "./useComposerKeyboard";
import { appI18n } from "@/i18n";
import { type ComposerSubmission } from "./useChatComposer";
import { useChatComposer, target } from "./chatComposerTestFixture";
import { TestAppProviders, composerWrapper } from "@/test-support/composer";
import { useQueryClient } from "@tanstack/react-query";

import { createChatStorageFixture } from "./chatStorageFixture";
import * as ui from "@/ui";

beforeEach(() => vi.stubGlobal("localStorage", createChatStorageFixture()));
afterEach(() => vi.unstubAllGlobals());

it.each([
  ["older", "prompt B"],
  ["prompt B", "prompt B"],
])("keeps browsing and draft restoration when replacement adds older prompts: %j", async (...entries) => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "draft A", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const { result } = renderHook(() => useChatComposer(target), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  const previous = ["prompt B"];
  await act(async () => {
    await result.current.navigateDraft({ direction: -1, entries: previous });
  });
  act(() => {
    result.current.replaceHistory({ kind: "replace", entries, previous });
  });
  expect(result.current.draftPair).toEqual({ text: "prompt B", protectedInput: "draft A" });
  await act(async () => {
    await result.current.navigateDraft({ direction: 1, entries });
  });
  expect(result.current.draftPair).toEqual({ text: "draft A", protectedInput: null });
});

it("keeps the original draft when a recalled prompt is edited and restores it after clearing", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "draft A", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const { result } = renderHook(() => useChatComposer(target), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  await act(async () => {
    await result.current.navigateDraft({ direction: -1, entries: ["prompt B"] });
  });
  expect(result.current.text).toBe("prompt B");
  act(() => {
    result.current.edit("edited B");
  });
  await act(async () => {
    expect(await result.current.navigateDraft({ direction: -1, entries: ["prompt B"] })).toEqual({
      kind: "blocked",
    });
  });
  expect(result.current.draftPair).toEqual({ text: "edited B", protectedInput: "draft A" });
  act(() => {
    result.current.edit("");
  });
  await act(async () => {
    expect(await result.current.navigateDraft({ direction: 1, entries: ["prompt B"] })).toEqual({
      kind: "restored",
      cursor: "start",
    });
  });
  expect(result.current.draftPair).toEqual({ text: "draft A", protectedInput: null });
});

it.each(["", "unsent draft"])("restores the draft after browsing: %j", async (input) => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input, protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const { result } = renderHook(() => useChatComposer(target), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  const entries = ["old", "new"];
  await act(async () => {
    await result.current.navigateDraft({ direction: -1, entries });
    await result.current.navigateDraft({ direction: -1, entries });
  });
  expect(result.current.text).toBe("old");
  await act(async () => {
    await result.current.navigateDraft({ direction: 1, entries });
    await result.current.navigateDraft({ direction: 1, entries });
  });
  expect(result.current.draftPair).toEqual({ text: input, protectedInput: null });
});

it("reopens both drafts without browsing and saves restoring the protected draft", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "edited B", protectedInput: "draft A" });
  const save = vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const { result } = renderHook(() => useChatComposer(target), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  await act(async () => {
    expect(await result.current.navigateDraft({ direction: 1, entries: ["edited B"] })).toEqual({
      kind: "blocked",
    });
    await result.current.flushDraft();
  });
  expect(save).toHaveBeenLastCalledWith(target, "edited B", "draft A");
  act(() => {
    result.current.edit("");
  });
  await act(async () => {
    await result.current.navigateDraft({ direction: 1, entries: ["edited B"] });
    await result.current.flushDraft();
  });
  expect(save).toHaveBeenLastCalledWith(target, "draft A", null);
});
const accepted: ChatInputMutationResult = {
  sessionID: target.sessionID,
  outcome: {
    kind: "accepted",
    queueItemID: parsePendingWorkItemID("79f762c8-0998-402a-bc80-6b42f3e241ed"),
    diagnostic: null,
  },
};

it("exposes Pending Work read failure and recovers without replaying input or losing draft", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getPromptHistory").mockResolvedValue([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "unsent", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const read = vi
    .spyOn(services.api.chat, "listPendingWork")
    .mockRejectedValueOnce(new Error("unavailable"))
    .mockResolvedValue({ items: [] });
  const send = vi.spyOn(services.api.chat, "steer");
  const notice = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
  const { result } = renderHook(() => useChatComposer({ ...target, submission: { kind: "ready" } }), {
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <TestAppProviders services={services}>{children}</TestAppProviders>
    ),
  });
  await waitFor(() => {
    expect(read).toHaveBeenCalledOnce();
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  expect(notice).not.toHaveBeenCalled();
  expect(result.current.pending.query.isError).toBe(true);
  act(() => {
    result.current.pending.refresh();
  });
  await waitFor(() => {
    expect(result.current.pending.query.isSuccess).toBe(true);
  });
  expect(read).toHaveBeenCalledTimes(2);
  expect(result.current.text).toBe("unsent");
  expect(send).not.toHaveBeenCalled();
  notice.mockRestore();
});

it.each([false, true])(
  "retains the protected draft after sending recalled text (edited: %s)",
  async (edited) => {
    const services = createTestServices([]);
    vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "draft A", protectedInput: null });
    vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
    vi.spyOn(services.api.chat, "steer").mockResolvedValue(accepted);
    const { result } = renderHook(() => useChatComposer({ ...target, submission: { kind: "ready" } }), {
      wrapper: composerWrapper(services),
    });
    await waitFor(() => {
      expect(result.current.draft.kind).toBe("ready");
    });
    await act(async () => {
      await result.current.navigateDraft({ direction: -1, entries: ["prompt B"] });
    });
    act(() => {
      if (edited) result.current.edit("edited B");
      result.current.submit("send");
    });
    await waitFor(() => {
      expect(result.current.inputPending).toBe(false);
    });
    await act(async () => {
      await result.current.navigateDraft({ direction: 1, entries: ["prompt B"] });
    });
    expect(result.current.draftPair).toEqual({ text: "draft A", protectedInput: null });
  },
);

it("loads recall independently and appends accepted trimmed input without rereading", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const read = vi.spyOn(services.api.chat, "getPromptHistory").mockResolvedValue(["older", "newer"]);
  vi.spyOn(services.api.chat, "steer").mockResolvedValue(accepted);
  const { result } = renderHook(() => useChatComposer({ ...target, submission: { kind: "ready" } }), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  await act(async () => {
    await result.current.navigateHistory(-1);
  });
  expect(result.current.text).toBe("newer");
  act(() => {
    result.current.edit("  local prompt \n");
  });
  act(() => {
    result.current.submit("send");
  });
  await waitFor(() => {
    expect(result.current.inputPending).toBe(false);
  });
  await act(async () => {
    await result.current.navigateHistory(-1);
  });
  expect(result.current.text).toBe("local prompt");
  expect(read).toHaveBeenCalledTimes(1);
});

it("detaches edited recall without a protected draft so the edit can be recalled back", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  vi.spyOn(services.api.chat, "getPromptHistory").mockResolvedValue(["old", "new"]);
  const { result } = renderHook(() => useChatComposer(target), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  await act(async () => {
    await result.current.navigateHistory(-1);
  });
  act(() => {
    result.current.edit("edited");
  });
  await act(async () => {
    await result.current.navigateHistory(-1);
  });
  expect(result.current.text).toBe("new");
  await act(async () => {
    await result.current.navigateHistory(1);
  });
  expect(result.current.draftPair).toEqual({ text: "edited", protectedInput: null });
});

it("recalls only at absolute unmodified collapsed boundaries and places the cursor", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "first\nsecond", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  vi.spyOn(services.api.chat, "getPromptHistory").mockResolvedValue(["older\nprompt", "/compact saved"]);
  function Probe() {
    const composer = useChatComposer(target);
    const keyboard = useComposerKeyboard(composer, false, null);
    return (
      <textarea
        data-testid="history-editor"
        value={composer.text}
        onChange={(event) => {
          composer.edit(event.target.value);
        }}
        onKeyDown={keyboard.onEditorKeyDown}
      />
    );
  }
  render(
    <TestAppProviders services={services}>
      <Probe />
    </TestAppProviders>,
  );
  const editor = screen.getByTestId<HTMLTextAreaElement>("history-editor");
  await waitFor(() => expect(editor).toHaveValue("first\nsecond"));
  editor.setSelectionRange(6, 6);
  fireEvent.keyDown(editor, { key: "ArrowUp" });
  expect(editor).toHaveValue("first\nsecond");
  editor.setSelectionRange(0, 1);
  fireEvent.keyDown(editor, { key: "ArrowUp" });
  editor.setSelectionRange(0, 0);
  fireEvent.keyDown(editor, { key: "ArrowUp", ctrlKey: true });
  expect(editor).toHaveValue("first\nsecond");
  fireEvent.keyDown(editor, { key: "ArrowUp" });
  await waitFor(() => expect(editor).toHaveValue("/compact saved"));
  expect(editor.selectionStart).toBe(0);
  fireEvent.keyDown(editor, { key: "ArrowUp" });
  await waitFor(() => expect(editor).toHaveValue("older\nprompt"));
  editor.setSelectionRange(editor.value.length, editor.value.length);
  fireEvent.keyDown(editor, { key: "ArrowDown" });
  await waitFor(() => expect(editor).toHaveValue("/compact saved"));
  expect(editor.selectionStart).toBe(editor.value.length);
  fireEvent.keyDown(editor, { key: "ArrowDown" });
  await waitFor(() => expect(editor).toHaveValue("first\nsecond"));
  expect(editor.selectionStart).toBe(0);
});

it("dispatches the built-in compact command with its exact guidance", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({
    input: "/compact \n preserve decisions",
    protectedInput: null,
  });
  const compact = vi.spyOn(services.api.chat, "compact").mockResolvedValue({
    sessionID: target.sessionID,
    outcome: { kind: "not_accepted", reason: { kind: "too_soon" } },
  });
  const { result } = renderHook(() => useChatComposer({ ...target, submission: { kind: "ready" } }), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.canSubmit).toBe(true);
  });
  await act(async () => {
    result.current.submit("send");
  });
  expect(compact).toHaveBeenCalledWith(target, {
    token: "/compact",
    separatorWhitespace: " \n ",
    rawGuidance: "preserve decisions",
  });
  expect(result.current.text).toBe("/compact \n preserve decisions");
});

it("dispatches independent button compactions without changing the editor draft", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({
    input: "ordinary draft",
    protectedInput: null,
  });
  const compact = vi
    .spyOn(services.api.chat, "compact")
    .mockImplementation(async () => new Promise(() => undefined));
  const { result } = renderHook(() => useChatComposer(target), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  act(() => {
    result.current.compact();
    result.current.compact();
  });
  await waitFor(() => {
    expect(compact).toHaveBeenCalledTimes(2);
  });
  expect(compact).toHaveBeenCalledWith(target, {
    token: "/compact",
    separatorWhitespace: "",
    rawGuidance: "",
  });
  expect(result.current.text).toBe("ordinary draft");
});

it("guards both compact activations against admitted active compaction", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({
    input: "/compact guidance",
    protectedInput: null,
  });
  const compact = vi.spyOn(services.api.chat, "compact");
  const { result } = renderHook(
    () => ({
      composer: useChatComposer({ ...target, submission: { kind: "ready" } }),
      client: useQueryClient(),
    }),
    {
      wrapper: composerWrapper(services),
    },
  );
  await waitFor(() => {
    expect(result.current.composer.canSubmit).toBe(true);
  });
  const view = mainViewRead().mainView;
  result.current.client.setQueryData(queryKeys.chatMainView(target.sessionID), {
    ...view,
    activity: {
      ...view.activity,
      state: "running",
      activeStep: { runID: "run-1", stepID: "step-1", activeKind: "compaction" },
    },
  });
  await act(async () => {
    result.current.composer.submit("send");
    result.current.composer.compact();
  });
  expect(compact).not.toHaveBeenCalled();
  expect(result.current.composer.text).toBe("/compact guidance");
});

it.each(["active", "disabled", "too_soon"] as const)(
  "preserves each source's draft after a typed %s compaction rejection",
  async (kind) => {
    const services = createTestServices([]);
    vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({
      input: "/compact keep decisions",
      protectedInput: null,
    });
    const compact = vi.spyOn(services.api.chat, "compact").mockResolvedValue({
      sessionID: target.sessionID,
      outcome: { kind: "not_accepted", reason: { kind } },
    });
    const { result } = renderHook(() => useChatComposer({ ...target, submission: { kind: "ready" } }), {
      wrapper: composerWrapper(services),
    });
    await waitFor(() => {
      expect(result.current.canSubmit).toBe(true);
    });
    await act(async () => {
      result.current.submit("send");
    });
    expect(result.current.text).toBe("/compact keep decisions");
    act(() => {
      result.current.edit("unrelated draft");
    });
    await act(async () => {
      result.current.compact();
    });
    expect(result.current.text).toBe("unrelated draft");
    expect(compact).toHaveBeenCalledTimes(2);
  },
);

it("delivers a rejected New Chat compaction's Session while preserving its exact draft", async () => {
  const services = createTestServices([]);
  const onDeliveredSession = vi.fn();
  const initialSettings: InitialChatSettings = {
    agentRole: "writer",
    supervisor: "off",
    thinking: null,
    fast: null,
    questionsEnabled: true,
    autoCompactionEnabled: true,
  };
  const rejected = {
    sessionID: target.sessionID,
    outcome: { kind: "not_accepted", reason: { kind: "too_soon" } },
  } as const;
  const compact = vi.spyOn(services.api.chat, "compact").mockResolvedValue(rejected);
  const steer = vi.spyOn(services.api.chat, "steer");
  const { result } = renderHook(
    () =>
      useChatComposer({
        kind: "new_chat",
        projectID: target.projectID,
        workspace: { workspaceID: "workspace-1" },
        submission: { kind: "ready", initialSettings },
        onDeliveredSession,
      }),
    {
      wrapper: composerWrapper(services),
    },
  );
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  act(() => {
    result.current.edit("/compact \n keep decisions");
  });
  await act(async () => {
    result.current.submit("send");
  });
  expect(onDeliveredSession).toHaveBeenCalledWith(rejected);
  expect(result.current.text).toBe("/compact \n keep decisions");
  expect(compact).toHaveBeenCalledExactlyOnceWith(
    {
      kind: "new_chat",
      projectID: target.projectID,
      workspace: { workspaceID: "workspace-1" },
      initialSettings,
    },
    { token: "/compact", separatorWhitespace: " \n ", rawGuidance: "keep decisions" },
  );
  expect(steer).not.toHaveBeenCalled();
});

it.each([
  { newChat: false, ctrlKey: false, text: "/worktree" },
  { newChat: false, ctrlKey: true, text: "/wt status" },
  { newChat: false, ctrlKey: true, text: "/wt ls" },
  { newChat: true, ctrlKey: false, text: "/worktree" },
  { newChat: true, ctrlKey: true, text: "/wt switch topic" },
  { newChat: true, ctrlKey: true, text: "/worktree delete a b" },
])("consumes Worktree commands locally: %j", async ({ newChat, ctrlKey, text }) => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const steer = vi.spyOn(services.api.chat, "steer");
  const queue = vi.spyOn(services.api.chat, "queue");
  const delivered = vi.fn();
  let finish!: () => void;
  const execute = vi.fn(
    async () =>
      new Promise<void>((resolve) => {
        finish = resolve;
      }),
  );
  const push = vi.fn();
  const commands = [createWorktreeCommand({ execute, push, t: appI18n.t })];
  const options = {
    ...(newChat
      ? {
          kind: "new_chat" as const,
          projectID: target.projectID,
          workspace: { workspaceID: "workspace-1" },
          submission: {
            kind: "ready" as const,
            initialSettings: {
              agentRole: "writer",
              supervisor: "off" as const,
              thinking: null,
              fast: null,
              questionsEnabled: true,
              autoCompactionEnabled: false,
            },
          },
        }
      : { ...target, submission: { kind: "ready" as const } }),
    commands,
    onDeliveredSession: delivered,
  };
  function Probe() {
    const composer = useChatComposer(options);
    const keyboard = useComposerKeyboard(composer, false, null);
    return (
      <textarea
        data-testid="command"
        disabled={composer.draft.kind !== "ready"}
        value={composer.text}
        onChange={(event) => {
          composer.edit(event.target.value);
        }}
        onKeyDown={keyboard.onEditorKeyDown}
      />
    );
  }
  render(
    <TestAppProviders services={services}>
      <Probe />
    </TestAppProviders>,
  );
  const editor = screen.getByTestId("command");
  await waitFor(() => expect(editor).not.toBeDisabled());
  fireEvent.change(editor, { target: { value: text } });
  fireEvent.keyDown(editor, { key: "Enter", ctrlKey });
  await waitFor(() => expect(editor).toHaveValue(""));
  if (!newChat && text !== "/wt ls") {
    expect(execute).toHaveBeenCalledOnce();
    await act(async () => {
      finish();
    });
    expect(push).not.toHaveBeenCalled();
  } else {
    expect(execute).not.toHaveBeenCalled();
    expect(push).toHaveBeenCalledOnce();
  }
  expect(steer).not.toHaveBeenCalled();
  expect(queue).not.toHaveBeenCalled();
  expect(delivered).not.toHaveBeenCalled();
  expect(editor).toHaveValue("");
});

it.each(["idle", "stopped", "failed"] as const)(
  "resynchronizes activity only after a successful Stop: %s",
  async (result) => {
    const report = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
    const services = createTestServices([], undefined, { platform: "linux" });
    vi.spyOn(services.api.chat, "subscribeTranscript").mockReturnValue({ close: () => undefined });
    const idle = mainViewRead();
    const read = vi.spyOn(services.api.chat, "getMainView").mockResolvedValue({
      ...idle,
      mainView: {
        ...idle.mainView,
        activity: {
          ...idle.mainView.activity,
          state: "running",
          activeStep: { runID: "run-1", stepID: "step-1", activeKind: "user_turn" },
        },
      },
    });
    vi.spyOn(services.api.chat, "getTranscriptPage").mockResolvedValue(transcriptPage(null));
    vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "", protectedInput: null });
    const stop = vi.spyOn(services.api.chat, "stop").mockImplementation(async () => {
      if (result === "failed") throw new Error("Stop request failed");
      read.mockResolvedValue(mainViewRead(2));
      return result;
    });
    function Probe() {
      const { composer, stoppable } = useComposerSurface();
      return (
        <button
          disabled={!stoppable}
          onClick={() => {
            composer.pending.stop();
          }}
        >
          Stop probe
        </button>
      );
    }
    function Composer() {
      const composer = useChatComposer(target);
      return (
        <ChatComposerSurface composer={composer}>
          <Probe />
        </ChatComposerSurface>
      );
    }
    render(
      <TestAppProviders services={services}>
        <ChatRuntimeProvider api={services.api} target={target} host={runtimeHost()}>
          <Composer />
        </ChatRuntimeProvider>
      </TestAppProviders>,
    );
    const button = screen.getByRole("button", { name: "Stop probe" });
    await waitFor(() => expect(button).not.toBeDisabled());
    fireEvent.click(button);
    await waitFor(() => {
      expect(stop).toHaveBeenCalledOnce();
    });
    if (result === "failed") {
      await waitFor(() => {
        expect(report).toHaveBeenCalled();
      });
      expect(read).toHaveBeenCalledTimes(1);
      expect(button).not.toBeDisabled();
      return;
    }
    await waitFor(() => {
      expect(read).toHaveBeenCalledTimes(2);
    });
    await waitFor(() => expect(button).toBeDisabled());
  },
);

it("clears an armed Stop on transcript loss while allowing a subsequent independent Stop", async () => {
  const services = createTestServices([], undefined, { platform: "linux" });
  const mainView = mainViewRead();
  vi.spyOn(services.api.chat, "getMainView").mockResolvedValue({
    ...mainView,
    mainView: {
      ...mainView.mainView,
      activity: {
        ...mainView.mainView.activity,
        state: "running",
        activeStep: { runID: "run-1", stepID: "step-1", activeKind: "user_turn" },
      },
    },
  });
  vi.spyOn(services.api.chat, "getTranscriptPage").mockResolvedValue(transcriptPage(null));
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "", protectedInput: null });
  const handlers: ChatTranscriptHandler[] = [];
  vi.spyOn(services.api.chat, "subscribeTranscript").mockImplementation((_target, handler) => {
    handlers.push(handler);
    return { close: () => undefined };
  });
  const stop = vi.spyOn(services.api.chat, "stop").mockResolvedValue("stopped");
  function Probe() {
    const { stoppable } = useComposerSurface();
    return (
      <button data-testid="keyboard" disabled={!stoppable}>
        Keyboard
      </button>
    );
  }
  function Composer() {
    const composer = useChatComposer(target);
    return (
      <ChatComposerSurface composer={composer}>
        <Probe />
      </ChatComposerSurface>
    );
  }
  const view = render(
    <TestAppProviders services={services}>
      <ChatRuntimeProvider api={services.api} target={target} host={runtimeHost()}>
        <Composer />
      </ChatRuntimeProvider>
    </TestAppProviders>,
  );
  const keyboard = screen.getByTestId("keyboard");
  await waitFor(() => expect(keyboard).not.toBeDisabled());
  fireEvent.keyDown(keyboard, { key: "Escape" });
  act(() => handlers[0]?.onTransportLoss?.());
  fireEvent.keyDown(keyboard, { key: "Escape" });
  await act(async () => {
    await Promise.resolve();
  });
  expect(stop).not.toHaveBeenCalled();
  fireEvent.keyDown(keyboard, { key: "Escape" });
  await waitFor(() => {
    expect(stop).toHaveBeenCalledOnce();
  });
  expect(handlers).toHaveLength(1);
  view.unmount();
});

it("persists subsequent Session edits through their ordinary independent request", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "saved", protectedInput: null });
  const persist = vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const { result } = renderHook(() => useChatComposer({ ...target }), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  vi.useFakeTimers();
  try {
    await act(async () => {
      await vi.advanceTimersByTimeAsync(350);
    });
    persist.mockClear();
    act(() => {
      result.current.edit("offline edits");
    });
    await act(async () => {
      await vi.advanceTimersByTimeAsync(350);
    });
    expect(persist).toHaveBeenCalledOnce();
    expect(result.current.text).toBe("offline edits");
  } finally {
    vi.useRealTimers();
  }
});

it("clears on activation and preserves later typing after acceptance", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "submitted", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  let deliver!: (value: ChatInputMutationResult) => void;
  const steer = vi.spyOn(services.api.chat, "steer").mockReturnValue(
    new Promise((resolve) => {
      deliver = resolve;
    }),
  );
  const readPending = vi.spyOn(services.api.chat, "listPendingWork").mockResolvedValue({ items: [] });
  const { result } = renderHook(
    () =>
      useChatComposer({
        ...target,
        submission: { kind: "ready" },
      }),
    {
      wrapper: composerWrapper(services),
    },
  );
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  act(() => {
    result.current.submit("send");
  });
  expect(result.current.text).toBe("");
  await waitFor(() => {
    expect(steer).toHaveBeenCalledWith(target, { kind: "text", text: "submitted" });
  });
  act(() => {
    result.current.edit("later");
  });
  await act(async () => {
    deliver(accepted);
  });
  expect(result.current.text).toBe("later");
  expect(readPending).toHaveBeenLastCalledWith({ ...target, sessionID: accepted.sessionID });
});

it("appends independently rejected Queue inputs after text typed during delivery", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "first\n ", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const deliveries: ((value: ChatInputMutationResult) => void)[] = [];
  vi.spyOn(services.api.chat, "queue").mockImplementation(
    async () => new Promise((resolve) => deliveries.push(resolve)),
  );
  const { result } = renderHook(() => useChatComposer({ ...target, submission: { kind: "ready" } }), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  act(() => {
    result.current.submit("queue");
  });
  act(() => {
    result.current.edit("second");
  });
  act(() => {
    result.current.submit("queue");
  });
  act(() => {
    result.current.edit("later");
  });
  const rejected: ChatInputMutationResult = {
    sessionID: target.sessionID,
    outcome: { kind: "not_accepted", reason: { kind: "pending_work_capacity" } },
  };
  await waitFor(() => {
    expect(deliveries).toHaveLength(2);
  });
  await act(async () => deliveries[1]?.(rejected));
  await act(async () => deliveries[0]?.(rejected));
  expect(result.current.text).toBe("later\nsecond\nfirst\n ");
  expect(result.current.inputPending).toBe(false);
});

it("restores a failed request but does not restore an accepted input with a diagnostic", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "input", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  vi.spyOn(services.api.chat, "steer")
    .mockRejectedValueOnce(new Error("delivery failed"))
    .mockResolvedValueOnce({
      ...accepted,
      outcome: {
        ...accepted.outcome,
        kind: "accepted",
        queueItemID: parsePendingWorkItemID("79f762c8-0998-402a-bc80-6b42f3e241ed"),
        diagnostic: { kind: "prompt_history_failure", operation: null, cause: null },
      },
    });
  const { result } = renderHook(() => useChatComposer({ ...target, submission: { kind: "ready" } }), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  await act(async () => {
    result.current.submit("send");
  });
  expect(result.current.text).toBe("input");
  await act(async () => {
    result.current.submit("send");
  });
  expect(result.current.text).toBe("");
});

it("keeps New Chat typing while Settings load and delivers the identified Session even on rejection", async () => {
  const services = createTestServices([]);
  const newChat = {
    kind: "new_chat",
    projectID: target.projectID,
    workspace: { workspaceID: "workspace-1" },
  } as const;
  const settings: InitialChatSettings = {
    agentRole: "writer",
    supervisor: "off",
    thinking: null,
    fast: null,
    questionsEnabled: true,
    autoCompactionEnabled: false,
  };
  const rejected: ChatInputMutationResult = {
    sessionID: target.sessionID,
    outcome: { kind: "not_accepted", reason: { kind: "runtime_unavailable" } },
  };
  const steer = vi.spyOn(services.api.chat, "steer").mockResolvedValue(rejected);
  const onDeliveredSession = vi.fn();
  const { result, rerender } = renderHook(
    ({ submission }: { submission: ComposerSubmission<"new_chat"> }) =>
      useChatComposer({
        ...newChat,
        submission,
        onDeliveredSession,
      }),
    {
      initialProps: { submission: { kind: "loading" } },
      wrapper: composerWrapper(services),
    },
  );
  act(() => {
    result.current.edit("typed before settings");
  });
  await act(async () => {
    result.current.submit("send");
  });
  expect(steer).not.toHaveBeenCalled();
  rerender({ submission: { kind: "ready", initialSettings: settings } });
  await act(async () => {
    result.current.submit("send");
  });
  expect(steer).toHaveBeenCalledWith(
    { ...newChat, initialSettings: settings },
    { kind: "text", text: "typed before settings" },
  );
  expect(result.current.text).toBe("typed before settings");
  expect(onDeliveredSession).toHaveBeenCalledWith(rejected);
  const readPending = vi.spyOn(services.api.chat, "listPendingWork").mockResolvedValue({
    items: [
      {
        id: parsePendingWorkItemID("79f762c8-0998-402a-bc80-6b42f3e241ed"),
        kind: "message",
        lane: "queue",
        state: "pending",
        canonicalInput: "created Session work",
        message: { text: "created Session work" },
      },
    ],
  });
  steer.mockResolvedValue(accepted);
  await act(async () => {
    result.current.submit("send");
  });
  expect(readPending).not.toHaveBeenCalled();
  expect(result.current.target).toEqual(newChat);
  expect(result.current.pending.items).toEqual([]);
});

it.each(["send", "queue"] as const)(
  "completes a selected prompt command and dispatches it through %s",
  async (intent) => {
    const services = createTestServices([]);
    vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "/rev", protectedInput: null });
    vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
    const queue = vi
      .spyOn(services.api.chat, intent === "send" ? "steer" : "queue")
      .mockResolvedValue(accepted);
    const { result } = renderHook(
      () =>
        useChatComposer({
          ...target,
          submission: { kind: "ready" },
          commands: [
            {
              token: "/review",
              aliases: ["/r"],
              description: null,
              preview: null,
              execution: { kind: "prompt", catalogIdentity: "prompt:review" },
            },
          ],
        }),
      {
        wrapper: composerWrapper(services),
      },
    );
    await waitFor(() => {
      expect(result.current.draft.kind).toBe("ready");
    });
    await act(async () => {
      result.current.submit(intent);
    });
    expect(queue).toHaveBeenCalledWith(target, {
      kind: "command",
      catalogIdentity: "prompt:review",
      token: "/review",
      separatorWhitespace: "",
      arguments: "",
    });
  },
);

it.each([true, false])(
  "dispatches a direct command with Queue support %s without a frontend queue",
  async (supportsQueue) => {
    const services = createTestServices([]);
    vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({
      input: "/hidden\t raw\n ",
      protectedInput: null,
    });
    vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
    const send = vi.fn().mockResolvedValue({ kind: "local" });
    const queue = vi.fn().mockResolvedValue({ kind: "local" });
    const { result } = renderHook(
      () =>
        useChatComposer({
          ...target,
          submission: { kind: "ready" },
          commands: [
            {
              token: "/direct",
              aliases: ["/hidden"],
              description: null,
              preview: null,
              execution: supportsQueue ? { kind: "direct", send, queue } : { kind: "direct", send },
            },
          ],
        }),
      {
        wrapper: composerWrapper(services),
      },
    );
    await waitFor(() => {
      expect(result.current.draft.kind).toBe("ready");
    });
    await act(async () => {
      result.current.submit("queue");
    });
    expect(supportsQueue ? queue : send).toHaveBeenCalledWith(target, {
      token: "/hidden",
      separatorWhitespace: "\t ",
      arguments: "raw\n ",
    });
    expect(supportsQueue ? send : queue).not.toHaveBeenCalled();
    expect(result.current.text).toBe("");
  },
);

it("prepends live interrupted messages in event order and restores Discard only after success", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "draft", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  vi.spyOn(services.api.chat, "listPendingWork").mockResolvedValue({ items: [] });
  const removal = vi
    .spyOn(services.api.chat, "removePendingWork")
    .mockRejectedValueOnce(new Error("not removed"))
    .mockResolvedValueOnce({
      kind: "manual_compaction",
      canonicalInput: "/compact guidance",
    });
  const { result } = renderHook(() => useChatComposer({ ...target }), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  act(() =>
    result.current.pending.observation.onHumanInputInterrupted?.([
      { QueueItemID: "first", Text: "one\n " },
      { QueueItemID: "second", Text: "two" },
    ]),
  );
  expect(result.current.text).toBe("one\n \ntwo\ndraft");
  const item = parsePendingWorkItemID("79f762c8-0998-402a-bc80-6b42f3e241ed");
  await act(async () => {
    result.current.pending.discard(item);
  });
  expect(result.current.text).toBe("one\n \ntwo\ndraft");
  await act(async () => {
    result.current.pending.discard(item);
  });
  expect(removal).toHaveBeenCalledTimes(2);
  expect(result.current.text).toBe("one\n \ntwo\ndraft\n/compact guidance");
  act(() =>
    result.current.pending.observation.onPendingWorkRestored?.({
      Restoration: {
        ItemID: item.toJSONValue(),
        Kind: "manual_compaction",
        CanonicalInput: "/compact restored",
      },
    }),
  );
  expect(result.current.text).toBe("/compact restored\none\n \ntwo\ndraft\n/compact guidance");
});

it("keeps repeated compactions distinct and restores only the discarded canonical command", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "draft", protectedInput: null });
  const first = parseCompactionRequestID("79f762c8-0998-402a-bc80-6b42f3e241ed");
  const second = parseCompactionRequestID("99f762c8-0998-402a-bc80-6b42f3e241ed");
  const items = [first, second].map((id) => ({
    id,
    kind: "manual_compaction" as const,
    state: "pending" as const,
    lane: "steer" as const,
    canonicalInput: "/compact",
    manualCompaction: { guidance: null },
  }));
  const read = vi.spyOn(services.api.chat, "listPendingWork").mockResolvedValue({ items });
  vi.spyOn(services.api.chat, "stop").mockResolvedValue("stopped");
  const remove = vi.spyOn(services.api.chat, "removePendingWork").mockImplementation(async () => {
    read.mockResolvedValue({ items: items.slice(1) });
    return { kind: "manual_compaction", canonicalInput: "/compact" };
  });
  const { result } = renderHook(() => useChatComposer({ ...target }), {
    wrapper: composerWrapper(services),
  });
  await waitFor(() => {
    expect(result.current.pending.items).toHaveLength(2);
  });
  await act(async () => {
    result.current.pending.stop();
  });
  expect(result.current.pending.items).toEqual(items);
  expect(result.current.text).toBe("draft");
  await act(async () => {
    result.current.pending.discard(first);
  });
  await waitFor(() => {
    expect(result.current.pending.items).toEqual(items.slice(1));
  });
  expect(remove).toHaveBeenCalledWith(target, first);
  expect(result.current.text).toBe("draft\n/compact");
});
