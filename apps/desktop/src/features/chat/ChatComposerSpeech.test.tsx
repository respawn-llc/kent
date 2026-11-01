import { afterEach, beforeEach, expect, it, vi } from "bun:test";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { createTestServices } from "@/test-support/app-services";
import { TestAppProviders } from "@/test-support/composer";
import {
  deferred,
  hydration,
  mainViewRead,
  runtimeHost,
  transcriptPage,
  target as runtimeTarget,
  transcriptSource,
  type ChatTranscriptHandler,
} from "@/test-support/chat-runtime";
import { approval, question } from "@/test-support/chat-prompts";
import { ChatPromptPresenceProvider, ChatRuntimeProvider } from "@/app-facade";
import { installTestMicrophone } from "@/test-support/microphone";
import { appI18n } from "@/i18n";
import type { PendingPrompt, SpeechResult } from "@/api";
import { ChatComposerSurface, useComposerSurface } from "./ChatComposerSurface";
import { ComposerSendButton } from "./ComposerSendButton";
import { ChatPromptPicker } from "./ChatPromptPicker";
import { useChatComposer } from "./chatComposerTestFixture";
import { installChatStorageFixture } from "./chatStorageFixture";

let microphone: ReturnType<typeof installTestMicrophone> | undefined;
let restoreStorage: (() => void) | undefined;
let animate: PropertyDescriptor | undefined;
beforeEach(() => {
  // Exercise submission rather than Happy DOM's incomplete WAAPI cancellation.
  animate = Object.getOwnPropertyDescriptor(Element.prototype, "animate");
  Object.defineProperty(Element.prototype, "animate", { configurable: true, value: undefined });
});
afterEach(() => {
  microphone?.restore();
  restoreStorage?.();
  vi.restoreAllMocks();
  if (animate === undefined) Reflect.deleteProperty(Element.prototype, "animate");
  else Object.defineProperty(Element.prototype, "animate", animate);
});

function Editor() {
  const { composer, speech, promptPicker, onEditorKeyDown } = useComposerSurface();
  return (
    <>
      {promptPicker?.state.current != null ? (
        <ChatPromptPicker picker={promptPicker} />
      ) : (
        <textarea
          value={composer.text}
          onChange={(event) => {
            composer.edit(event.target.value);
          }}
          onKeyDown={onEditorKeyDown}
        />
      )}
      {speech.control}
      <ComposerSendButton />
    </>
  );
}

function Composer() {
  const composer = useChatComposer({ ...runtimeTarget, kind: "session", submission: { kind: "ready" } });
  return (
    <ChatComposerSurface composer={composer}>
      <Editor />
    </ChatComposerSurface>
  );
}

it.each([
  { platform: "macos", key: "Meta", modifier: "metaKey" },
  { platform: "windows", key: "Control", modifier: "ctrlKey" },
  { platform: "linux", key: "Control", modifier: "ctrlKey" },
] as const)(
  "queues from Send with the platform modifier and resets on release or blur: %j",
  async ({ platform, key, modifier }) => {
    const { queue, steer } = await setupSpeech("queued text", undefined, platform);
    const sendLabel = appI18n.t("chatComposer.send");
    const queueLabel = appI18n.t("chatComposer.queue");
    fireEvent.keyDown(window, { key, [modifier]: true });
    const button = screen.getByRole("button", { name: queueLabel });
    fireEvent.keyUp(window, { key });
    expect(screen.getByRole("button", { name: sendLabel })).toBeEnabled();
    fireEvent.keyDown(window, { key, [modifier]: true });
    fireEvent.blur(window);
    expect(screen.getByRole("button", { name: sendLabel })).toBeEnabled();
    fireEvent.keyDown(window, { key, [modifier]: true });
    fireEvent.click(button, { [modifier]: true });
    await waitFor(() => {
      expect(queue).toHaveBeenCalledTimes(1);
    });
    expect(steer).not.toHaveBeenCalled();
  },
);

