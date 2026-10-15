import { screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type * as Sonner from "sonner";
import { appI18n } from "@/i18n";
import { sessionWithPrompts } from "@/test-support/chat-destination";

const warning = vi.hoisted(() => vi.fn(() => "connection-warning"));
vi.mock("sonner", async (importOriginal) => {
  const original = await importOriginal<typeof Sonner>();
  return { ...original, toast: Object.assign(original.toast, { warning }) };
});

it("loads Chat and warns through Sonner when the server projects a replacement Connection", async () => {
  sessionWithPrompts((services) => {
    const read = vi.mocked(services.api.chat.getSettings);
    const original = read.getMockImplementation();
    if (original === undefined) throw new Error("Missing settings read fixture");
    read.mockImplementation(async (target) => ({
      ...(await original(target)),
      connectionReplacement: { previous: "removed", current: "work" },
    }));
  });
  expect(await screen.findByRole("textbox")).toBeEnabled();
  await waitFor(() => {
    expect(warning).toHaveBeenCalledOnce();
  });
});

it("loads a configured Connection without a replacement warning", async () => {
  sessionWithPrompts();
  expect(await screen.findByRole("textbox")).toBeEnabled();
  expect(warning).not.toHaveBeenCalled();
});

it("shows an actionable read failure when replacement settings are unavailable and allows Retry", async () => {
  sessionWithPrompts((services) => {
    vi.mocked(services.api.chat.getSettings).mockRejectedValueOnce(new Error("replacement unavailable"));
  });
  const retry = await screen.findByRole("button", { name: appI18n.t("app.retry") });
  expect(screen.queryByRole("textbox")).not.toBeInTheDocument();
  expect(warning).not.toHaveBeenCalled();
  await userEvent.setup().click(retry);
  expect(await screen.findByRole("textbox")).toBeEnabled();
});
