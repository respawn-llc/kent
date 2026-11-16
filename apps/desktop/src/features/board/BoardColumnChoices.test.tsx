import { expect, it, vi } from "bun:test";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import * as Stream from "effect/Stream";
import { QueryClient } from "@tanstack/react-query";
import {
  ValidationErrorCode,
  type BoardColumn,
  type SelectedWorkflowBoard,
  type WorkflowPickerItem,
} from "@/api";
import { ChatPromptPresenceProvider, queryKeys } from "@/app-facade";
import { appI18n } from "@/i18n";
import { TestAppProviders, createTestServices, startupRoutes } from "@/test-support/app-services";
import { TestSidebar } from "@/test-support/sidebar-rendering";
import { BoardRoute } from "./BoardRoute";

function workflow(id: string, name = id): WorkflowPickerItem {
  return {
    id,
    name,
    description: "",
    version: 1,
    isProjectDefault: true,
    validForExecution: true,
    validationErrors: [],
  };
}

function column(
  id: string,
  name: string,
  options: Readonly<{ isBacklog?: boolean; isDone?: boolean; taskCount?: number; sortOrder?: number }> = {},
): BoardColumn {
  return {
    id,
    key: id,
    kind: "agent",
    name,
    assigneeRole: "",
    outputFields: [],
    groupID: null,
    sortOrder: options.sortOrder ?? 0,
    isBacklog: options.isBacklog ?? false,
    isDone: options.isDone ?? false,
    taskCount: options.taskCount ?? 0,
  };
}

function selectedBoard(
  selectedWorkflow: ReturnType<typeof workflow>,
  columns: readonly BoardColumn[],
  projectID = "project-1",
): SelectedWorkflowBoard {
  return {
    projectID,
    projectKey: "KNT",
    projectName: "Project",
    defaultWorkspaceID: "workspace-1",
    attachedWorkspaceCount: 1,
    selectedWorkflow,
    workflows: [selectedWorkflow],
    groups: [],
    columns,
  };
}

function mountBoard(
  initialBoard: SelectedWorkflowBoard,
  initialRequestedWorkflowID: string | undefined = initialBoard.selectedWorkflow.id,
) {
  let response = initialBoard;
  let requestedProjectID = initialBoard.projectID;
  let requestedWorkflowID: string | undefined = initialRequestedWorkflowID;
  let boardVisible = true;
  const services = createTestServices(startupRoutes);
  vi.spyOn(services.api, "getBoard").mockImplementation(async () => response);
  vi.spyOn(services.api, "listProjectLabels").mockImplementation(async (projectID) => ({
    projectID,
    labels: [],
  }));
  vi.spyOn(services.api, "subscribeProject").mockReturnValue(Stream.never);
  const queryClient = new QueryClient({
    defaultOptions: {
      mutations: { retry: false },
      queries: { retry: false },
    },
  });
  const renderRoute = () => (
    <TestAppProviders queryClient={queryClient} services={services}>
      {boardVisible ? (
        <ChatPromptPresenceProvider>
          <TestSidebar>
            <BoardRoute projectId={requestedProjectID} workflowId={requestedWorkflowID} selectedTaskId="" />
          </TestSidebar>
        </ChatPromptPresenceProvider>
      ) : null}
    </TestAppProviders>
  );
  const view = render(renderRoute());

  return {
    services,
    setResponse(next: SelectedWorkflowBoard) {
      response = next;
    },
    setRoute(projectID: string, workflowID: string | undefined) {
      requestedProjectID = projectID;
      requestedWorkflowID = workflowID;
      view.rerender(renderRoute());
    },
    setBoardVisible(visible: boolean) {
      boardVisible = visible;
      view.rerender(renderRoute());
    },
    async refresh() {
      await act(async () => {
        await queryClient.invalidateQueries({
          queryKey: queryKeys.board(requestedProjectID, requestedWorkflowID, { kind: "none" }),
        });
      });
    },
    close() {
      view.unmount();
      queryClient.clear();
    },
  };
}

it("lets a populated column be collapsed from its header", async () => {
  const harness = mountBoard(
    selectedBoard(workflow("workflow-1", "Workflow"), [column("node-1", "Doing", { taskCount: 2 })]),
  );

  try {
    const column = await screen.findByRole("listitem", { name: "Doing" });
    fireEvent.click(within(column).getByRole("button", { name: appI18n.t("app.collapse") }));

    expect(
      within(column).getByRole("button", {
        name: appI18n.t("board.expandColumn", { name: "Doing" }),
      }),
    ).toBeInTheDocument();
  } finally {
    harness.close();
  }
});