async function setupSpeech(
  text: string,
  prompt?: PendingPrompt,
  platform: "macos" | "windows" | "linux" = "macos",
) {
  restoreStorage = installChatStorageFixture();
  microphone = installTestMicrophone();
  const services = createTestServices([], undefined, { platform });
  vi.spyOn(services.api.chat, "getMainView").mockResolvedValue(mainViewRead());
  vi.spyOn(services.api.chat, "getTranscriptPage").mockResolvedValue(transcriptPage(null));
  const handlers: ChatTranscriptHandler[] = [];
  vi.spyOn(services.api.chat, "subscribeTranscript").mockImplementation(() =>
    transcriptSource((handler) => {
      handlers.push(handler);
      return { close: vi.fn() };
    }),
  );
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: text, protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  vi.spyOn(services.api, "speechAvailability").mockResolvedValue({
    uploadUrl: "/speech",
    encodings: [{ mimeType: "audio/webm", extension: "webm" }],
    maxBytes: 25_000_000,
    maxDurationSeconds: 600,
  });
  const transcript = deferred<SpeechResult>();
  const readTranscript = vi.fn(async () => transcript.promise);
  vi.spyOn(services.speech, "begin").mockResolvedValue({
    append: async () => ({ bytes: 2000, limitReached: false }),
    finalize: async () => ({
      kind: "native",
      original: { path: "/temporary/voice.webm", mimeType: "audio/webm", bytes: 2000 },
      durationMs: 2000,
    }),
    result: readTranscript,
    cancel: async () => undefined,
  });
  vi.spyOn(services.speech, "discard").mockResolvedValue();
  const steer = vi.spyOn(services.api.chat, "steer").mockRejectedValue(new Error("send failed"));
  const queue = vi.spyOn(services.api.chat, "queue").mockRejectedValue(new Error("send failed"));
  const answer = vi
    .spyOn(services.api.chat, "answerPromptBatch")
    .mockRejectedValue(new Error("answer failed"));
  render(
    <TestAppProviders services={services}>
      <ChatPromptPresenceProvider>
        <ChatRuntimeProvider api={services.api} target={runtimeTarget} host={runtimeHost()}>
          <Composer />
        </ChatRuntimeProvider>
      </ChatPromptPresenceProvider>
    </TestAppProviders>,
  );
  const editor = await screen.findByRole("textbox");
  if (prompt !== undefined) {
    await waitFor(() => {
      expect(handlers.length).toBeGreaterThan(0);
    });
    act(() => {
      handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
      handlers[0]?.onEvent({ sequence: 2, kind: "prompt", payload: { state: "pending", prompt } });
    });
    await screen.findByRole("radiogroup");
  }
  return { editor, services, transcript, readTranscript, steer, queue, answer, handlers };
}

it.each([question(), approval()])(
  "inserts into the visible prompt when it arrives while the ordinary composer is recording: %s",
  async (prompt) => {
    const { handlers, transcript } = await setupSpeech("ordinary draft");
    fireEvent.click(await screen.findByRole("button", { name: appI18n.t("speech.record") }));
    await screen.findByRole("button", { name: appI18n.t("speech.stop") });
    act(() => {
      handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
      handlers[0]?.onEvent({ sequence: 2, kind: "prompt", payload: { state: "pending", prompt } });
    });
    await screen.findByRole("radiogroup");
    fireEvent.click(screen.getByRole("button", { name: appI18n.t("speech.stop") }));
    await act(async () => {
      transcript.resolve({ kind: "transcript", text: "visible commentary" });
    });
    await waitFor(() => {
      expect(screen.getByRole("textbox")).toHaveValue("visible commentary");
    });
  },
);

