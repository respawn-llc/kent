import { useState } from "react";
import userEvent from "@testing-library/user-event";
import { render, screen } from "@testing-library/react";

import { ChatPromptPresenceProvider } from "@/app-facade";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { TestSidebar } from "@/test-support/sidebar";
import { GoalBrowserFixture, type GoalBrowserPendingPrompt } from "./GoalBrowserFixture";

function renderFixture() {
  const services = createTestServices([]);
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
  await userEvent.setup().selectOptions(screen.getByRole("combobox", { name: label }), value);
}

describe("Goal browser fixture", () => {
  it("mounts the real sidebar and resolves a pending lifecycle action", async () => {
    renderFixture();
    await selectFixture("Goal fixture state", "active");
    await selectFixture("Goal mutation outcome", "pending");

    await userEvent.setup().click(await screen.findByRole("button", { name: "Pause" }));
    await userEvent.setup().click(screen.getByRole("button", { name: "Resolve pending Goal action" }));

    expect(await screen.findByRole("button", { name: "Resume" })).toBeInTheDocument();
  });

  it("exercises hydration hold and observation retry through the destination", async () => {
    renderFixture();
    await selectFixture("Goal fixture state", "loading");
    expect(await screen.findByTestId("loading-state")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Hydrate Goal" }));
    expect(await screen.findByText("Deterministic Goal fixture")).toBeInTheDocument();

    await selectFixture("Goal fixture state", "error-retry");
    await userEvent.setup().click(await screen.findByRole("button", { name: "Try again" }));
    await userEvent.setup().click(screen.getByRole("button", { name: "Hydrate Goal" }));
    expect(await screen.findByText("Deterministic Goal fixture")).toBeInTheDocument();
  });

  it("keeps New Chat rejection drafts and opaque prompt identity independent", async () => {
    renderFixture();
    await selectFixture("Goal fixture state", "new-chat");
    await selectFixture("New Chat Set outcome", "rejection");

    const user = userEvent.setup();
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
