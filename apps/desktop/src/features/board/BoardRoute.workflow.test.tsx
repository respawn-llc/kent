import { createBrowserNativeBridge, type NativeDialogWindowOptions } from "@app/native-bridge";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { vi } from "vitest";

import type { JsonValue } from "../../api/json";
import { App } from "../../App";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";
import {
  boardColumn,
  boardNodeCardsResponse,
  type BoardRouteCard,
  boardResponse,
  boardRoutes,
  type BoardRouteWorkflow,
  expandBoardHoverMenu,
  firstBoardCard,
  installBoardRouteLifecycle,
  isObject,
  nativeDialogBridge,
  TestDataTransfer,
  workflow,
  workflowDefinitionResponse,
} from "./boardRouteTestHarness";

describe("BoardRoute workflows", () => {
  installBoardRouteLifecycle();

  it("restores the last valid project workflow route on relaunch", async () => {
    window.history.pushState(null, "", "/");
    localStorage.setItem(
      "desktop.lastProjectRoute",
      JSON.stringify({ projectId: "project-1", workflowId: "workflow-1" }),
    );
    const services = createTestServices([...startupRoutes, ...boardRoutes()]);

    render(<App services={services} />);

    await screen.findByTestId("app-chrome-title");
    expect(window.location.pathname).toBe("/projects/project-1");
    expect(window.location.search).toContain("workflowId=workflow-1");
  });

  it("renders Main SWE groups after the Implementation column", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const mainSWEWorkflow = { ...workflow, display_name: "Main SWE" };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(
        {
          board: {
            ...boardResponse.board,
            selected_workflow: mainSWEWorkflow,
            workflows: [mainSWEWorkflow],
            groups: [
              {
                group_id: "group-review",
                key: "review",
                display_name: "Review group",
                sort_order: 1,
                node_ids: ["review", "qa"],
              },
            ],
            columns: [
              boardColumn({
                displayName: "Backlog",
                isBacklog: true,
                key: "backlog",
                kind: "start",
                nodeID: "backlog",
                sortOrder: 0,
                taskCount: 1,
              }),
              boardColumn({
                assigneeRole: "coder",
                displayName: "Implementation",
                key: "implementation",
                kind: "agent",
                nodeID: "implementation",
                sortOrder: 1,
              }),
              boardColumn({
                assigneeRole: "reviewer",
                displayName: "Review",
                groupID: "group-review",
                key: "review",
                kind: "agent",
                nodeID: "review",
                sortOrder: 2,
                taskCount: 1,
              }),
              boardColumn({
                assigneeRole: "qa",
                displayName: "QA",
                groupID: "group-review",
                key: "qa",
                kind: "agent",
                nodeID: "qa",
                sortOrder: 3,
              }),
              boardColumn({
                displayName: "Done",
                isDone: true,
                key: "done",
                kind: "terminal",
                nodeID: "done",
                sortOrder: 99,
              }),
            ],
          },
        },
        {
          backlog: { cards: boardResponse.board.cards },
          done: { cards: [] },
          implementation: { cards: [] },
          qa: { cards: [] },
          review: { cards: [] },
        },
      ),
    ]);

    render(<App services={services} />);

    const backlog = await screen.findByRole("heading", { name: "Backlog" });
    const implementation = await screen.findByRole("heading", { name: "Implementation" });
    const group = await screen.findByRole("heading", { name: "Review group" });
    const done = await screen.findByRole("heading", { name: "Done" });

    expect(backlog).toAppearBefore(implementation);
    expect(implementation).toAppearBefore(group);
    expect(group).toAppearBefore(done);
  });

  it("hides a group header while all grouped columns are collapsed", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const mainSWEWorkflow = { ...workflow, display_name: "Main SWE" };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(
        {
          board: {
            ...boardResponse.board,
            selected_workflow: mainSWEWorkflow,
            workflows: [mainSWEWorkflow],
            groups: [
              {
                group_id: "group-review",
                key: "review",
                display_name: "Review group",
                sort_order: 1,
                node_ids: ["review", "qa"],
              },
            ],
            columns: [
              boardColumn({
                displayName: "Backlog",
                isBacklog: true,
                key: "backlog",
                kind: "start",
                nodeID: "backlog",
                sortOrder: 0,
                taskCount: 1,
              }),
              boardColumn({
                assigneeRole: "coder",
                displayName: "Implementation",
                key: "implementation",
                kind: "agent",
                nodeID: "implementation",
                sortOrder: 1,
              }),
              boardColumn({
                assigneeRole: "reviewer",
                displayName: "Review",
                groupID: "group-review",
                key: "review",
                kind: "agent",
                nodeID: "review",
                sortOrder: 2,
              }),
              boardColumn({
                assigneeRole: "qa",
                displayName: "QA",
                groupID: "group-review",
                key: "qa",
                kind: "agent",
                nodeID: "qa",
                sortOrder: 3,
              }),
              boardColumn({
                displayName: "Done",
                isDone: true,
                key: "done",
                kind: "terminal",
                nodeID: "done",
                sortOrder: 99,
                taskCount: 1,
              }),
            ],
          },
        },
        {
          backlog: { cards: boardResponse.board.cards },
          done: { cards: [] },
          implementation: { cards: [] },
          qa: { cards: [] },
          review: { cards: [] },
        },
      ),
    ]);

    render(<App services={services} />);

    await screen.findByRole("heading", { hidden: true, name: "Review group" });
    expect(screen.getByTestId("kanban-group-header-group-review")).toHaveAttribute("aria-hidden", "true");

    fireEvent.click(await screen.findByRole("button", { name: "Expand Review" }));

    expect(screen.getByTestId("kanban-group-header-group-review")).not.toHaveAttribute("aria-hidden");
  });

  it("renders the shared full-page empty state when project has no workflows", async () => {
    window.history.pushState(null, "", "/projects/project-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes({
        board: {
          ...boardResponse.board,
          workflows: [],
        },
      }),
      {
        method: "workflow.listProjectLinks",
        result: {
          links: [
            {
              id: "link-1",
              project_id: "project-1",
              workflow_id: "workflow-1",
              default: true,
            },
          ],
        },
      },
      { method: "workflow.get", result: workflowDefinitionResponse },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
    ]);

    render(<App services={services} />);

    expect(await screen.findByTestId("empty-state")).toBeInTheDocument();
    expect(screen.getByTestId("empty-state-icon")).not.toBeEmptyDOMElement();
    expect(screen.getByRole("button", { name: "Link workflow" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create workflow" })).toBeInTheDocument();
  });

  it("disables no-workflow actions while disconnected", async () => {
    window.history.pushState(null, "", "/projects/project-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes({
        board: {
          ...boardResponse.board,
          workflows: [],
        },
      }),
    ]);
    services.transport.connection.set("disconnected", "offline");

    render(<App services={services} />);

    expect(await screen.findByRole("button", { name: "Link workflow" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Create workflow" })).toBeDisabled();
  });

  it("creates and links the first project workflow from the no-workflow empty state", async () => {
    window.history.pushState(null, "", "/projects/project-1");
    const createdWorkflow = {
      id: "workflow-created",
      name: "Created Workflow",
      description: "",
      version: 1,
    };
    const createdPickerWorkflow = {
      ...workflow,
      workflow_id: "workflow-created",
      display_name: "Created Workflow",
      is_project_default: true,
    };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes({
        board: {
          ...boardResponse.board,
          selected_workflow: createdPickerWorkflow,
          workflows: [],
        },
      }),
      {
        method: "workflow.createAndLinkProject",
        result: {
          workflow: createdWorkflow,
          link: {
            id: "link-created",
            project_id: "project-1",
            workflow_id: "workflow-created",
            default: true,
          },
        },
      },
      {
        method: "workflow.listProjectLinks",
        result: {
          links: [
            {
              id: "link-created",
              project_id: "project-1",
              workflow_id: "workflow-created",
              default: true,
            },
          ],
        },
      },
      {
        method: "workflow.get",
        result: {
          definition: {
            ...workflowDefinitionResponse.definition,
            workflow: createdWorkflow,
          },
        },
      },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
    ]);

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Create workflow" }));
    fireEvent.change(await screen.findByLabelText("Workflow name"), {
      target: { value: "Created Workflow" },
    });
    fireEvent.click(
      within(screen.getByTestId("app-sidebar-host")).getByRole("button", { name: "Create workflow" }),
    );

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.createAndLinkProject",
        params: {
          name: "Created Workflow",
          description: "",
          project_id: "project-1",
          default_policy: "if_project_has_none",
        },
      });
    });
    await waitFor(() => {
      expect(window.location.pathname).toBe("/workflows/workflow-created/editor");
    });
    expect(window.location.search).toContain("projectId=project-1");
  });

  it("falls back to the project name for the chrome title when workflow name is blank", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes({
        board: {
          ...boardResponse.board,
          selected_workflow: { ...workflow, display_name: "" },
          workflows: [{ ...workflow, display_name: "" }],
        },
      }),
    ]);

    render(<App services={services} />);

    await screen.findByTestId("app-chrome-title");
  });

  it("places the chrome title directly after the icon row on macOS", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices(
      [...startupRoutes, ...boardRoutes()],
      createBrowserNativeBridge({ platform: "macos" }),
    );

    render(<App services={services} />);

    await screen.findByTestId("app-chrome-title");
    const chromeNavigation = screen.getByTestId("app-chrome-navigation");
    expect(within(chromeNavigation).getByTestId("app-chrome-title")).toBeInTheDocument();
  });

  it("lets invalid workflows create Backlog tasks while blocking execution moves", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const invalidWorkflow = {
      ...workflow,
      valid_for_task_creation: false,
      validation_errors: [
        {
          code: "workflow.validation.invalid_start_outgoing_shape",
          message: "task start requires exactly one outgoing transition group",
          node_id: "backlog",
          edge_id: "",
          blocks_context: true,
        },
        {
          code: "workflow.validation.non_terminal_cannot_reach_terminal",
          message: "Node Write focused tests cannot reach a terminal",
          node_id: "node-1",
          edge_id: "",
          blocks_context: true,
        },
        {
          code: "workflow.validation.node_unreachable_from_start",
          message: "Node Review changes not reachable",
          node_id: "node-2",
          edge_id: "",
          blocks_context: true,
        },
      ],
    };
    const opened: NativeDialogWindowOptions[] = [];
    const services = createTestServices(
      [
        ...startupRoutes,
        ...boardRoutes({
          board: {
            ...boardResponse.board,
            selected_workflow: invalidWorkflow,
            workflows: [invalidWorkflow],
            groups: [],
            columns: [
              {
                node: { node_id: "backlog", key: "backlog", kind: "start", display_name: "Backlog" },
                group_id: "",
                sort_order: 0,
                is_backlog: true,
                is_done: false,
                task_count: 0,
              },
              {
                node: { node_id: "done", key: "done", kind: "terminal", display_name: "Done" },
                group_id: "",
                sort_order: 1,
                is_backlog: false,
                is_done: true,
                task_count: 0,
              },
            ],
          },
        }),
        { method: "workflow.task.start", result: {} },
      ],
      nativeDialogBridge(opened),
    );

    render(<App services={services} />);

    expect(await screen.findByRole("heading", { name: "Backlog" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Done" })).toBeInTheDocument();
    const issues = screen.getByRole("complementary", { name: "Workflow issues" });
    expect(within(issues).getAllByRole("listitem")).toHaveLength(3);
    fireEvent.click(within(issues).getByRole("button", { name: "Collapse" }));
    const expandButton = screen.getByRole("button", { name: "Expand" });
    expect(expandButton).toBeInTheDocument();
    expect(screen.getByRole("article", { name: "Write focused tests" })).toHaveAttribute("draggable", "true");

    const card = screen.getByRole("article", { name: "Write focused tests" });
    const doneColumn = screen.getByRole("listitem", { name: "Done" });
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    expect(doneColumn).toHaveAttribute("data-drop-state", "blocked");
    fireEvent.drop(doneColumn, { dataTransfer });

    expect(services.transport.calls.some((call) => call.method === "workflow.task.start")).toBe(false);
    expect(services.transport.calls.some((call) => call.method === "workflow.task.move")).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: "New Task" }));
    expect(await screen.findByRole("complementary", { name: "Create Backlog task" })).toHaveAttribute(
      "data-mode",
      "overlay",
    );
    expect(opened).toHaveLength(0);
  });

  it("expands the bottom-left board menu with workflow selection", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([...startupRoutes, ...boardRoutes()]);

    render(<App services={services} />);

    const menu = await screen.findByRole("navigation");
    vi.useFakeTimers();
    expect(screen.getByRole("button", { name: "New Task" })).toBeInTheDocument();

    fireEvent.mouseEnter(menu);

    expect(screen.getByRole("heading", { name: "Workflows" })).toBeInTheDocument();
    expect(
      within(screen.getByTestId("board-hover-menu-header")).getByRole("button", {
        name: "Link workflow",
      }),
    ).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Delivery" })).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Edit workflow Delivery" })).toBeInTheDocument();

    fireEvent.mouseLeave(menu);
    act(() => {
      vi.advanceTimersByTime(500);
    });

    fireEvent.mouseEnter(menu);
  });

  it("does not keep stale node-card pages after workflow switch", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const workflow2: BoardRouteWorkflow = { ...workflow, workflow_id: "workflow-2", display_name: "Ops" };
    const workflow2Card: BoardRouteCard = {
      ...firstBoardCard(),
      task_id: "task-2",
      title: "Second workflow task",
      workflow_id: "workflow-2",
    };
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.board.get",
        handler: (params: JsonValue) => {
          const requestedWorkflowID =
            isObject(params) && typeof params.workflow_id === "string" ? params.workflow_id : "";
          return {
            board: {
              ...boardResponse.board,
              selected_workflow: requestedWorkflowID === "workflow-2" ? workflow2 : workflow,
              workflows: [workflow, workflow2],
            },
          };
        },
      },
      {
        method: "workflow.board.nodeCards.list",
        handler: (params: JsonValue) => {
          const nodeID = isObject(params) && typeof params.node_id === "string" ? params.node_id : "";
          const workflowID =
            isObject(params) && typeof params.workflow_id === "string" ? params.workflow_id : "";
          return boardNodeCardsResponse(
            nodeID,
            workflowID === "workflow-2" ? [workflow2Card] : [firstBoardCard()],
            "",
          );
        },
      },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("article", { name: "Write focused tests" })).toBeInTheDocument();
    fireEvent.mouseEnter(screen.getByRole("navigation"));
    fireEvent.click(await screen.findByRole("button", { name: "Ops" }));

    expect(await screen.findByRole("article", { name: "Second workflow task" })).toBeInTheDocument();
    expect(screen.queryByRole("article", { name: "Write focused tests" })).not.toBeInTheDocument();
    expect(services.transport.calls).toContainEqual({
      method: "workflow.board.nodeCards.list",
      params: {
        project_id: "project-1",
        workflow_id: "workflow-2",
        node_id: "backlog",
        page_size: 100,
        page_token: "",
      },
    });
  });

  it("opens workflow editor from workflow menu without nesting interactive controls", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const workflow2: BoardRouteWorkflow = { ...workflow, workflow_id: "workflow-2", display_name: "Ops" };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes({
        board: {
          ...boardResponse.board,
          workflows: [workflow, workflow2],
        },
      }),
    ]);

    render(<App services={services} />);

    await screen.findByRole("heading", { name: "Core" });
    fireEvent.mouseEnter(screen.getByRole("navigation"));
    const menu = await screen.findByTestId("board-hover-menu-workflows");
    const editWorkflow = within(menu).getByRole("button", { name: "Edit workflow Delivery" });

    expect(within(menu).getByRole("button", { name: "Delivery" })).toBeVisible();
    expect(menu).not.toHaveAttribute("inert");
    expect(menu).toHaveAttribute("aria-hidden", "false");
    fireEvent.click(editWorkflow);
    await waitFor(() => {
      expect(window.location.pathname).toBe("/workflows/workflow-1/editor");
    });
    expect(window.location.search).toContain("projectId=project-1");
  });

  it("links reusable workflows from the board workflow menu through the sidebar", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const workflow2: BoardRouteWorkflow = { ...workflow, workflow_id: "workflow-2", display_name: "Ops" };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes({
        board: {
          ...boardResponse.board,
          workflows: [workflow, workflow2],
        },
      }),
      {
        method: "workflow.list",
        result: {
          workflows: [
            {
              id: "workflow-1",
              name: "Delivery",
              description: "",
              version: 1,
            },
            { id: "workflow-2", name: "Ops", description: "", version: 1 },
          ],
          next_page_token: "",
        },
      },
      {
        method: "workflow.listProjectLinks",
        result: {
          links: [
            {
              id: "link-1",
              project_id: "project-1",
              workflow_id: "workflow-1",
              default: true,
            },
          ],
        },
      },
      {
        method: "workflow.linkProject",
        result: {
          link: {
            id: "link-2",
            project_id: "project-1",
            workflow_id: "workflow-2",
            default: false,
          },
        },
      },
    ]);

    render(<App services={services} />);

    await screen.findByRole("heading", { name: "Core" });
    fireEvent.click(within(await expandBoardHoverMenu()).getByRole("button", { name: "Link workflow" }));
    expect(await screen.findByRole("complementary", { name: "Link workflow" })).toBeInTheDocument();
    const sidebar = within(screen.getByTestId("app-sidebar-host"));
    fireEvent.click(await sidebar.findByRole("button", { name: "Link" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.linkProject",
        params: {
          project_id: "project-1",
          workflow_id: "workflow-2",
          default_policy: "if_project_has_none",
        },
      });
    });
    await waitFor(() => {
      expect(window.location.pathname).toBe("/projects/project-1");
    });
    expect(window.location.search).toContain("workflowId=workflow-2");
  });

  it("disables workflow linking from the board hover menu while disconnected", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([...startupRoutes, ...boardRoutes()]);
    services.transport.connection.set("disconnected", "offline");

    render(<App services={services} />);

    await screen.findByRole("heading", { name: "Core" });
    expect(
      within(await expandBoardHoverMenu()).getByRole("button", { name: "Link workflow" }),
    ).toBeDisabled();
  });

  it("creates reusable workflows from the board link sidebar and opens the project-context editor", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const createdWorkflow = {
      id: "workflow-created",
      name: "Created Workflow",
      description: "",
      version: 1,
    };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      {
        method: "workflow.list",
        result: {
          workflows: [
            {
              id: "workflow-1",
              name: "Delivery",
              description: "",
              version: 1,
            },
          ],
          next_page_token: "",
        },
      },
      {
        method: "workflow.listProjectLinks",
        result: {
          links: [
            {
              id: "link-1",
              project_id: "project-1",
              workflow_id: "workflow-1",
              default: true,
            },
          ],
        },
      },
      {
        method: "workflow.createAndLinkProject",
        result: {
          workflow: createdWorkflow,
          link: {
            id: "link-created",
            project_id: "project-1",
            workflow_id: "workflow-created",
            default: false,
          },
        },
      },
      {
        method: "workflow.get",
        result: {
          definition: {
            ...workflowDefinitionResponse.definition,
            workflow: createdWorkflow,
          },
        },
      },
      { method: "workflow.validate", result: { valid: true, errors: [] } },
    ]);

    render(<App services={services} />);

    await screen.findByRole("heading", { name: "Core" });
    fireEvent.click(within(await expandBoardHoverMenu()).getByRole("button", { name: "Link workflow" }));
    const sidebar = within(await screen.findByRole("complementary", { name: "Link workflow" }));
    fireEvent.click(await sidebar.findByRole("button", { name: "New workflow" }));
    fireEvent.change(await screen.findByLabelText("Workflow name"), {
      target: { value: "Created Workflow" },
    });
    fireEvent.click(
      within(screen.getByTestId("app-sidebar-host")).getByRole("button", { name: "Create workflow" }),
    );

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.createAndLinkProject",
        params: {
          name: "Created Workflow",
          description: "",
          project_id: "project-1",
          default_policy: "if_project_has_none",
        },
      });
    });
    await waitFor(() => {
      expect(window.location.pathname).toBe("/workflows/workflow-created/editor");
    });
    expect(window.location.search).toContain("projectId=project-1");
  });

  it("treats duplicate workflow links as idempotent select success", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      {
        method: "workflow.list",
        result: {
          workflows: [
            {
              id: "workflow-1",
              name: "Delivery",
              description: "",
              version: 1,
            },
          ],
          next_page_token: "",
        },
      },
      {
        method: "workflow.listProjectLinks",
        result: {
          links: [
            {
              id: "link-1",
              project_id: "project-1",
              workflow_id: "workflow-1",
              default: true,
            },
          ],
        },
      },
      {
        method: "workflow.linkProject",
        result: {
          link: {
            id: "link-1",
            project_id: "project-1",
            workflow_id: "workflow-1",
            default: true,
          },
        },
      },
    ]);

    render(<App services={services} />);

    await screen.findByRole("heading", { name: "Core" });
    fireEvent.click(within(await expandBoardHoverMenu()).getByRole("button", { name: "Link workflow" }));
    fireEvent.click(
      await within(screen.getByTestId("app-sidebar-host")).findByRole("button", { name: "Select" }),
    );

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.linkProject",
        params: {
          project_id: "project-1",
          workflow_id: "workflow-1",
          default_policy: "if_project_has_none",
        },
      });
    });
    expect(window.location.search).toContain("workflowId=workflow-1");
  });
});
