import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { useLayoutEffect, useState, type ReactNode } from "react";
import * as Atom from "effect/unstable/reactivity/Atom";
import { RegistryProvider, useAtomSet } from "@effect/atom-react";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import type {
  ChatInputMutationResult,
  ChatTranscriptHandler,
  InitialChatSettings,
  ChatSettingsTarget,
} from "@/api";
import { ChatRuntimeProvider, useAppServices } from "@/app-facade";
import { createChatComposerViewModel } from "./ChatComposerViewModel";
import { mainViewRead, runtimeHost, transcriptPage } from "@/test-support/chat-runtime";
import { ChatComposerSurface, useComposerSurface } from "./ChatComposerSurface";
import { parsePendingWorkItemID } from "@/api";

import {
  createTestServices,
  TestAppProviders as AppProviders,
  type TestAppServices,
} from "@/test-support/app-services";
import { createWorktreeCommand } from "./worktreeCommand";
import { useComposerKeyboard } from "./useComposerKeyboard";
import { appI18n } from "@/i18n";
import {
  useChatComposer as useComposer,
  type ChatComposerOptions,
  type ComposerSubmission,
} from "./useChatComposer";

import { createChatStorageFixture } from "./chatStorageFixture";

beforeEach(() => vi.stubGlobal("localStorage", createChatStorageFixture()));
afterEach(() => vi.unstubAllGlobals());

function useChatComposer(
  options: ChatSettingsTarget & Omit<ChatComposerOptions, "model"> & { submission?: ComposerSubmission },
) {
  const services = useAppServices();
  const client = useQueryClient();
  const { t } = useTranslation();
  const [target] = useState(() =>
    Atom.make<ChatSettingsTarget>(
      options.kind === "session"
        ? { kind: "session", projectID: options.projectID, sessionID: options.sessionID }
        : { kind: "new_chat", projectID: options.projectID, workspace: options.workspace },
    ),
  );
  const [submission] = useState(() =>
    Atom.make<ComposerSubmission>(options.submission ?? { kind: "loading" }),
  );
  const setSubmission = useAtomSet(submission);
  const state = options.submission?.kind ?? "loading";
  const settings =
    options.submission?.kind === "ready" && "initialSettings" in options.submission
      ? options.submission.initialSettings
      : null;
  const error = options.submission?.kind === "failed" ? options.submission.error : null;
  useLayoutEffect(() => {
    setSubmission(
      state === "ready"
        ? settings === null
          ? { kind: "ready" }
          : { kind: "ready", initialSettings: settings }
        : state === "failed"
          ? { kind: "failed", error }
          : { kind: "loading" },
    );
  }, [state, settings, error, setSubmission]);
  const [model] = useState(() =>
    createChatComposerViewModel({ services, client, t, target, submission, opening: options }),
  );
  return useComposer({ ...options, model });
}

function TestAppProviders({
  children,
  services,
}: Readonly<{ children: ReactNode; services: TestAppServices }>) {
  return (
    <RegistryProvider>
      <AppProviders services={services}>{children}</AppProviders>
    </RegistryProvider>
  );
}

const target = {
  kind: "session",
  projectID: "project-1",
  sessionID: "session-1",
} as const;
const accepted: ChatInputMutationResult = {
  sessionID: target.sessionID,
  outcome: {
    kind: "accepted",
    queueItemID: parsePendingWorkItemID("79f762c8-0998-402a-bc80-6b42f3e241ed"),
    diagnostic: null,
  },
};

it.each([
  { newChat: false, ctrlKey: false, text: "/worktree" },
  { newChat: false, ctrlKey: true, text: "/wt status" },
  { newChat: false, ctrlKey: true, text: "/wt ls" },
  { newChat: true, ctrlKey: false, text: "/worktree" },
  { newChat: true, ctrlKey: true, text: "/wt switch topic" },
  { newChat: true, ctrlKey: true, text: "/worktree delete a b" },
])("consumes Worktree commands locally: %j", async ({ newChat, ctrlKey, text }) => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("");
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
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("");
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
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("saved");
  const persist = vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const { result } = renderHook(() => useChatComposer({ ...target }), {
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <TestAppProviders services={services}>{children}</TestAppProviders>
    ),
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
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("submitted");
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
      wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
        <TestAppProviders services={services}>{children}</TestAppProviders>
      ),
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
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("first\n ");
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const deliveries: ((value: ChatInputMutationResult) => void)[] = [];
  vi.spyOn(services.api.chat, "queue").mockImplementation(
    async () => new Promise((resolve) => deliveries.push(resolve)),
  );
  const { result } = renderHook(() => useChatComposer({ ...target, submission: { kind: "ready" } }), {
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <TestAppProviders services={services}>{children}</TestAppProviders>
    ),
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
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("input");
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
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <TestAppProviders services={services}>{children}</TestAppProviders>
    ),
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
      wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
        <TestAppProviders services={services}>{children}</TestAppProviders>
      ),
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
    vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("/rev");
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
        wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
          <TestAppProviders services={services}>{children}</TestAppProviders>
        ),
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
    vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("/hidden\t raw\n ");
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
        wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
          <TestAppProviders services={services}>{children}</TestAppProviders>
        ),
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

it("places a late saved draft before typing without losing exact whitespace", async () => {
  const services = createTestServices([]);
  let deliver!: (text: string) => void;
  vi.spyOn(services.api.chat, "getDraft").mockReturnValue(
    new Promise<string>((resolve) => {
      deliver = resolve;
    }),
  );
  const save = vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const { result } = renderHook(() => useChatComposer({ ...target }), {
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <TestAppProviders services={services}>{children}</TestAppProviders>
    ),
  });
  act(() => {
    result.current.edit(" new\ntext ");
  });
  expect(result.current.draft.kind).toBe("loading");
  expect(save).not.toHaveBeenCalled();
  await act(async () => {
    deliver(" saved\n ");
  });
  await waitFor(() => {
    expect(result.current.text).toBe(" saved\n \n new\ntext ");
  });
  expect(result.current.draft.kind).toBe("ready");
});

it("restores exact text in either direction and does not add separators for empty input", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("");
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const { result } = renderHook(() => useChatComposer({ ...target }), {
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <TestAppProviders services={services}>{children}</TestAppProviders>
    ),
  });
  await waitFor(() => {
    expect(result.current.draft.kind).toBe("ready");
  });
  act(() => {
    result.current.restore(" \nfirst ", "append");
  });
  expect(result.current.text).toBe(" \nfirst ");
  act(() => {
    result.current.restore("", "prepend");
  });
  expect(result.current.text).toBe(" \nfirst ");
  act(() => {
    result.current.restore("last\n ", "append");
  });
  expect(result.current.text).toBe(" \nfirst \nlast\n ");
  act(() => {
    result.current.restore("before", "prepend");
  });
  expect(result.current.text).toBe("before\n \nfirst \nlast\n ");
});

it("prepends live interrupted messages in event order and restores Discard only after success", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue("draft");
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
    wrapper: ({ children }: Readonly<{ children: ReactNode }>) => (
      <TestAppProviders services={services}>{children}</TestAppProviders>
    ),
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
