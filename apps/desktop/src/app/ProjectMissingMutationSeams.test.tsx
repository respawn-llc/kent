import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeAll, beforeEach, describe, expect, it, mock, vi } from "bun:test";
import type { ReactNode } from "react";

import { RpcError } from "@/api";
import { appI18n, initializeI18n } from "@/i18n";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
const fixture: {
  createError: Error | null;
} = { createError: null };
const actualTaskDependencies = await import("@/shared/task-dependencies");
const actualWorkflowLibrary = await import("@/shared/workflow-library");
const actualUi = await import("@/ui");
const workflowRendering = await import("@/test-support/workflow-rendering");

await mock.module("@/shared/labels", () => ({
  LabelChooser: () => null,
  orderedAssignedLabels: () => [],
  ProjectLabelsProvider: ({ children }: Readonly<{ children: ReactNode }>) => <>{children}</>,
  useProjectLabelCatalog: () => ({ data: { labels: [] } }),
}));
await mock.module("@/shared/task-mutations", () => ({
  useCreateTask: () => ({ error: fixture.createError, isPending: false, submit: vi.fn() }),
}));
await mock.module("@/shared/task-dependencies", () => ({
  ...actualTaskDependencies,
  DependenciesArea: () => null,
}));
await mock.module("@/shared/workflow-library", () => ({
  ...actualWorkflowLibrary,
  WorkflowActionsContextMenu: workflowRendering.WorkflowActionsContextMenuStub,
}));
await mock.module("@/ui", () => ({
  ...actualUi,
  VirtualizedInfiniteList: workflowRendering.WorkflowListStub,
}));
const { NewTaskForm } = await import("@/features/tasks");
const { LinkWorkflowSidebar } = await import("@/features/workflows");

const missing = new RpcError({ code: "project_not_found", message: "gone", method: "mutation" });

beforeAll(async () => initializeI18n());
beforeEach(() => Object.assign(fixture, { createError: null }));

describe("Project-missing mutation seams", () => {
  it("dismisses New Task when creation reports the Project missing", async () => {
    fixture.createError = missing;
    const services = createTestServices([]);
    vi.spyOn(services.api, "listWorkspaces").mockResolvedValue({
      projectID: "project-1",
      offset: 0,
      workspaces: [],
      nextOffset: null,
    });
    const navigator = createTestSidebarNavigator();
    render(
      <TestAppProviders services={services}>
        <NewTaskForm
          boardQueryWorkflowID="workflow-1"
          navigator={navigator}
          projectID="project-1"
          workflowID="workflow-1"
        />
      </TestAppProviders>,
    );
    expect(navigator.back).toHaveBeenCalledTimes(1);
  });

  it("backs out of Link Workflow when linking reports the Project missing", async () => {
    const services = createTestServices([]);
    vi.spyOn(services.api, "listWorkflows").mockResolvedValue({
      workflows: [
        {
          id: "workflow-1",
          name: "Workflow",
          description: "",
          version: 1,
          executionTargetPolicy: { mode: "default_branch", customRef: null },
        },
      ],
      nextOffset: null,
    });
    vi.spyOn(services.api, "listProjectWorkflowLinks").mockResolvedValue([]);
    vi.spyOn(services.api, "linkWorkflowToProject").mockRejectedValue(missing);
    const navigator = createTestSidebarNavigator();
    render(
      <TestAppProviders services={services}>
        <LinkWorkflowSidebar
          creating={false}
          navigator={navigator}
          onCreated={vi.fn()}
          onLinked={vi.fn()}
          projectID="project-1"
        />
      </TestAppProviders>,
    );
    fireEvent.click(await screen.findByRole("button", { name: appI18n.t("workflowLibrary.link") }));
    await waitFor(() => {
      expect(navigator.back).toHaveBeenCalledTimes(1);
    });
  });
});
