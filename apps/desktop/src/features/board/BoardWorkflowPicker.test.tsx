import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeAll, expect, it, vi } from "bun:test";

import type { WorkflowPickerItem } from "@/api";
import { appI18n, initializeI18n } from "@/i18n";
import { TestAppProviders, createTestServices } from "@/test-support/app-services";
import { BoardWorkflowPicker } from "./BoardWorkflowPicker";

beforeAll(async () => {
  await initializeI18n();
});

const workflows = [
  {
    id: "workflow-1",
    name: "Main",
    description: "",
    isProjectDefault: true,
    validForExecution: true,
    validationErrors: [],
    version: 1,
  },
  {
    id: "workflow-2",
    name: "Review",
    description: "",
    isProjectDefault: false,
    validForExecution: true,
    validationErrors: [],
    version: 1,
  },
] as const satisfies readonly WorkflowPickerItem[];

function fixture() {
  const onEditWorkflow = vi.fn();
  const onSelectWorkflow = vi.fn();
  const onOpenTasks = vi.fn();
  const activeWorkflow = workflows[0];
  render(
    <TestAppProviders services={createTestServices([])}>
      <BoardWorkflowPicker
        activeWorkflow={activeWorkflow}
        onEditWorkflow={onEditWorkflow}
        onLinkWorkflow={vi.fn()}
        onOpenTasks={onOpenTasks}
        onSelectWorkflow={onSelectWorkflow}
        workflows={workflows}
      />
    </TestAppProviders>,
  );
  return { onEditWorkflow, onSelectWorkflow, onOpenTasks };
}

it.each([...workflows])("opens the editor for $name without selecting that workflow", async (workflow) => {
  const user = userEvent.setup({ delay: null });
  const actions = fixture();
  await user.click(screen.getByRole("button", { name: workflows[0].name }));
  await user.click(
    screen.getByRole("button", { name: appI18n.t("board.editWorkflow", { name: workflow.name }) }),
  );
  expect(actions.onEditWorkflow).toHaveBeenCalledWith(workflow.id);
  expect(actions.onSelectWorkflow).not.toHaveBeenCalled();
  expect(actions.onOpenTasks).not.toHaveBeenCalled();
  expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
});

it("keeps workflow selection and Tasks navigation separate from editing", async () => {
  const user = userEvent.setup({ delay: null });
  const actions = fixture();
  await user.click(screen.getByRole("button", { name: workflows[0].name }));
  await user.click(screen.getByRole("radio", { name: workflows[1].name }));
  expect(actions.onSelectWorkflow).toHaveBeenCalledWith(workflows[1].id);
  expect(actions.onEditWorkflow).not.toHaveBeenCalled();
  expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();

  await user.click(screen.getByRole("button", { name: workflows[0].name }));
  await user.click(screen.getByRole("button", { name: appI18n.t("home.prototype.tasks") }));
  expect(actions.onOpenTasks).toHaveBeenCalledTimes(1);
  expect(actions.onEditWorkflow).not.toHaveBeenCalled();
  expect(screen.queryByRole("radiogroup")).not.toBeInTheDocument();
});
