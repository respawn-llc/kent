import { expect, it, vi } from "bun:test";
import { fireEvent, render, screen } from "@testing-library/react";
import { useState } from "react";

import { appI18n } from "@/i18n";
import { worktreeCommandFixtureRoutes } from "@/test-support/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { WorktreeDeleteButton } from "./WorktreeDeleteButton";

it("confirms with the selected branch choice and remembers it across popup openings", async () => {
  const services = createTestServices(worktreeCommandFixtureRoutes());
  const confirm = vi.fn();
  function ListDestination() {
    const [open, setOpen] = useState(true);
    const [includeBranch, setIncludeBranch] = useState(true);
    return (
      <WorktreeDeleteButton
        open={open}
        onOpenChange={setOpen}
        includeBranch={includeBranch}
        onIncludeBranchChange={setIncludeBranch}
        cleanliness={null}
        canIncludeBranch
        confirm={confirm}
      />
    );
  }
  render(
    <TestAppProviders services={services} skipAnimations>
      <ListDestination />
    </TestAppProviders>,
  );
  const checkbox = await screen.findByRole("checkbox");
  expect(checkbox).toBeChecked();
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.confirm") }));
  expect(confirm).toHaveBeenCalledTimes(1);
  fireEvent.click(checkbox);
  fireEvent.keyDown(checkbox, { key: "Escape" });
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.delete") }));
  expect(await screen.findByRole("checkbox")).not.toBeChecked();
  fireEvent.click(screen.getByRole("button", { name: appI18n.t("chat.worktree.confirm") }));
  expect(confirm).toHaveBeenCalledTimes(2);
  expect(screen.queryByRole("button", { name: appI18n.t("app.close") })).not.toBeInTheDocument();
});