it("creates a Backlog Task from a board with a non-ready Workflow", async () => {
  const nonReadyWorkflow = {
    ...workflow("workflow-1"),
    validForExecution: false,
    validationErrors: [
      {
        code: ValidationErrorCode.ASSIGNEE_SELECTION_INAPPLICABLE,
        message: "fixture diagnostic",
        workflowID: "workflow-1",
        nodeID: null,
        transitionGroupID: null,
        edgeID: "edge-done",
        details: {
          fieldName: "role",
          inputName: "role",
          placeholder: ".Params.role",
          providerEdgeID: null,
          role: "coder",
          requiredTool: null,
        },
        relatedIDs: [],
        blocksContext: true,
      },
    ],
  };
  const harness = mountBoard(
    selectedBoard(nonReadyWorkflow, [column("backlog", "Backlog", { isBacklog: true })]),
  );
  const createTask = vi.spyOn(harness.services.api, "createTask").mockResolvedValue({
    id: "task-created",
    shortID: "KNT-42",
    title: "New task",
    workflowID: "workflow-1",
  });
  vi.spyOn(harness.services.api, "listWorkspaces").mockResolvedValue({
    projectID: "project-1",
    offset: 0,
    workspaces: [{ id: "workspace-1", name: "Main", rootPath: "/", isDefault: true }],
    nextOffset: null,
  });

  try {
    const user = userEvent.setup({ delay: null });
    await user.click(await screen.findByRole("button", { name: appI18n.t("board.newTask") }));
    const title = await screen.findByRole("textbox", { name: appI18n.t("task.name") });
    await user.type(title, "Task from non-ready Workflow");
    const submit = screen.getByRole("button", { name: appI18n.t("task.create") });
    await user.click(submit);

    await waitFor(() => {
      expect(createTask).toHaveBeenCalledWith(
        expect.objectContaining({
          projectID: "project-1",
          title: "Task from non-ready Workflow",
          workflowID: "workflow-1",
        }),
      );
    });
  } finally {
    harness.close();
  }
});

it("opens Backlog, the first Active column and populated columns by default", async () => {
  const harness = mountBoard(
    selectedBoard(workflow("workflow-1"), [
      column("backlog", "Backlog", { isBacklog: true }),
      column("first-active", "Doing", { sortOrder: 1 }),
      column("empty", "Review", { sortOrder: 2 }),
      column("populated", "Ready", { sortOrder: 3, taskCount: 2 }),
    ]),
  );

  try {
    await screen.findByRole("listitem", { name: "Ready" });
    const collapseName = appI18n.t("app.collapse");
    const expandName = (name: string) => appI18n.t("board.expandColumn", { name });

    expect(
      within(screen.getByRole("listitem", { name: "Backlog" })).getByRole("button", { name: collapseName }),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("listitem", { name: "Doing" })).getByRole("button", { name: collapseName }),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("listitem", { name: "Review" })).getByRole("button", {
        name: expandName("Review"),
      }),
    ).toBeInTheDocument();
    expect(
      within(screen.getByRole("listitem", { name: "Ready" })).getByRole("button", { name: collapseName }),
    ).toBeInTheDocument();
  } finally {
    harness.close();
  }
});

it("keeps explicit column choices when Task counts change", async () => {
  const workflowItem = workflow("workflow-1");
  const harness = mountBoard(
    selectedBoard(workflowItem, [
      column("backlog", "Backlog", { isBacklog: true }),
      column("first-active", "Doing", { sortOrder: 1 }),
      column("empty", "Review", { sortOrder: 2 }),
      column("populated", "Ready", { sortOrder: 3, taskCount: 2 }),
    ]),
  );

  try {
    await screen.findByRole("listitem", { name: "Ready" });
    const collapseName = appI18n.t("app.collapse");
    const expandName = (name: string) => appI18n.t("board.expandColumn", { name });
    const review = within(screen.getByRole("listitem", { name: "Review" }));
    const ready = within(screen.getByRole("listitem", { name: "Ready" }));

    fireEvent.click(review.getByRole("button", { name: expandName("Review") }));
    fireEvent.click(ready.getByRole("button", { name: collapseName }));

    harness.setResponse(
      selectedBoard(workflowItem, [
        column("backlog", "Backlog", { isBacklog: true }),
        column("first-active", "Doing", { sortOrder: 1 }),
        column("empty", "Review", { sortOrder: 2, taskCount: 3 }),
        column("populated", "Ready", { sortOrder: 3 }),
      ]),
    );
    await harness.refresh();

    expect(review.getByRole("button", { name: collapseName })).toBeInTheDocument();
    expect(ready.getByRole("button", { name: expandName("Ready") })).toBeInTheDocument();

    harness.setResponse(
      selectedBoard(workflowItem, [
        column("backlog", "Backlog", { isBacklog: true }),
        column("first-active", "Doing", { sortOrder: 1 }),
        column("empty", "Review", { sortOrder: 2 }),
        column("populated", "Ready", { sortOrder: 3, taskCount: 2 }),
      ]),
    );
    await harness.refresh();

    expect(review.getByRole("button", { name: collapseName })).toBeInTheDocument();
    expect(ready.getByRole("button", { name: expandName("Ready") })).toBeInTheDocument();
  } finally {
    harness.close();
  }
});

