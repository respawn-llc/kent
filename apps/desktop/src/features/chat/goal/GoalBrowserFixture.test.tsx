import userEvent from "@testing-library/user-event";
import { render, screen, waitFor } from "@testing-library/react";

import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { GoalBrowserFixture } from "./GoalBrowserFixture";

function renderFixture() {
  render(
    <TestAppProviders services={createTestServices([])}>
      <GoalBrowserFixture />
    </TestAppProviders>,
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
    expect(screen.getByTestId("goal-question-picker")).toBeInTheDocument();
    expect(await screen.findByRole("button", { name: "Pause" })).toBeInTheDocument();
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
