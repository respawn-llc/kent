import { fireEvent, render, screen } from "@testing-library/react";
import { beforeEach, expect, it, mock, vi } from "bun:test";

import type { WorkflowRecord } from "@/api";

const fixture = {
  edit: vi.fn(),
  open: vi.fn(),
};

await mock.module("react-i18next", () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}));

await mock.module("@/shared/workflow-deletion", () => ({
  useWorkflowDeleteLauncher: () => ({
    dialog: null,
    disabled: false,
    openWorkflowDelete: vi.fn(),
    opening: false,
    submitting: false,
  }),
}));

const { WorkflowRow } = await import("./WorkflowRow");

beforeEach(() => {
  fixture.edit.mockClear();
  fixture.open.mockClear();
});

it("opens from the whole workflow row while keeping Edit independent", () => {
  render(<WorkflowRow contextActions={{ onEdit: fixture.edit }} onOpen={fixture.open} workflow={workflow} />);

  fireEvent.click(screen.getByRole("button", { name: workflow.name }));
  expect(fixture.open).toHaveBeenCalledTimes(1);

  fireEvent.click(screen.getByRole("button", { name: /workflowLibrary\.editWorkflow/u }));
  expect(fixture.edit).toHaveBeenCalledTimes(1);
  expect(fixture.open).toHaveBeenCalledTimes(1);
});

const workflow: WorkflowRecord = {
  description: "A reusable workflow",
  executionTargetPolicy: { customRef: null, mode: "default_branch" },
  id: "workflow-1",
  name: "Ship it",
  version: 7,
};
