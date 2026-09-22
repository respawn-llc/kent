import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement, ReactNode } from "react";

import { RpcError, rpcErrorCodes, type WorkflowRecord } from "@/api";
import { NewTaskForm } from "@/features/tasks";
import { LinkWorkflowSidebar } from "@/features/workflows";
import { appI18n, initializeI18n } from "@/i18n";
import type * as TaskDependenciesModule from "@/shared/task-dependencies";
import { createTestSidebarNavigator } from "@/test-support/sidebar";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import type * as UiModule from "@/ui";
import type * as WorkflowLibrary from "@/shared/workflow-library";
const fixture = vi.hoisted<{
  createError: Error | null;
}>(() => ({ createError: null }));
vi.mock("@/shared/labels", () => ({
  LabelChooser: () => null,
  orderedAssignedLabels: () => [],
  ProjectLabelsProvider: ({ children }: Readonly<{ children: ReactNode }>) => <>{children}</>,
  useProjectLabelCatalog: () => ({ data: { labels: [] } }),
}));
vi.mock("@/shared/task-mutations", () => ({
  useCreateTask: () => ({ error: fixture.createError, isPending: false, submit: vi.fn() }),
}));
vi.mock("@/shared/task-dependencies", async (importOriginal) => ({
  ...(await importOriginal<typeof TaskDependenciesModule>()),
  DependenciesArea: () => null,
}));
vi.mock("@/shared/workflow-library", async (original) => ({
  ...(await original<typeof WorkflowLibrary>()),
  WorkflowActionsContextMenu: ({ children }: { children: (loading: boolean) => ReactElement }) =>
    children(false),
}));
vi.mock("@/ui", async (original) => ({
  ...(await original<typeof UiModule>()),
  VirtualizedInfiniteList: ({
    items,
    renderItem,
  }: {
    items: readonly WorkflowRecord[];
    renderItem: (item: WorkflowRecord) => ReactNode;
  }) => (
    <>
      {items.map((item) => (
        <div key={item.id}>{renderItem(item)}</div>
      ))}
    </>
  ),
}));

const missing = new RpcError({ code: rpcErrorCodes.projectNotFound, message: "gone", method: "mutation" });

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
    expect(navigator.back).toHaveBeenCalledOnce();
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
      expect(navigator.back).toHaveBeenCalledOnce();
    });
  });
});
