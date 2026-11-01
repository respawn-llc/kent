import { afterEach, beforeEach, expect, it, vi } from "bun:test";
import { act, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useCurrentWindowChromeTitle } from "@/app-facade";
import { appI18n } from "@/i18n";
import { deferred, mainViewRead, target } from "@/test-support/chat-runtime";
import { sessionWithPrompts } from "@/test-support/chat-destination";
import {
  emitSessionSettingsSnapshot,
  hasSessionSettingsSubscription,
  openSessionSettingsSubscription,
} from "@/test-support/api";
import { installChatStorageFixture } from "./chatStorageFixture";

let restoreStorage: (() => void) | undefined;
beforeEach(() => {
  restoreStorage = installChatStorageFixture();
});
afterEach(() => {
  restoreStorage?.();
  restoreStorage = undefined;
});

function TitleProbe() {
  return <output data-testid="delivered-title">{useCurrentWindowChromeTitle()}</output>;
}

it("delivers later Settings while its Main View refresh remains unresolved", async () => {
  const pending = deferred<ReturnType<typeof mainViewRead>>();
  const view = sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "getMainView")
      .mockResolvedValueOnce(mainViewRead())
      .mockReturnValue(pending.promise);
  });
  const user = userEvent.setup();
  await waitFor(() => {
    expect(hasSessionSettingsSubscription(view.services.transport)).toBe(true);
    expect(view.services.api.chat.getMainView).toHaveBeenCalledTimes(1);
  });
  openSessionSettingsSubscription(view.services.transport);
  await act(async () => {
    emitSessionSettingsSnapshot(view.services.transport, target.sessionID, null, true);
  });
  await waitFor(() => {
    expect(view.services.api.chat.getMainView).toHaveBeenCalledTimes(2);
  });
  await act(async () => {
    emitSessionSettingsSnapshot(view.services.transport, target.sessionID, null, false);
  });
  await user.hover(screen.getByRole("button", { name: appI18n.t("chatSettings.open") }));
  await waitFor(() => {
    expect(
      within(screen.getByRole("dialog")).getByRole("switch", { name: appI18n.t("chatSettings.questions") }),
    ).not.toBeChecked();
  });
  view.unmount();
  pending.resolve(mainViewRead());
});

it.each(["unavailable", "registered_idle"] as const)(
  "updates two %s Chats from real settings messages without resetting drafts or transcript observation",
  async (state) => {
    let name: string | null = null;
    let sequence = 1;
    const views = [0, 1].map(() =>
      sessionWithPrompts(
        (services) => {
          vi.spyOn(services.api.chat, "getMainView").mockImplementation(async () => {
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
    for (const view of views) openSessionSettingsSubscription(view.services.transport);
    const editors = await screen.findAllByRole("textbox");
    const user = userEvent.setup({ delay: null });
    for (const [index, editor] of editors.entries()) {
      await user.click(editor);
      await user.paste(`independent draft ${index.toString()}`);
    }
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
      await user.hover(button);
      const dialog = screen.getByRole("dialog");
      expect(
        within(dialog).getByRole("switch", {
          name: appI18n.t("chatSettings.questions"),
        }),
      ).not.toBeChecked();
      await user.keyboard("{Escape}");
      await waitFor(() => {
        expect(dialog.isConnected).toBe(false);
      });
      expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
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
