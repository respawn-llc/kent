import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, expect, it } from "vitest";

import { appI18n, initializeI18n } from "@/i18n";
import { usePathFormatter } from "@/app-facade";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { createTaskDetailFixture } from "@/test-support/task-detail";
import { PromptAccessTargets } from "@/ui";
import { TaskExecutionTargetFacts } from "./TaskExecutionTargetFacts";

beforeAll(initializeI18n);

it("collapses read-only Task paths but copies their absolute values", async () => {
  const user = userEvent.setup();
  const fixture = await createTaskDetailFixture();
  const home = "/Users/engineer";
  const source = `${home}/project`;
  const worktree = `${home}/.kent/worktrees/project/803`;
  render(
    <TestAppProviders services={createTestServices([], undefined, { homePath: home, platform: "macos" })}>
      <TaskExecutionTargetFacts
        detail={{
          ...fixture,
          sourceWorkspace: { ...fixture.sourceWorkspace, rootPath: source },
          worktreePath: worktree,
        }}
      />
    </TestAppProviders>,
  );
  expect(screen.getByText("~/project")).toBeInTheDocument();
  expect(screen.getByText("~/.kent/worktrees/project/803")).toBeInTheDocument();
  await user.click(screen.getByRole("button", { name: appI18n.t("task.copySourceWorkspacePath") }));
  await waitFor(async () => {
    expect(await navigator.clipboard.readText()).toBe(source);
  });
  await user.click(screen.getByRole("button", { name: appI18n.t("task.copyManagedWorktreePath") }));
  await waitFor(async () => {
    expect(await navigator.clipboard.readText()).toBe(worktree);
  });
});

it("collapses structured approval targets without changing their values", () => {
  const target = { requestedPath: "/Users/engineer/input", resolvedPath: "/Users/engineer/resolved" };
  render(
    <TestAppProviders
      services={createTestServices([], undefined, { homePath: "/Users/engineer", platform: "macos" })}
    >
      <ApprovalTargets target={target} />
    </TestAppProviders>,
  );
  expect(screen.getByText("- ~/input → ~/resolved")).toBeInTheDocument();
  expect(target.resolvedPath).toBe("/Users/engineer/resolved");
});

function ApprovalTargets({ target }: Readonly<{ target: { requestedPath: string; resolvedPath: string } }>) {
  const formatPath = usePathFormatter();
  return <PromptAccessTargets targets={[target]} formatPath={formatPath} />;
}
