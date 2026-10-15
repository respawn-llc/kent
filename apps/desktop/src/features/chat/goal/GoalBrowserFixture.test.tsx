import { useState } from "react";
import userEvent from "@testing-library/user-event";
import { render, screen, waitFor, within } from "@testing-library/react";

import { ChatPromptPresenceProvider } from "@/app-facade";
import { appI18n } from "@/i18n";
import { createTestServices, startupRoutes, TestAppProviders } from "@/test-support/app-services";
import { TestSidebar } from "@/test-support/sidebar-rendering";
import { GoalBrowserFixture, type GoalBrowserPendingPrompt } from "./GoalBrowserFixture";

function renderFixture() {
  const services = createTestServices(startupRoutes);
  render(
    <TestAppProviders services={services}>
      <ChatPromptPresenceProvider>
        <TestSidebar>
          <GoalBrowserFixtureHarness />
        </TestSidebar>
      </ChatPromptPresenceProvider>
    </TestAppProviders>,
  );
}

function GoalBrowserFixtureHarness() {
  const [pendingPrompt, setPendingPrompt] = useState<GoalBrowserPendingPrompt | null>(null);
  return (
    <>
      <GoalBrowserFixture onPromptOpen={setPendingPrompt} />
      <div
        data-prompt-id={pendingPrompt?.promptID}
        data-prompt-kind={pendingPrompt?.kind}
        data-testid="pending-chat-prompt"
      />
    </>
  );
}

async function selectFixture(label: string, value: string) {
  await userEvent.setup({ delay: null }).selectOptions(screen.getByRole("combobox", { name: label }), value);
}

describe("Goal browser fixture", () => {
  it("closes the Goal sidebar on repeated chip activation and opens it again", async () => {
    renderFixture();
    await screen.findByRole("textbox", { name: "Goal" });
    const user = userEvent.setup({ delay: null });
    const chip = within(screen.getByTestId("goal-browser-fixture")).getByRole("button", { name: "Goal" });
    await user.click(chip);
    await waitFor(() => {
      expect(screen.queryByRole("complementary")).not.toBeInTheDocument();
    });
    await user.click(chip);
    expect(await screen.findByRole("textbox", { name: "Goal" })).toBeInTheDocument();
  });

  it.each([
    { state: "active", action: "pause", next: "resume" },
    { state: "paused", action: "resume", next: "pause" },
    { state: "complete", action: "reopen", next: "pause" },
  ] as const)(
    "keeps $action pending without showing an objective-save spinner",
    async ({ state, action, next }) => {
      renderFixture();
      await selectFixture("Goal fixture state", state);
      await selectFixture("Goal mutation outcome", "pending");

      await userEvent
        .setup({ delay: null })
        .click(await screen.findByRole("button", { name: appI18n.t(`chat.goal.${action}`) }));
      expect(screen.queryByTestId("goal-mutation-pending")).not.toBeInTheDocument();
      expect(screen.getByRole("button", { name: appI18n.t(`chat.goal.${next}`) })).toBeDisabled();
      await userEvent
        .setup({ delay: null })
        .click(screen.getByRole("button", { name: "Resolve pending Goal action" }));

      expect(await screen.findByRole("button", { name: appI18n.t(`chat.goal.${next}`) })).toBeInTheDocument();
      await waitFor(() => {
        expect(screen.queryByTestId("goal-mutation-pending")).not.toBeInTheDocument();
      });
    },
  );

  it("shows pending Clear and restores the objective and actions on failure", async () => {
    renderFixture();
    await selectFixture("Goal fixture state", "active");
    await selectFixture("Goal mutation outcome", "pending");
    const user = userEvent.setup({ delay: null });
    await user.click(await screen.findByRole("button", { name: "Clear" }));
    expect(screen.getByTestId("goal-mutation-pending")).toBeInTheDocument();
    expect(screen.getByRole("textbox", { name: "Goal" })).toHaveValue("");
    await user.click(screen.getByRole("button", { name: "Fail pending Goal action" }));
    expect(await screen.findByText("Deterministic Goal fixture")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Clear" })).toBeEnabled();
    await waitFor(() => {
      expect(screen.queryByTestId("goal-mutation-pending")).not.toBeInTheDocument();
    });
  });

  it("exercises hydration hold and observation retry through the destination", async () => {
    renderFixture();
    await selectFixture("Goal fixture state", "loading");
    expect(await screen.findByTestId("loading-state")).toBeInTheDocument();
    await userEvent.setup({ delay: null }).click(screen.getByRole("button", { name: "Hydrate Goal" }));
    expect(await screen.findByText("Deterministic Goal fixture")).toBeInTheDocument();

    await selectFixture("Goal fixture state", "error-retry");
    await userEvent.setup({ delay: null }).click(await screen.findByRole("button", { name: "Try again" }));
    await userEvent.setup({ delay: null }).click(screen.getByRole("button", { name: "Hydrate Goal" }));
    expect(await screen.findByText("Deterministic Goal fixture")).toBeInTheDocument();
  });

  it("keeps New Chat rejection drafts and opaque prompt identity independent", async () => {
    renderFixture();
    await selectFixture("Goal fixture state", "new-chat");
    await selectFixture("New Chat Set outcome", "rejection");

    const user = userEvent.setup({ delay: null });
    const objective = "Preserve this New Chat draft";
    await user.type(await screen.findByRole("textbox", { name: "Goal" }), objective);
    await user.click(screen.getByTestId("goal-save"));
    expect(await screen.findByDisplayValue(objective)).toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: "Open pending Question" }));
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-kind", "question");
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-id", "question-1");
    await user.click(screen.getByRole("button", { name: "Open pending Approval" }));
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-kind", "approval");
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-id", "approval-1");
  });
});
