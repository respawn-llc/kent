import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { parsePendingWorkItemID, type ChatInputMutationResult } from "@/api";
import { createTestServices } from "@/test-support/app-services";
import { composerWrapper } from "@/test-support/composer";
import { useChatComposer, target } from "./chatComposerTestFixture";
import { createChatStorageFixture } from "./chatStorageFixture";
import { useComposerKeyboard } from "./useComposerKeyboard";

beforeEach(() => vi.stubGlobal("localStorage", createChatStorageFixture()));
afterEach(() => vi.unstubAllGlobals());

it.each([
  { platform: "macos", modifier: "metaKey", intent: "queue" },
  { platform: "macos", modifier: "ctrlKey", intent: null },
  { platform: "linux", modifier: "ctrlKey", intent: "queue" },
  { platform: "windows", modifier: "ctrlKey", intent: "queue" },
  { platform: "linux", modifier: "metaKey", intent: null },
  { platform: "macos", modifier: null, intent: "send" },
  { platform: "macos", modifier: "shiftKey", intent: null },
] as const)("uses the platform queue shortcut: %j", async ({ platform, modifier, intent }) => {
  const services = createTestServices([], undefined, { platform });
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "queued text", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const accepted: ChatInputMutationResult = {
    sessionID: target.sessionID,
    outcome: {
      kind: "accepted",
      queueItemID: parsePendingWorkItemID("79f762c8-0998-402a-bc80-6b42f3e241ed"),
      diagnostic: null,
    },
  };
  const queue = vi.spyOn(services.api.chat, "queue").mockResolvedValue(accepted);
  const send = vi.spyOn(services.api.chat, "steer").mockResolvedValue(accepted);
  function Probe() {
    const composer = useChatComposer({ ...target, submission: { kind: "ready" } });
    const keyboard = useComposerKeyboard(composer, false, null);
    return (
      <textarea
        aria-label="Composer shortcut"
        disabled={!composer.canSubmit}
        value={composer.text}
        readOnly
        onKeyDown={keyboard.onEditorKeyDown}
      />
    );
  }
  render(<Probe />, { wrapper: composerWrapper(services) });
  const editor = screen.getByRole("textbox", { name: "Composer shortcut" });
  await waitFor(() => expect(editor).not.toBeDisabled());
  fireEvent.keyDown(editor, { key: "Enter", ...(modifier === null ? {} : { [modifier]: true }) });
  if (intent !== null) {
    await waitFor(() => {
      expect(intent === "queue" ? queue : send).toHaveBeenCalledOnce();
    });
  }
  expect(queue).toHaveBeenCalledTimes(intent === "queue" ? 1 : 0);
  expect(send).toHaveBeenCalledTimes(intent === "send" ? 1 : 0);
});
