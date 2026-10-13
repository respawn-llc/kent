import { act, fireEvent, render, renderHook, screen, waitFor } from "@testing-library/react";
import { parsePendingWorkItemID } from "@/api";
import { createTestServices } from "@/test-support/app-services";
import { TestAppProviders, composerWrapper } from "@/test-support/composer";
import { useChatComposer, target } from "./chatComposerTestFixture";
import { createChatStorageFixture } from "./chatStorageFixture";
import { useComposerKeyboard } from "./useComposerKeyboard";

beforeEach(() => vi.stubGlobal("localStorage", createChatStorageFixture()));
afterEach(() => vi.unstubAllGlobals());

it.each(["send", "queue"] as const)(
  "completes a selected prompt command and dispatches it through %s",
  async (intent) => {
    const services = createTestServices([]);
    vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "/rev", protectedInput: null });
    vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
    const queue = vi.spyOn(services.api.chat, intent === "send" ? "steer" : "queue").mockResolvedValue({
      sessionID: target.sessionID,
      outcome: {
        kind: "accepted",
        queueItemID: parsePendingWorkItemID("79f762c8-0998-402a-bc80-6b42f3e241ed"),
        diagnostic: null,
      },
    });
    const { result } = renderHook(
      () =>
        useChatComposer({
          ...target,
          submission: { kind: "ready" },
          commands: [
            {
              token: "/prompt:review",
              aliases: ["/review"],
              description: null,
              preview: null,
              execution: { kind: "prompt", catalogIdentity: "prompt:review" },
            },
          ],
        }),
      { wrapper: composerWrapper(services) },
    );
    await waitFor(() => {
      expect(result.current.draft.kind).toBe("ready");
    });
    expect(result.current.pickerOpen).toBe(true);
    expect(result.current.selectedCommand?.token).toBe("/prompt:review");
    await act(async () => {
      result.current.submit(intent);
    });
    expect(queue).toHaveBeenCalledWith(
      target,
      {
        kind: "command",
        catalogIdentity: "prompt:review",
        token: "/prompt:review",
        separatorWhitespace: "",
        arguments: "",
      },
      expect.anything(),
    );
  },
);

it("completes the selected slash command with Tab without submitting work", async () => {
  const services = createTestServices([]);
  vi.spyOn(services.api.chat, "getDraft").mockResolvedValue({ input: "/review", protectedInput: null });
  vi.spyOn(services.api.chat, "persistDraft").mockResolvedValue();
  const steer = vi.spyOn(services.api.chat, "steer");
  const queue = vi.spyOn(services.api.chat, "queue");
  const commands = ["prompt:review", "prompt:review-all"].map((name) => ({
    token: `/${name}`,
    aliases: [],
    description: null,
    preview: null,
    execution: { kind: "prompt", catalogIdentity: name } as const,
  }));
  function Probe() {
    const composer = useChatComposer({ ...target, commands });
    const keyboard = useComposerKeyboard(composer, false, null);
    return (
      <textarea
        data-testid="completion-editor"
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
  const editor = screen.getByTestId<HTMLTextAreaElement>("completion-editor");
  await waitFor(() => {
    expect(editor).toHaveValue("/review");
  });
  expect(fireEvent.keyDown(editor, { key: "Tab", shiftKey: true })).toBe(true);
  expect(editor).toHaveValue("/review");
  fireEvent.keyDown(editor, { key: "ArrowDown" });
  expect(fireEvent.keyDown(editor, { key: "Tab" })).toBe(false);
  expect(editor).toHaveValue("/prompt:review-all ");
  expect(editor.selectionStart).toBe(editor.value.length);
  expect(fireEvent.keyDown(editor, { key: "Tab" })).toBe(true);
  expect(steer).not.toHaveBeenCalled();
  expect(queue).not.toHaveBeenCalled();
});
