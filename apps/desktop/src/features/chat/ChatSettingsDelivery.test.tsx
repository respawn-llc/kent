import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useCurrentWindowChromeTitle } from "@/app-facade";
import { appI18n } from "@/i18n";
import { mainViewRead, target } from "@/test-support/chat-runtime";
import { sessionWithPrompts } from "@/test-support/chat-destination";
import { emitSessionSettingsSnapshot, hasSessionSettingsSubscription } from "@/test-support/api";
import { createChatStorageFixture } from "./chatStorageFixture";

beforeEach(() => vi.stubGlobal("localStorage", createChatStorageFixture()));
afterEach(() => vi.unstubAllGlobals());

function TitleProbe() {
  return <output data-testid="delivered-title">{useCurrentWindowChromeTitle()}</output>;
}

it.each(["unavailable", "registered_idle"] as const)(
  "updates two %s Chats from real settings messages without resetting drafts or transcript observation",
  async (state) => {
    let name: string | null = null;
    let sequence = 1;
    const views = [0, 1].map(() =>
      sessionWithPrompts(
        (services) => {
          vi.mocked(services.api.chat.getMainView).mockImplementation(async () => {
            const read = mainViewRead(sequence++);
            return {
              ...read,
              mainView: {
                ...read.mainView,
                sessionName: name,
                activity: { ...read.mainView.activity, state },
              },
            };
          });
        },
        { kind: "session", ...target },
        <TitleProbe />,
      ),
    );
    await waitFor(() => {
      for (const view of views) {
        expect(view.handlers).toHaveLength(1);
        expect(hasSessionSettingsSubscription(view.services.transport)).toBe(true);
      }
    });
    const editors = await screen.findAllByRole("textbox");
    for (const [index, editor] of editors.entries())
      await userEvent.setup().type(editor, `independent draft ${index.toString()}`);
    const deliver = async (questions: boolean) => {
      await act(async () => {
        for (const view of views)
          emitSessionSettingsSnapshot(view.services.transport, target.sessionID, name, questions);
      });
    };
    name = "Delivered name";
    await deliver(true);
    await waitFor(() => {
      for (const title of screen.getAllByTestId("delivered-title"))
        expect(title.textContent).toBe("Delivered name");
    });
    await deliver(false);
    for (const button of screen.getAllByRole("button", { name: appI18n.t("chatSettings.open") })) {
      await userEvent.setup().click(button);
      expect(
        within(screen.getByRole("dialog")).getByRole("switch", {
          name: appI18n.t("chatSettings.questions"),
        }),
      ).not.toBeChecked();
      await userEvent.setup().keyboard("{Escape}");
    }
    name = null;
    await deliver(false);
    await waitFor(() => {
      for (const title of screen.getAllByTestId("delivered-title")) expect(title.textContent).toBe("");
    });
    for (const [index, editor] of editors.entries())
      expect(editor).toHaveValue(`independent draft ${index.toString()}`);
    for (const view of views) expect(view.handlers).toHaveLength(1);
  },
);