it("inserts Retry text into the visible Question instead of the original hidden composer", async () => {
  const { handlers, services, transcript, readTranscript } = await setupSpeech("ordinary draft");
  const retry = vi.spyOn(services.speech, "retry").mockResolvedValue({
    result: async () => ({ kind: "transcript", text: "retried commentary" }),
    cancel: async () => undefined,
  });
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("speech.record") }));
  await screen.findByRole("button", { name: appI18n.t("speech.stop") });
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("speech.stop") }));
  await waitFor(() => {
    expect(readTranscript).toHaveBeenCalled();
  });
  await act(async () => {
    transcript.reject(new Error("transcription failed"));
  });
  await screen.findByRole("dialog");
  await act(async () => {
    handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
    handlers[0]?.onEvent({ sequence: 2, kind: "prompt", payload: { state: "pending", prompt: question() } });
  });
  await userEvent.setup().click(screen.getByRole("button", { name: appI18n.t("speech.retry") }));
  await waitFor(() => {
    expect(retry).toHaveBeenCalledTimes(1);
  });
  await waitFor(() => {
    expect(screen.queryAllByRole("dialog").length).toBe(0);
  });
  expect(retry).toHaveBeenCalledTimes(1);
  expect(screen.getByRole("textbox")).toHaveValue("retried commentary");
});

it("leaves a newly visible Question's transcript for review after ordinary recording Send", async () => {
  const { handlers, transcript, steer, queue, answer, readTranscript } = await setupSpeech("ordinary draft");
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("speech.record") }));
  await screen.findByRole("button", { name: appI18n.t("speech.stop") });
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chatComposer.send") }));
  await waitFor(() => {
    expect(readTranscript).toHaveBeenCalled();
  });
  act(() => {
    handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
    handlers[0]?.onEvent({ sequence: 2, kind: "prompt", payload: { state: "pending", prompt: question() } });
  });
  await screen.findByRole("radiogroup");
  await act(async () => {
    transcript.resolve({ kind: "transcript", text: "new commentary" });
  });
  await waitFor(() => {
    expect(screen.getByRole("textbox")).toHaveValue("new commentary");
  });
  expect(steer).not.toHaveBeenCalled();
  expect(queue).not.toHaveBeenCalled();
  expect(answer).not.toHaveBeenCalled();
});

it("delivers to the Question selected during recording rather than the Question where it began", async () => {
  const { handlers, transcript } = await setupSpeech("ordinary draft", question());
  await act(async () => {
    handlers[0]?.onEvent({
      sequence: 3,
      kind: "prompt",
      payload: { state: "pending", prompt: question("question-2") },
    });
  });
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("speech.record") }));
  await screen.findByRole("button", { name: appI18n.t("speech.stop") });
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chat.picker.next") }));
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("speech.stop") }));
  await act(async () => {
    transcript.resolve({ kind: "transcript", text: "second answer" });
  });
  await waitFor(() => {
    expect(screen.getByRole("textbox")).toHaveValue("second answer");
  });
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chat.picker.previous") }));
  await waitFor(() => {
    expect(screen.getByRole("textbox")).toHaveValue("");
  });
});

it("inserts into the ordinary composer when the recorded Question disappears during transcription", async () => {
  const prompt = question();
  const { handlers, transcript, readTranscript, steer, answer } = await setupSpeech("ordinary draft", prompt);
  const choice = screen.getAllByRole("radio").at(0);
  if (choice === undefined) throw new Error("Question has no answer choices");
  fireEvent.click(choice);
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("speech.record") }));
  await screen.findByRole("button", { name: appI18n.t("speech.stop") });
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chatComposer.send") }));
  await waitFor(() => {
    expect(readTranscript).toHaveBeenCalled();
  });
  await act(async () => {
    handlers[0]?.onEvent({
      sequence: 3,
      kind: "prompt",
      payload: { state: "resolved", toolCallID: prompt.toolCallID },
    });
  });
  await waitFor(() => {
    expect(screen.queryByRole("radiogroup")).toBeNull();
  });
  await act(async () => {
    transcript.resolve({ kind: "transcript", text: "back in composer" });
  });
  await waitFor(() => {
    expect(screen.getByRole("textbox")).toHaveValue("ordinary draft\n\nback in composer");
  });
  expect(answer).not.toHaveBeenCalled();
  expect(steer).not.toHaveBeenCalled();
});

