import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { createTestServices } from "@/test-support/app-services";
import { composerWrapper } from "@/test-support/composer";
import { useChatClipboard } from "./ChatClipboard";
import { deferred } from "@/test-support/chat-runtime";
import * as ui from "@/ui";
import { ChatPromptPresenceProvider, ChatRuntimeProvider } from "@/app-facade";
import { mainViewRead, runtimeHost, transcriptPage } from "@/test-support/chat-runtime";
import { TestAppProviders } from "@/test-support/composer";
import { ChatComposer } from "./ChatComposer";
import { ChatComposerSurface } from "./ChatComposerSurface";
import { useChatComposer, target } from "./chatComposerTestFixture";
import { createChatStorageFixture } from "./chatStorageFixture";

beforeEach(() => vi.stubGlobal("localStorage", createChatStorageFixture()));
afterEach(() => vi.unstubAllGlobals());

function clipboardServices(paste: () => Promise<string | null>) {
  const services = createTestServices([]);
  return {
    ...services,
    nativeBridge: {
      ...services.nativeBridge,
      clipboard: { ...services.nativeBridge.clipboard, paste },
    },
  };
}

it.each(["macos", "windows", "linux", "menu"] as const)(
  "pastes through the Chat composer using %s input",
  async (platform) => {
    const paste = vi.fn(async () => "/tmp/kent-clipboard-image.png");
    const services = clipboardServices(paste);
    vi.spyOn(services.api.chat, "subscribeTranscript").mockReturnValue({ close: () => undefined });
    vi.spyOn(services.api.chat, "getMainView").mockResolvedValue(mainViewRead());
    vi.spyOn(services.api.chat, "getTranscriptPage").mockResolvedValue(transcriptPage(null));
    vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({
      input: "before selected after",
      protectedInput: null,
    });
    vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
    function Composer() {
      const composer = useChatComposer(target);
      return (
        <ChatComposerSurface composer={composer}>
          <ChatComposer
            settings={{ kind: "loading-session", retry: () => undefined }}
            availableHeight={null}
            onHeightChange={() => undefined}
          />
        </ChatComposerSurface>
      );
    }
    render(
      <TestAppProviders services={services}>
        <ChatPromptPresenceProvider>
          <ChatRuntimeProvider api={services.api} target={target} host={runtimeHost()}>
            <Composer />
          </ChatRuntimeProvider>
        </ChatPromptPresenceProvider>
      </TestAppProviders>,
    );
    const editor = await screen.findByRole<HTMLTextAreaElement>("textbox");
    await waitFor(() => {
      expect(editor).toHaveValue("before selected after");
    });
    editor.focus();
    editor.setSelectionRange(7, 15);
    if (platform === "menu") fireEvent.paste(editor);
    else
      fireEvent.keyDown(editor, { key: "v", metaKey: platform === "macos", ctrlKey: platform !== "macos" });
    await waitFor(() => {
      expect(editor).toHaveValue("before /tmp/kent-clipboard-image.png after");
    });
    expect(paste).toHaveBeenCalledOnce();
  },
);

it("replaces the selected composer text with the clipboard image path", async () => {
  const services = clipboardServices(async () => "/tmp/kent-clipboard-image.png");
  const { result } = renderHook(() => useChatClipboard(), { wrapper: composerWrapper(services) });
  const input = document.createElement("textarea");
  document.body.append(input);
  input.value = "before selected after";
  input.setSelectionRange(7, 15);
  const changed = vi.fn();
  input.addEventListener("input", changed);
  act(() => {
    result.current.paste(input);
  });
  await waitFor(() => {
    expect(input.value).toBe("before /tmp/kent-clipboard-image.png after");
  });
  expect(changed).toHaveBeenCalledOnce();
  input.remove();
});

it.each(["plain text\nsecond line", null])(
  "pastes text or leaves an empty clipboard unchanged: %j",
  async (content) => {
    const services = clipboardServices(async () => content);
    const { result } = renderHook(() => useChatClipboard(), { wrapper: composerWrapper(services) });
    const input = document.createElement("textarea");
    document.body.append(input);
    input.value = "draft";
    input.setSelectionRange(5, 5);
    act(() => {
      result.current.paste(input);
    });
    await waitFor(() => {
      expect(result.current.pending).toBe(false);
    });
    expect(input.value).toBe(`draft${content ?? ""}`);
    input.remove();
  },
);

it("admits one pending paste and does not insert into a departed composer", async () => {
  const read = deferred<string | null>();
  const paste = vi.fn(async () => read.promise);
  const services = clipboardServices(paste);
  const { result } = renderHook(() => useChatClipboard(), { wrapper: composerWrapper(services) });
  const input = document.createElement("textarea");
  document.body.append(input);
  input.value = "draft";
  act(() => {
    result.current.paste(input);
  });
  await waitFor(() => {
    expect(result.current.pending).toBe(true);
  });
  act(() => {
    result.current.paste(input);
  });
  input.remove();
  await act(async () => {
    read.resolve("/tmp/image.png");
  });
  await waitFor(() => {
    expect(result.current.pending).toBe(false);
  });
  expect(paste).toHaveBeenCalledOnce();
  expect(input.value).toBe("draft");
});

it("preserves the draft and reports clipboard failures", async () => {
  const report = vi.spyOn(ui, "showStatusToast").mockImplementation(() => undefined);
  const services = clipboardServices(async () => {
    throw new Error("clipboard unavailable");
  });
  const { result } = renderHook(() => useChatClipboard(), { wrapper: composerWrapper(services) });
  const input = document.createElement("textarea");
  document.body.append(input);
  input.value = "draft";
  act(() => {
    result.current.paste(input);
  });
  await waitFor(() => {
    expect(report).toHaveBeenCalledOnce();
  });
  expect(input.value).toBe("draft");
  expect(result.current.pending).toBe(false);
  input.remove();
  report.mockRestore();
});
