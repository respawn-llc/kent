import { afterEach, expect, it, mock, vi } from "bun:test";
import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { appI18n } from "@/i18n";

const warning = vi.fn(() => "connection-warning");
afterEach(() => warning.mockClear());

const actualSonner = await import("sonner");
await mock.module("sonner", () => ({
  ...actualSonner,
  toast: Object.assign(actualSonner.toast, { warning }),
}));
const { sessionSettingsRead, sessionWithPrompts } = await import("@/test-support/chat-destination");

it("loads Chat and warns through Sonner when the server projects a replacement Connection", async () => {
  sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "getSettings").mockResolvedValue({
      ...sessionSettingsRead(),
      connectionReplacement: { previous: "removed", current: "work" },
    });
  });
  expect(await screen.findByRole("textbox")).toBeEnabled();
  await waitFor(() => {
    expect(warning).toHaveBeenCalledTimes(1);
  });
});

it("loads a configured Connection without a replacement warning", async () => {
  sessionWithPrompts();
  expect(await screen.findByRole("textbox")).toBeEnabled();
  expect(warning).not.toHaveBeenCalled();
});

it("shows an actionable read failure when replacement settings are unavailable and allows Retry", async () => {
  sessionWithPrompts((services) => {
    vi.spyOn(services.api.chat, "getSettings").mockRejectedValueOnce(new Error("replacement unavailable"));
  });
  const retry = await screen.findByRole("button", { name: appI18n.t("app.retry") });
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  expect(warning).not.toHaveBeenCalled();
  await userEvent.setup().click(retry);
  expect(await screen.findByRole("textbox")).toBeEnabled();
});