it("resets choices when refreshed responses resolve a different Workflow", async () => {
  const workflowA = workflow("workflow-a", "Workflow A");
  const workflowB = workflow("workflow-b", "Workflow B");
  const boardA = (taskCount: number) =>
    selectedBoard(workflowA, [
      column("backlog-a", "Backlog A", { isBacklog: true }),
      column("active-a", "Active A", { sortOrder: 1 }),
      column("review-a", "Review A", { sortOrder: 2, taskCount }),
    ]);
  const boardB = selectedBoard(workflowB, [
    column("backlog-b", "Backlog B", { isBacklog: true }),
    column("active-b", "Active B", { sortOrder: 1 }),
    column("review-b", "Review B", { sortOrder: 2 }),
  ]);
  const harness = mountBoard(boardA(0), undefined);
  const collapseName = appI18n.t("app.collapse");
  const expandName = (name: string) => appI18n.t("board.expandColumn", { name });

  try {
    const reviewA = within(await screen.findByRole("listitem", { name: "Review A" }));
    fireEvent.click(reviewA.getByRole("button", { name: expandName("Review A") }));
    expect(reviewA.getByRole("button", { name: collapseName })).toBeInTheDocument();

    harness.setResponse(boardA(1));
    await harness.refresh();
    expect(reviewA.getByRole("button", { name: collapseName })).toBeInTheDocument();

    harness.setResponse(boardB);
    await harness.refresh();
    const reviewB = within(await screen.findByRole("listitem", { name: "Review B" }));
    expect(reviewB.getByRole("button", { name: expandName("Review B") })).toBeInTheDocument();
    fireEvent.click(reviewB.getByRole("button", { name: expandName("Review B") }));
    expect(reviewB.getByRole("button", { name: collapseName })).toBeInTheDocument();

    harness.setResponse(boardA(0));
    await harness.refresh();
    const returnedReviewA = within(await screen.findByRole("listitem", { name: "Review A" }));
    expect(returnedReviewA.getByRole("button", { name: expandName("Review A") })).toBeInTheDocument();
  } finally {
    harness.close();
  }
});

it("resets choices after requested board changes and after leaving the board", async () => {
  const workflowA = workflow("workflow-a", "Workflow A");
  const workflowB = workflow("workflow-b", "Workflow B");
  const boardA = selectedBoard(workflowA, [
    column("backlog", "Backlog", { isBacklog: true }),
    column("active", "Active", { sortOrder: 1 }),
    column("review", "Review A", { sortOrder: 2 }),
  ]);
  const boardB = selectedBoard(workflowB, [
    column("backlog", "Backlog", { isBacklog: true }),
    column("active", "Active", { sortOrder: 1 }),
    column("review", "Review B", { sortOrder: 2 }),
  ]);
  const otherProjectBoard = selectedBoard(
    workflowB,
    [
      column("backlog", "Backlog", { isBacklog: true }),
      column("active", "Active", { sortOrder: 1 }),
      column("review", "Review in Project 2", { sortOrder: 2 }),
    ],
    "project-2",
  );
  const harness = mountBoard(boardA);
  const expandName = (name: string) => appI18n.t("board.expandColumn", { name });

  try {
    const reviewA = within(await screen.findByRole("listitem", { name: "Review A" }));
    fireEvent.click(reviewA.getByRole("button", { name: expandName("Review A") }));

    harness.setResponse(boardB);
    harness.setRoute(boardB.projectID, workflowB.id);
    const reviewB = within(await screen.findByRole("listitem", { name: "Review B" }));
    expect(reviewB.getByRole("button", { name: expandName("Review B") })).toBeInTheDocument();
    fireEvent.click(reviewB.getByRole("button", { name: expandName("Review B") }));

    harness.setResponse(otherProjectBoard);
    harness.setRoute(otherProjectBoard.projectID, workflowB.id);
    const reviewInProject2 = within(await screen.findByRole("listitem", { name: "Review in Project 2" }));
    expect(
      reviewInProject2.getByRole("button", { name: expandName("Review in Project 2") }),
    ).toBeInTheDocument();
    fireEvent.click(reviewInProject2.getByRole("button", { name: expandName("Review in Project 2") }));

    harness.setBoardVisible(false);
    harness.setBoardVisible(true);
    const returnedReviewInProject2 = within(
      await screen.findByRole("listitem", { name: "Review in Project 2" }),
    );
    expect(
      returnedReviewInProject2.getByRole("button", {
        name: expandName("Review in Project 2"),
      }),
    ).toBeInTheDocument();
  } finally {
    harness.close();
  }
});
