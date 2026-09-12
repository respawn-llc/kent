import { useState } from "react";
import userEvent from "@testing-library/user-event";
import { render, screen, waitFor } from "@testing-library/react";

import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { SidebarHost } from "@/app/sidebar";
import { SidebarProvider } from "@/app/sidebarProvider";
import { sidebarDestinationPolicy } from "@/app/sidebarDestinationPolicy";
import { SidebarRootOwner } from "@/app-facade";
import { GoalBrowserFixture, type GoalBrowserPendingPrompt } from "./GoalBrowserFixture";

function renderFixture() {
  const services = createTestServices([]);
  render(
    <TestAppProviders services={services}>
      <SidebarProvider policy={sidebarDestinationPolicy}>
        <SidebarRootOwner>
          <GoalBrowserFixtureHarness />
        </SidebarRootOwner>
        <SidebarHost />
      </SidebarProvider>
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

async function selectFixture(state: string) {
  await userEvent.setup().selectOptions(screen.getByRole("combobox", { name: "Goal fixture state" }), state);
}

describe("Goal browser fixture", () => {
  it("mounts the real Goal sidebar and resolves a pending lifecycle action", async () => {
    renderFixture();
    await selectFixture("pending-pause");

    expect(await screen.findByRole("button", { name: "Pause" })).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Pause" }));

    expect(await screen.findByRole("button", { name: "Resume" })).toBeInTheDocument();
    expect(screen.getByText("Paused")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Resolve pending Goal action" }));

    expect(await screen.findByRole("button", { name: "Resume" })).toBeInTheDocument();
  });

  it("exercises observation Loading and Retry through the destination", async () => {
    renderFixture();
    await selectFixture("loading");
    expect(await screen.findByTestId("loading-state")).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Hydrate Goal" }));
    expect(await screen.findByText("Deterministic Goal fixture")).toBeInTheDocument();

    await selectFixture("error-retry");
    expect(await screen.findByRole("button", { name: "Try again" })).toBeInTheDocument();
    await userEvent.setup().click(screen.getByRole("button", { name: "Try again" }));
    expect(await screen.findByText("Deterministic Goal fixture")).toBeInTheDocument();
  });

  it("keeps the New Chat draft after ambiguous loss and keeps prompt pickers visible", async () => {
    renderFixture();
    await selectFixture("new-chat-loss");
    const user = userEvent.setup();
    const objective = "Preserve this browser fixture draft";
    await user.type(await screen.findByRole("textbox", { name: "Goal" }), objective);
    await user.click(screen.getByTestId("goal-save"));

    await userEvent.setup().click(screen.getByRole("button", { name: "Resolve pending Goal action" }));
    expect(await screen.findByDisplayValue(objective)).toBeInTheDocument();
    expect(screen.getByTestId("goal-save")).toBeInTheDocument();

    await selectFixture("question-picker");
    await userEvent.setup().click(screen.getByRole("button", { name: "Open pending Question" }));
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-kind", "question");
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-id", "question-1");
    expect(await screen.findByRole("button", { name: "Pause" })).toBeInTheDocument();

    await selectFixture("approval-picker");
    await userEvent.setup().click(screen.getByRole("button", { name: "Open pending Approval" }));
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-kind", "approval");
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-id", "approval-1");
    await userEvent.setup().click(screen.getByRole("button", { name: "Close" }));
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-kind", "approval");
    await userEvent.setup().click(screen.getByRole("button", { name: "Goal" }));
    expect(await screen.findByRole("button", { name: "Pause" })).toBeInTheDocument();
    expect(screen.getByTestId("pending-chat-prompt")).toHaveAttribute("data-prompt-kind", "approval");
  });

  it("renders the submitted Markdown during pending Save", async () => {
    renderFixture();
    await selectFixture("pending-save");
    const user = userEvent.setup();
    const readField = await screen.findByRole("textbox", { name: "Goal" });
    await user.click(readField);
    const field = await screen.findByRole("textbox", { name: "Goal" });
    await user.clear(field);
    const objective = "Submitted while the request is pending";
    await user.type(field, objective);
    await user.click(screen.getByTestId("goal-save"));

    expect(await screen.findByText(objective)).toBeInTheDocument();
    await waitFor(() => {
      expect(screen.queryByTestId("goal-save")).not.toBeInTheDocument();
    });
    expect(screen.getByRole("button", { name: "Resolve pending Goal action" })).toBeInTheDocument();
  });
});