it.each(["button", "enter", "queue"] as const)(
  "%s finishes recording, waits for transcription, and preserves the complete draft on submission failure",
  async (action) => {
    const { editor, transcript, steer, queue } = await setupSpeech(action === "button" ? "" : "typed");
    fireEvent.click(await screen.findByRole("button", { name: appI18n.t("speech.record") }));
    await screen.findByRole("button", { name: appI18n.t("speech.stop") });
    const send = screen.getByRole("button", { name: appI18n.t("chatComposer.send") });
    expect(send).toBeEnabled();
    if (action === "button") fireEvent.click(send);
    else fireEvent.keyDown(editor, { key: "Enter", metaKey: action === "queue" });
    expect(steer).not.toHaveBeenCalled();
    expect(queue).not.toHaveBeenCalled();
    await act(async () => {
      transcript.resolve({ kind: "transcript", text: "dictated" });
    });
    const submitted = action === "queue" ? queue : steer;
    await waitFor(() => {
      expect(submitted).toHaveBeenCalledTimes(1);
    });
    expect(action === "queue" ? steer : queue).not.toHaveBeenCalled();
    await waitFor(() => {
      expect(editor).toHaveValue(action === "button" ? "dictated" : "typed\n\ndictated");
    });
  },
);

it("opens ordinary recovery instead of submitting when transcription fails", async () => {
  const { transcript, readTranscript, steer, queue } = await setupSpeech("typed");
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("speech.record") }));
  await screen.findByRole("button", { name: appI18n.t("speech.stop") });
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chatComposer.send") }));
  await waitFor(() => {
    expect(readTranscript).toHaveBeenCalled();
  });
  await act(async () => {
    transcript.reject(new Error("transcription failed"));
  });
  await screen.findByRole("dialog");
  expect(steer).not.toHaveBeenCalled();
  expect(queue).not.toHaveBeenCalled();
  expect(screen.getByRole("textbox")).toHaveValue("typed");
});

it("does not send an empty final message", async () => {
  const { transcript, steer, queue } = await setupSpeech("");
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("speech.record") }));
  await screen.findByRole("button", { name: appI18n.t("speech.stop") });
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chatComposer.send") }));
  await act(async () => {
    transcript.resolve({ kind: "noop" });
  });
  await screen.findByRole("button", { name: appI18n.t("speech.record") });
  expect(steer).not.toHaveBeenCalled();
  expect(queue).not.toHaveBeenCalled();
});

it("keeps Question Send disabled without a selected answer while recording", async () => {
  const { transcript, answer } = await setupSpeech("ordinary draft", question());
  fireEvent.click(await screen.findByRole("button", { name: appI18n.t("speech.record") }));
  await screen.findByRole("button", { name: appI18n.t("speech.stop") });
  expect(screen.getByRole("button", { name: appI18n.t("chatComposer.send") })).toBeDisabled();
  fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter", metaKey: true });
  await act(async () => {
    transcript.resolve({ kind: "transcript", text: "commentary" });
  });
  await screen.findByRole("button", { name: appI18n.t("speech.record") });
  expect(answer).not.toHaveBeenCalled();
});

it.each(["enter", "command", "button"] as const)(
  "Question %s finishes recording and follows the answer submission rules",
  async (action) => {
    const { transcript, answer, steer, queue } = await setupSpeech("ordinary draft", question());
    const send = screen.getByRole("button", { name: appI18n.t("chatComposer.send") });
    expect(send).toBeDisabled();
    const choice = screen.getAllByRole("radio").at(0);
    if (choice === undefined) throw new Error("Question has no answer choices");
    fireEvent.click(choice);
    fireEvent.click(await screen.findByRole("button", { name: appI18n.t("speech.record") }));
    await screen.findByRole("button", { name: appI18n.t("speech.stop") });
    expect(send).toBeEnabled();
    if (action === "button") fireEvent.click(send);
    else fireEvent.keyDown(screen.getByRole("textbox"), { key: "Enter", metaKey: action === "command" });
    expect(answer).not.toHaveBeenCalled();
    await act(async () => {
      transcript.resolve({ kind: "transcript", text: "commentary" });
    });
    if (action !== "enter")
      await waitFor(() => {
        expect(answer).toHaveBeenCalledTimes(1);
      });
    else {
      await screen.findByRole("button", { name: appI18n.t("speech.record") });
      expect(answer).not.toHaveBeenCalled();
    }
    expect(steer).not.toHaveBeenCalled();
    expect(queue).not.toHaveBeenCalled();
  },
);
