import { act, fireEvent, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { hydration, target, transcriptPage } from "@/test-support/chat-runtime";
import { catalog, opening, sessionWithPrompts } from "@/test-support/chat-destination";
import { installAnimatedHeightGeometry } from "@/test-support/resize-observer";
import { row } from "@/test-support/transcript-window";

it.each([
  ["new-chat", false],
  ["existing-session", false],
  ["existing-session", true],
] as const)(
  "carries composer text into committed %s content with scroll interruption=%s",
  async (kind, interrupt) => {
    const geometry = installAnimatedHeightGeometry(80);
    let committed: ReturnType<typeof row> | null = null;
    const view = sessionWithPrompts(
      (services) => {
        const sessionSettings = vi.mocked(services.api.chat.getSettings).getMockImplementation();
        vi.mocked(services.api.chat.getSettings).mockImplementation(async (selection) => {
          if (selection.kind === "new_chat") return catalog;
          if (sessionSettings === undefined) throw new Error("Missing Session settings fixture.");
          return sessionSettings(selection);
        });
        vi.spyOn(services.api.chat, "getCommandCatalog").mockResolvedValue([]);
        vi.spyOn(services.api.chat, "steer").mockImplementation(async (_, __, id) => {
          committed = {
            ...row(1),
            User: { Text: "First message", QueueItemIDs: [id.toJSONValue()] },
          };
          return {
            sessionID: target.sessionID,
            outcome: { kind: "accepted", queueItemID: id, diagnostic: null },
          };
        });
        vi.mocked(services.api.chat.getTranscriptPage).mockImplementation(async () => ({
          ...transcriptPage(null),
          entries: committed === null ? [] : [committed],
        }));
      },
      kind === "new-chat" ? opening : { kind: "session", ...target },
      undefined,
      false,
    );
    try {
      const editor = await screen.findByRole("textbox");
      vi.spyOn(editor, "getBoundingClientRect").mockReturnValue(new DOMRect(0, 0, 320, 80));
      if (kind === "existing-session") {
        await waitFor(() => {
          expect(view.handlers).toHaveLength(1);
        });
        await act(async () => {
          view.handlers[0]?.onEvent({ sequence: 1, kind: "hydration", payload: hydration() });
        });
      }
      const user = userEvent.setup();
      await user.type(editor, "First message");
      await user.keyboard("{Enter}");
      await waitFor(() => {
        expect(view.handlers).toHaveLength(1);
      });
      await act(async () => {
        if (committed === null) throw new Error("Input was not committed.");
        if (kind === "existing-session") {
          view.handlers[0]?.onEvent({ sequence: 2, kind: "committed_row", payload: committed });
          return;
        }
        view.handlers[0]?.onEvent({
          sequence: 1,
          kind: "hydration",
          payload: {
            ...hydration(),
            TailSegment: {
              Entries: [committed],
              OlderCursor: null,
              HasMoreAbove: false,
            },
          },
        });
      });
      const list = await screen.findByRole("list");
      const text = within(list).getByText("First message");
      // Text reappears inside the ordinary expanding bubble, not a clipped copy.
      await waitFor(
        () => {
          expect(text).not.toBeVisible();
        },
        { interval: 1 },
      );
      expect(screen.getAllByText("First message")).toHaveLength(2);
      if (interrupt) fireEvent.wheel(list, { deltaY: -100 });
      await waitFor(() => {
        expect(text).toBeVisible();
      });
      expect(screen.getByRole("textbox")).toHaveValue("");
    } finally {
      view.unmount();
      geometry.restore();
    }
  },
);
