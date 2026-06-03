/* eslint-disable max-lines -- Board route integration tests keep representative board fixtures local. */
import {
  createBrowserNativeBridge,
  createTauriNativeBridge,
  type NativeBridge,
  type NativeDialogWindowOptions,
  type NativeTaskDetailTarget,
} from "@builder/desktop-native-bridge";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, vi } from "vitest";

import type { JsonValue } from "../../api/json";
import { App } from "../../App";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";

async function expandBoardHoverMenu(): Promise<HTMLElement> {
  const menu = await screen.findByRole("navigation");
  fireEvent.mouseEnter(menu);
  return menu;
}

describe("BoardRoute", () => {
  const originalUserAgent = window.navigator.userAgent;

  beforeEach(() => {
    installStorage("localStorage");
    installStorage("sessionStorage");
    setNavigatorUserAgent(originalUserAgent);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.unstubAllGlobals();
    setNavigatorUserAgent(originalUserAgent);
  });

  it("restores the last valid project workflow route on relaunch", async () => {
    window.history.pushState(null, "", "/");
    localStorage.setItem(
      "builder.desktop.lastProjectRoute",
      JSON.stringify({ projectId: "project-1", workflowId: "workflow-1" }),
    );
    const services = createTestServices([...startupRoutes, ...boardRoutes()]);

    render(<App services={services} />);

    await screen.findByTestId("app-chrome-title");
    expect(window.location.pathname).toBe("/projects/project-1");
    expect(window.location.search).toContain("workflowId=workflow-1");
  });

  it("renders workflow groups and drag-starts a Backlog task without confirmation", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      { method: "workflow.task.start", result: {} },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("heading", { name: "Core" })).toBeInTheDocument();
    expect(screen.queryByTestId("board-transition-source")).not.toBeInTheDocument();
    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const targetColumn = screen.getByRole("listitem", { name: "Implement" });
    const dataTransfer = new TestDataTransfer();

    fireEvent.dragStart(card, { dataTransfer });
    fireEvent.drop(targetColumn, { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.start",
        params: { task_id: "task-1" },
      });
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("starts tasks from in-memory drag state after rerender when browser dataTransfer drops custom payloads", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      { method: "workflow.task.start", result: {} },
    ]);

    const view = render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const targetColumn = screen.getByRole("listitem", { name: "Implement" });
    const dataTransfer = new DroppedPayloadDataTransfer();

    fireEvent.dragStart(card, { dataTransfer });
    view.rerender(<App services={services} />);
    fireEvent.drop(targetColumn, { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.start",
        params: { task_id: "task-1" },
      });
    });
  });

  it("accepts an override drop on a blocked target", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      { method: "workflow.task.move", result: {} },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const dataTransfer = new TestDataTransfer();
    const doneColumn = screen.getByRole("listitem", { name: "Done" });
    fireEvent.dragStart(card, { dataTransfer });
    expect(doneColumn).toHaveAttribute("data-drop-state", "blocked");
    fireEvent.drop(doneColumn, { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.move",
        params: {
          task_id: "task-1",
          target_node_id: "done",
          output_values: {},
          allow_missing_edge: true,
        },
      });
    });
  });

  it("loads node card pages only after columns become visible", async () => {
    const visibility = installIntersectionObserverMock();
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const nodeCardCalls: string[] = [];
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.board.get", result: boardResponse },
      {
        method: "workflow.board.nodeCards.list",
        handler: (params: JsonValue) => {
          const nodeID = isObject(params) && typeof params.node_id === "string" ? params.node_id : "";
          nodeCardCalls.push(nodeID);
          return boardNodeCardsResponse(nodeID, nodeID === "backlog" ? [firstBoardCard()] : [], "");
        },
      },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("heading", { name: "Backlog" })).toBeInTheDocument();
    expect(nodeCardCalls).toEqual([]);

    act(() => {
      visibility.reveal("Backlog");
    });
    expect(await screen.findByRole("article", { name: "Write focused tests" })).toBeInTheDocument();
    expect(nodeCardCalls).toEqual(["backlog"]);

    act(() => {
      visibility.reveal("Implement");
    });
    await waitFor(() => {
      expect(nodeCardCalls).toEqual(["backlog", "node-1"]);
    });
  });

  it("loads Done node cards after Done becomes visible", async () => {
    const visibility = installIntersectionObserverMock();
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const nodeCardCalls: string[] = [];
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.board.get",
        result: {
          board: {
            ...boardResponse.board,
            columns: boardResponse.board.columns.map((column) =>
              column.is_done ? { ...column, task_count: 1 } : column,
            ),
          },
        },
      },
      {
        method: "workflow.board.nodeCards.list",
        handler: (params: JsonValue) => {
          const nodeID = isObject(params) && typeof params.node_id === "string" ? params.node_id : "";
          nodeCardCalls.push(nodeID);
          return boardNodeCardsResponse(nodeID, [], "");
        },
      },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("heading", { name: "Done" })).toBeInTheDocument();
    act(() => {
      visibility.reveal("Done");
    });
    await waitFor(() => {
      expect(nodeCardCalls).toContain("done");
    });
  });

  it("renders pending-approval tasks in their source node column", async () => {
    const visibility = installIntersectionObserverMock();
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const pendingApprovalCard = {
      ...firstBoardCard(),
      short_id: "BUI-7",
      title: "Waiting on approval",
      body_preview: "Approval should remain visible on the board.",
      active_node_ids: ["node-1"],
      status: {
        ...firstBoardCard().status,
        kind: "waiting_approval",
        label: "Approval",
        native_state: "waiting_approval",
        node_ids: ["node-1"],
        attention_types: ["approval"],
      },
      actions: {
        ...taskActions,
        can_start: false,
        manual_move_target_node_ids: [],
      },
    };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(
        {
          board: {
            ...boardResponse.board,
            columns: boardResponse.board.columns.map((column) =>
              column.node.node_id === "node-1"
                ? { ...column, task_count: 1 }
                : { ...column, task_count: 0 },
            ),
            cards: [],
          },
        },
        {
          backlog: { cards: [] },
          "node-1": { cards: [pendingApprovalCard] },
          done: { cards: [] },
        },
      ),
    ]);

    render(<App services={services} />);

    const implementColumn = await screen.findByRole("listitem", { name: "Implement" });
    act(() => {
      visibility.reveal("Implement");
    });
    expect(await within(implementColumn).findByRole("article", { name: "Waiting on approval" })).toBeInTheDocument();
    expect(screen.queryByRole("article", { name: "Write focused tests" })).not.toBeInTheDocument();
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

  it("opens Create Task in the global sidebar instead of a native dialog window", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const opened: NativeDialogWindowOptions[] = [];
    const services = createTestServices(
      [
        ...startupRoutes,
        ...boardRoutes(),
        {
          method: "project.workspace.list",
          result: {
            project_id: "project-1",
            workspaces: [workspace],
            default_workspace_id: "workspace-1",
            next_page_token: "",
          },
        },
        { method: "workflow.task.create", result: { task: { id: "task-new" } } },
      ],
      nativeDialogBridge(opened),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "New Task" }));

    const sidebar = await screen.findByTestId("app-sidebar-host");
    const panel = screen.getByRole("complementary", { name: "Create Backlog task" });
    expect(panel).toHaveAttribute("data-mode", "overlay");
    expect(opened).toHaveLength(0);
    await within(sidebar).findByDisplayValue("workspace-1");
    fireEvent.change(within(sidebar).getByLabelText("Title"), { target: { value: "Sidebar task" } });
    fireEvent.click(within(sidebar).getByRole("button", { name: "Create task" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.create",
        params: {
          project_id: "project-1",
          workflow_id: "workflow-1",
          title: "Sidebar task",
          body: "",
          source_workspace_id: "workspace-1",
        },
      });
    });
    await waitFor(() => {
      expect(screen.queryByTestId("app-sidebar-host")).not.toBeInTheDocument();
    });
  });

  it("keeps overlay sidebar mounted for close animation before unmounting", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      {
        method: "project.workspace.list",
        result: {
          project_id: "project-1",
          workspaces: [workspace],
          default_workspace_id: "workspace-1",
          next_page_token: "",
        },
      },
    ]);

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "New Task" }));
    const sidebar = await screen.findByRole("complementary", { name: "Create Backlog task" });
    expect(sidebar).toHaveAttribute("data-state", "open");

    fireEvent.click(within(sidebar).getByRole("button", { name: "Close" }));

    await waitFor(() => {
      expect(screen.getByTestId("app-sidebar-host")).toHaveAttribute("data-state", "closing");
    });
    await waitFor(() => {
      expect(screen.queryByTestId("app-sidebar-host")).not.toBeInTheDocument();
    });
  });

  it("closes the sidebar on main route navigation", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      {
        method: "project.workspace.list",
        result: {
          project_id: "project-1",
          workspaces: [workspace],
          default_workspace_id: "workspace-1",
          next_page_token: "",
        },
      },
      {
        method: "workflow.attention.list",
        result: { items: [], next_page_token: "", generated_at_unix_ms: 1 },
      },
      { method: "project.list", result: { projects: [], next_page_token: "", generated_at_unix_ms: 1 } },
    ]);

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "New Task" }));
    expect(await screen.findByRole("complementary", { name: "Create Backlog task" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("link", { name: "Home" }));

    await waitFor(() => {
      expect(screen.queryByRole("complementary", { name: "Create Backlog task" })).not.toBeInTheDocument();
    });
    expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument();
  });

  it("uses the same Create Task sidebar when native dialogs are unavailable", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const opened: NativeDialogWindowOptions[] = [];
    const services = createTestServices(
      [
        ...startupRoutes,
        ...boardRoutes(),
        {
          method: "project.workspace.list",
          result: {
            project_id: "project-1",
            workspaces: [workspace],
            default_workspace_id: "workspace-1",
            next_page_token: "",
          },
        },
        { method: "workflow.task.create", result: { task: { id: "task-new" } } },
      ],
      rejectingNativeDialogBridge(opened),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "New Task" }));

    const sidebar = await screen.findByRole("complementary", { name: "Create Backlog task" });
    expect(sidebar).toHaveAttribute("data-mode", "overlay");
    expect(opened).toHaveLength(0);
    await within(sidebar).findByDisplayValue("workspace-1");
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Fallback task" } });
    fireEvent.click(screen.getByRole("button", { name: "Create task" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.create",
        params: {
          project_id: "project-1",
          workflow_id: "workflow-1",
          title: "Fallback task",
          body: "",
          source_workspace_id: "workspace-1",
        },
      });
    });
    await waitFor(() => {
      expect(screen.queryByRole("complementary", { name: "Create Backlog task" })).not.toBeInTheDocument();
    });
  });

  it("opens board task detail in the global sidebar instead of a native task detail window", async () => {
    const restoreWindowWidth = mockWindowWidth(1600);
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const opened: NativeTaskDetailTarget[] = [];
    const services = createTestServices(
      [
        ...startupRoutes,
        ...boardRoutes(),
        { method: "workflow.task.get", result: taskDetailResponseForCancel() },
        { method: "workflow.task.activity.list", result: emptyActivityResponse },
      ],
      taskDetailWindowBridge(opened),
    );

    const { unmount } = render(<App services={services} />);

    try {
      const card = await screen.findByRole("article", { name: "Write focused tests" });
      fireEvent.click(card);

      const sidebar = await screen.findByTestId("app-sidebar-host");
      expect(screen.getByRole("complementary", { name: "Task" })).toHaveAttribute("data-mode", "overlay");
      expect(sidebarWidthStyle(sidebar)).toBe("560px");
      expect(await within(sidebar).findByDisplayValue("Task detail title")).toBeInTheDocument();
      expect(methodCallCount(services.transport.calls, "workflow.task.get")).toBe(1);
      expect(methodCallCount(services.transport.calls, "workflow.task.activity.list")).toBe(1);
      expect(opened).toEqual([]);
      expect(new URLSearchParams(window.location.search).get("taskId")).toBe("task-1");

      fireEvent.click(within(sidebar).getByRole("button", { name: "Close" }));

      await waitFor(() => {
        expect(screen.queryByTestId("app-sidebar-host")).not.toBeInTheDocument();
      });
      expect(new URLSearchParams(window.location.search).get("taskId")).toBe("");

      fireEvent.click(await screen.findByRole("article", { name: "Write focused tests" }));

      const reopenedSidebar = await screen.findByTestId("app-sidebar-host");
      expect(screen.getByRole("complementary", { name: "Task" })).toHaveAttribute("data-mode", "overlay");
      expect(sidebarWidthStyle(reopenedSidebar)).toBe("560px");
      expect(await within(reopenedSidebar).findByDisplayValue("Task detail title")).toBeInTheDocument();
      expect(new URLSearchParams(window.location.search).get("taskId")).toBe("task-1");
    } finally {
      unmount();
      restoreWindowWidth();
    }
  });

  it("keeps resized sidebar width across board sidebar destinations", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      {
        method: "project.workspace.list",
        result: {
          project_id: "project-1",
          workspaces: [workspace],
          default_workspace_id: "workspace-1",
          next_page_token: "",
        },
      },
      { method: "workflow.task.get", result: taskDetailResponseForCancel() },
      { method: "workflow.task.activity.list", result: emptyActivityResponse },
    ]);

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "New Task" }));
    const createTaskSidebar = await screen.findByRole("complementary", { name: "Create Backlog task" });
    fireEvent.keyDown(within(createTaskSidebar).getByRole("separator", { name: "Resize sidebar" }), {
      key: "End",
    });
    const resizedWidth = sidebarWidthStyle(createTaskSidebar);
    expect(Number.parseInt(resizedWidth, 10)).toBeGreaterThan(360);

    fireEvent.click(within(createTaskSidebar).getByRole("button", { name: "Close" }));

    await waitFor(() => {
      expect(screen.queryByRole("complementary", { name: "Create Backlog task" })).not.toBeInTheDocument();
    });

    fireEvent.click(await screen.findByRole("article", { name: "Write focused tests" }));

    const taskDetailSidebar = await screen.findByRole("complementary", { name: "Task" });
    expect(sidebarWidthStyle(taskDetailSidebar)).toBe(resizedWidth);
    expect(await within(taskDetailSidebar).findByDisplayValue("Task detail title")).toBeInTheDocument();
  });

  it("renders Create Task in a native dialog route and closes the native window after submit", async () => {
    window.history.pushState(null, "", "/native-dialog/new-task?projectID=project-1&workflowID=workflow-1");
    let closeCount = 0;
    const services = createTestServices(
      [
        ...startupRoutes,
        {
          method: "project.workspace.list",
          result: {
            project_id: "project-1",
            workspaces: [workspace],
            default_workspace_id: "workspace-1",
            next_page_token: "",
          },
        },
        { method: "workflow.task.create", result: { task: { id: "task-new" } } },
      ],
      nativeWindowBridge(() => {
        closeCount += 1;
      }),
    );

    render(<App services={services} />);

    expect(await screen.findByRole("dialog", { name: "Create Backlog task" })).toBeInTheDocument();
    await screen.findByDisplayValue("workspace-1");
    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Native task" } });
    fireEvent.click(screen.getByRole("button", { name: "Create task" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.create",
        params: {
          project_id: "project-1",
          workflow_id: "workflow-1",
          title: "Native task",
          body: "",
          source_workspace_id: "workspace-1",
        },
      });
    });
    await waitFor(() => {
      expect(closeCount).toBe(1);
    });
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
    expect(within(screen.getByTestId("board-hover-menu-header")).getByRole("button", {
      name: "Link workflow",
    })).toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Delivery" })).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Edit workflow Delivery" })).toBeInTheDocument();

    fireEvent.mouseLeave(menu);
    act(() => {
      vi.advanceTimersByTime(500);
    });

    fireEvent.mouseEnter(menu);
  });

  it("uses server manual-move target permissions and card action flags", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const baseCard = firstBoardCard();
    const activeCard = {
      ...baseCard,
      active_node_ids: ["node-1"],
      status: { ...baseCard.status, kind: "active", label: "Active", node_ids: ["node-1"] },
      actions: {
        ...taskActions,
        can_start: false,
        can_interrupt: true,
        interrupt_run_id: "run-1",
        manual_move_target_node_ids: ["done"],
      },
    };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(boardResponse, {
        backlog: { cards: [] },
        "node-1": { cards: [activeCard] },
        done: { cards: [] },
      }),
      { method: "workflow.task.move", result: {} },
      { method: "workflow.task.interrupt", result: {} },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    fireEvent.click(screen.getByRole("button", { name: "Interrupt" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.interrupt",
        params: { task_id: "task-1", run_id: "run-1" },
      });
    });

    const doneColumn = screen.getByRole("listitem", { name: "Done" });
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    expect(doneColumn).toHaveAttribute("data-drop-state", "allowed");
    fireEvent.drop(doneColumn, { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.move",
        params: { task_id: "task-1", target_node_id: "done", output_values: {} },
      });
    });
  });

  it("moves a Done card back to Backlog without confirmation", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const doneCard = doneBoardCard();
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(
        {
          board: {
            ...boardResponse.board,
            columns: boardResponse.board.columns.map((column) =>
              column.is_done ? { ...column, task_count: 1 } : column,
            ),
          },
        },
        {
          backlog: { cards: [] },
          "node-1": { cards: [] },
          done: { cards: [doneCard] },
        },
      ),
      { method: "workflow.task.move", result: {} },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    expect(card).toHaveAttribute("draggable", "true");
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    fireEvent.drop(screen.getByRole("listitem", { name: "Backlog" }), { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.move",
        params: {
          task_id: "task-1",
          target_node_id: "backlog",
          output_values: {},
          allow_missing_edge: true,
        },
      });
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
  });

  it("confirms a Done rollback that starts an agent", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const doneCard = doneBoardCard();
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(
        {
          board: {
            ...boardResponse.board,
            columns: boardResponse.board.columns.map((column) =>
              column.is_done ? { ...column, task_count: 1 } : column,
            ),
          },
        },
        {
          backlog: { cards: [] },
          "node-1": { cards: [] },
          done: { cards: [doneCard] },
        },
      ),
      { method: "workflow.task.move", result: {} },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const implementColumn = screen.getByRole("listitem", { name: "Implement" });
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    expect(implementColumn).toHaveAttribute("data-drop-state", "blocked");
    fireEvent.drop(implementColumn, { dataTransfer });

    const dialog = await screen.findByRole("dialog", { name: "Rollback and start the agent?" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(services.transport.calls.some((call) => call.method === "workflow.task.move")).toBe(false);

    fireEvent.dragStart(card, { dataTransfer: new TestDataTransfer() });
    fireEvent.drop(implementColumn, { dataTransfer: new TestDataTransfer() });
    fireEvent.click(await screen.findByRole("button", { name: "Rollback and start agent" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.move",
        params: {
          task_id: "task-1",
          target_node_id: "node-1",
          output_values: {},
          auto_approve: true,
        },
      });
    });
  });

  it("collects missing inputs before an override drop that starts an agent", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const codeReviewColumn = {
      node: {
        node_id: "node-2",
        key: "code_review",
        kind: "agent",
        display_name: "Code review",
        assignee_role: "reviewer",
        transition_output_fields: [{ name: "summary", description: "Prior work summary." }],
      },
      group_id: "group-1",
      sort_order: 2,
      is_backlog: false,
      is_done: false,
      task_count: 0,
    };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes({
        board: {
          ...boardResponse.board,
          groups: [
            {
              ...firstBoardGroup(),
              node_ids: ["node-1", "node-2"],
            },
          ],
          columns: [boardColumnAt(0), boardColumnAt(1), codeReviewColumn, boardColumnAt(2)],
        },
      }),
      { method: "workflow.task.move", result: {} },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const codeReview = screen.getByRole("listitem", { name: "Code review" });
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    expect(codeReview).toHaveAttribute("data-drop-state", "blocked");
    fireEvent.drop(codeReview, { dataTransfer });

    const dialog = await screen.findByRole("dialog", { name: "Submit missing inputs" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel" }));
    expect(services.transport.calls.some((call) => call.method === "workflow.task.move")).toBe(false);

    fireEvent.dragStart(card, { dataTransfer: new TestDataTransfer() });
    fireEvent.drop(codeReview, { dataTransfer: new TestDataTransfer() });
    fireEvent.change(await screen.findByLabelText("summary"), { target: { value: "Replacement summary" } });
    fireEvent.click(screen.getByRole("button", { name: "Submit and start agent" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.move",
        params: {
          task_id: "task-1",
          target_node_id: "node-2",
          output_values: { summary: "Replacement summary" },
          allow_missing_edge: true,
          auto_approve: true,
        },
      });
    });
  });

  it("shows a toast when an allowed card drop fails on the server", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const activeCard = {
      ...firstBoardCard(),
      active_node_ids: ["node-1"],
      status: { ...firstBoardCard().status, kind: "active", label: "Active", node_ids: ["node-1"] },
      actions: {
        ...taskActions,
        can_start: false,
        manual_move_target_node_ids: ["done"],
      },
    };
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(boardResponse, {
        backlog: { cards: [] },
        "node-1": { cards: [activeCard] },
        done: { cards: [] },
      }),
      { method: "workflow.task.move", error: new Error("required output summary") },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    fireEvent.drop(screen.getByRole("listitem", { name: "Done" }), { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls.some((call) => call.method === "workflow.task.move")).toBe(true);
    });
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
    expect(within(await expandBoardHoverMenu()).getByRole("button", { name: "Link workflow" })).toBeDisabled();
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

  it("refreshes node-card pages after task cancel so task moves from Backlog to Done", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    let canceled = false;
    const canceledCard: BoardRouteCard = {
      ...firstBoardCard(),
      active_node_ids: ["done"],
      status: {
        attention_types: [],
        kind: "canceled",
        label: "Canceled",
        native_state: "canceled",
        node_ids: ["done"],
        run_ids: [],
      },
      actions: { ...taskActions, can_start: false, can_cancel: false },
    };
    const boardWithCancelState = () => ({
      board: {
        ...boardResponse.board,
        columns: boardResponse.board.columns.map((column) => {
          if (column.is_backlog) {
            return { ...column, task_count: canceled ? 0 : 1 };
          }
          if (column.is_done) {
            return { ...column, task_count: canceled ? 1 : 0 };
          }
          return column;
        }),
      },
    });
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.board.get", handler: () => boardWithCancelState() },
      {
        method: "workflow.board.nodeCards.list",
        handler: (params: JsonValue) => {
          const nodeID = isObject(params) && typeof params.node_id === "string" ? params.node_id : "";
          const cards =
            nodeID === "backlog" && !canceled
              ? [firstBoardCard()]
              : nodeID === "done" && canceled
                ? [canceledCard]
                : [];
          return boardNodeCardsResponse(nodeID, cards, "");
        },
      },
      { method: "workflow.task.get", result: taskDetailResponseForCancel() },
      { method: "workflow.task.activity.list", result: emptyActivityResponse },
      {
        method: "workflow.task.cancel",
        handler: () => {
          canceled = true;
          return {};
        },
      },
    ]);

    render(<App services={services} />);

    const card = await screen.findByRole("article", { name: "Write focused tests" });
    fireEvent.click(card);
    fireEvent.click(await screen.findByRole("button", { name: "Cancel task" }));
    fireEvent.click(await screen.findByRole("button", { name: "Confirm" }));

    await waitFor(() => {
      expect(
        within(screen.getByTestId("kanban-column-scroll-backlog")).queryByRole("article", {
          name: "Write focused tests",
        }),
      ).not.toBeInTheDocument();
    });

    await waitFor(() => {
      expect(
        within(screen.getByTestId("kanban-column-scroll-done")).getByRole("article", {
          name: "Write focused tests",
        }),
      ).toBeInTheDocument();
    });
    expect(services.transport.calls).toContainEqual({
      method: "workflow.task.cancel",
      params: { task_id: "task-1" },
    });
  });

  it("fetches the next board task page when a column scroll reaches the end", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const secondPageCard = {
      ...firstBoardCard(),
      task_id: "task-2",
      short_id: "T-2",
      title: "Second page task",
    };
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.board.get", result: boardResponse },
      {
        method: "workflow.board.nodeCards.list",
        handler: (params: JsonValue) => {
          const pageToken =
            isObject(params) && typeof params.page_token === "string" ? params.page_token : "";
          return boardNodeCardsResponse(
            "backlog",
            pageToken === "cursor-2" ? [secondPageCard] : [firstBoardCard()],
            pageToken === "cursor-2" ? "" : "cursor-2",
          );
        },
      },
    ]);

    render(<App services={services} />);

    const scroller = await screen.findByTestId("kanban-column-scroll-backlog");
    await screen.findByRole("article", { name: "Write focused tests" });
    setScrollMetrics(scroller, { clientHeight: 100, scrollHeight: 140, scrollTop: 40 });
    fireEvent.scroll(scroller);

    expect(await screen.findByRole("article", { name: "Second page task" })).toBeInTheDocument();
    expect(services.transport.calls).toContainEqual({
      method: "workflow.board.nodeCards.list",
      params: {
        project_id: "project-1",
        workflow_id: "workflow-1",
        node_id: "backlog",
        page_size: 100,
        page_token: "cursor-2",
      },
    });
  });
});

class TestDataTransfer {
  readonly #values = new Map<string, string>();
  dropEffect = "none";
  effectAllowed = "all";

  get types(): readonly string[] {
    return [...this.#values.keys()];
  }

  setData(type: string, value: string): void {
    this.#values.set(type, value);
  }

  getData(type: string): string {
    return this.#values.get(type) ?? "";
  }
}

class DroppedPayloadDataTransfer {
  dropEffect = "none";
  effectAllowed = "all";
  readonly types: readonly string[] = [];

  setData(): void {
    // Browser shells may omit custom drag payloads on drop; board route must not rely on them.
  }

  getData(): string {
    return "";
  }
}

function isObject(value: JsonValue): value is Readonly<Record<string, JsonValue>> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function setScrollMetrics(
  element: HTMLElement,
  metrics: Readonly<{ clientHeight: number; scrollHeight: number; scrollTop: number }>,
): void {
  Object.defineProperty(element, "clientHeight", { configurable: true, value: metrics.clientHeight });
  Object.defineProperty(element, "scrollHeight", { configurable: true, value: metrics.scrollHeight });
  Object.defineProperty(element, "scrollTop", { configurable: true, value: metrics.scrollTop });
}

function installStorage(name: "localStorage" | "sessionStorage"): void {
  const values = new Map<string, string>();
  Object.defineProperty(globalThis, name, {
    configurable: true,
    value: {
      clear() {
        values.clear();
      },
      getItem(key: string) {
        return values.get(key) ?? null;
      },
      removeItem(key: string) {
        values.delete(key);
      },
      setItem(key: string, value: string) {
        values.set(key, value);
      },
    },
  });
}

function setNavigatorUserAgent(userAgent: string): void {
  Object.defineProperty(window.navigator, "userAgent", {
    configurable: true,
    value: userAgent,
  });
}

function installIntersectionObserverMock(): Readonly<{ reveal: (label: string) => void }> {
  const callbacks = new Map<string, (isIntersecting: boolean) => void>();
  class MockIntersectionObserver implements IntersectionObserver {
    readonly root = null;
    readonly rootMargin = "";
    readonly scrollMargin = "";
    readonly thresholds = [];
    readonly #callback: IntersectionObserverCallback;
    readonly #labels = new Set<string>();

    constructor(callback: IntersectionObserverCallback) {
      this.#callback = callback;
    }

    disconnect(): void {
      for (const label of this.#labels) {
        callbacks.delete(label);
      }
      this.#labels.clear();
    }

    observe(element: Element): void {
      const label = element.getAttribute("aria-label") ?? "";
      this.#labels.add(label);
      callbacks.set(label, (isIntersecting: boolean) => {
        this.#callback([intersectionEntry(element, isIntersecting)], this);
      });
    }

    takeRecords(): IntersectionObserverEntry[] {
      return [];
    }

    unobserve(element: Element): void {
      const label = element.getAttribute("aria-label") ?? "";
      this.#labels.delete(label);
      callbacks.delete(label);
    }
  }
  vi.stubGlobal("IntersectionObserver", MockIntersectionObserver);
  return {
    reveal(label: string): void {
      const callback = callbacks.get(label);
      if (callback === undefined) {
        throw new Error(`No observed column ${label}`);
      }
      callback(true);
    },
  };
}

function intersectionEntry(element: Element, isIntersecting: boolean): IntersectionObserverEntry {
  return {
    boundingClientRect: element.getBoundingClientRect(),
    intersectionRatio: isIntersecting ? 1 : 0,
    intersectionRect: isIntersecting ? element.getBoundingClientRect() : new DOMRectReadOnly(),
    isIntersecting,
    rootBounds: null,
    target: element,
    time: 0,
  };
}

const workflow: BoardRouteWorkflow = {
  workflow_id: "workflow-1",
  display_name: "Delivery",
  description: "",
  version: 1,
  is_project_default: true,
  valid_for_task_creation: true,
  validation_errors: [],
};

type BoardRouteWorkflow = Readonly<{
  workflow_id: string;
  display_name: string;
  description: string;
  version: number;
  is_project_default: boolean;
  valid_for_task_creation: boolean;
  validation_errors: readonly BoardRouteValidationError[];
}>;

type BoardRouteValidationError = Readonly<{
  code: string;
  message: string;
  node_id: string;
  edge_id: string;
  blocks_context: boolean;
}>;

const workspace = {
  workspace_id: "workspace-1",
  display_name: "Main",
  root_path: "/tmp/project",
  availability: "available",
  is_primary: true,
  updated_at_unix_ms: 1,
};

type BoardRouteTaskActions = Readonly<{
  can_start: boolean;
  can_interrupt: boolean;
  interrupt_run_id: string;
  can_resume: boolean;
  resume_run_id: string;
  can_cancel: boolean;
  needs_detail_for_interrupt: boolean;
  needs_detail_for_resume: boolean;
  manual_move_target_node_ids: readonly string[];
}>;

type BoardRouteTaskStatus = Readonly<{
  kind: string;
  label: string;
  native_state: string;
  node_ids: readonly string[];
  run_ids: readonly string[];
  attention_types: readonly string[];
}>;

type BoardRouteCard = Readonly<{
  task_id: string;
  short_id: string;
  title: string;
  body_preview: string;
  workflow_id: string;
  active_node_ids: readonly string[];
  source_workspace: typeof workspace;
  status: BoardRouteTaskStatus;
  actions: BoardRouteTaskActions;
  updated_at_unix_ms: number;
}>;

const taskActions: BoardRouteTaskActions = {
  can_start: true,
  can_interrupt: false,
  interrupt_run_id: "",
  can_resume: false,
  resume_run_id: "",
  can_cancel: true,
  needs_detail_for_interrupt: false,
  needs_detail_for_resume: false,
  manual_move_target_node_ids: [],
};

const boardCards: readonly BoardRouteCard[] = [
  {
    task_id: "task-1",
    short_id: "T-1",
    title: "Write focused tests",
    body_preview: "Cover drag start",
    workflow_id: "workflow-1",
    active_node_ids: [],
    source_workspace: workspace,
    status: {
      kind: "backlog",
      label: "Backlog",
      native_state: "backlog",
      node_ids: [],
      run_ids: [],
      attention_types: [],
    },
    actions: taskActions,
    updated_at_unix_ms: 1,
  },
];

function boardRoutes(
  response = boardResponse,
  nodePages: Readonly<
    Record<
      string,
      Readonly<{ cards: readonly (typeof boardResponse.board.cards)[number][]; nextPageToken?: string }>
    >
  > = {
    backlog: { cards: boardResponse.board.cards },
    "node-1": { cards: [] },
    done: { cards: [] },
  },
) {
  return [
    { method: "workflow.board.get", result: response },
    {
      method: "workflow.board.nodeCards.list",
      handler: (params: JsonValue) => {
        const nodeID = isObject(params) && typeof params.node_id === "string" ? params.node_id : "";
        const page = nodePages[nodeID] ?? { cards: [] };
        return boardNodeCardsResponse(nodeID, page.cards, page.nextPageToken ?? "");
      },
    },
  ];
}

function boardNodeCardsResponse(
  nodeID: string,
  cards: readonly (typeof boardResponse.board.cards)[number][],
  nextPageToken: string,
) {
  return {
    project_id: boardResponse.board.project_id,
    workflow_id: boardResponse.board.selected_workflow.workflow_id,
    node_id: nodeID,
    cards,
    next_page_token: nextPageToken,
    generated_at_unix_ms: 1,
  };
}

function taskDetailResponseForCancel() {
  return {
    task: {
      summary: {
        id: "task-1",
        project_id: "project-1",
        workflow_id: "workflow-1",
        short_id: "T-1",
        title: "Task detail title",
        created_at_unix_ms: 1,
        updated_at_unix_ms: 2,
        done: false,
        canceled_at_unix_ms: 0,
      },
      project: { display_name: "Project" },
      workflow,
      body: "Cancel this task",
      source_workspace: workspace,
      status: {
        kind: "backlog",
        label: "Backlog",
        native_state: "active",
        node_ids: ["backlog"],
        run_ids: [],
        attention_types: [],
      },
      actions: {
        can_start: true,
        can_interrupt: false,
        interrupt_run_id: "",
        can_resume: false,
        resume_run_id: "",
        can_cancel: true,
        needs_detail_for_interrupt: false,
        needs_detail_for_resume: false,
        manual_move_target_node_ids: [],
      },
      attention: [],
      runs: [],
      transitions: [],
      comments: [],
    },
  };
}

const emptyActivityResponse = {
  items: [],
  next_page_token: "",
  generated_at_unix_ms: 1,
};

const boardResponse = {
  board: {
    project_id: "project-1",
    project: { project_key: "proj", display_name: "Project" },
    selected_workflow: workflow,
    workflows: [workflow],
    groups: [{ group_id: "group-1", key: "core", display_name: "Core", sort_order: 1, node_ids: ["node-1"] }],
    columns: [
      {
        node: { node_id: "backlog", key: "backlog", kind: "start", display_name: "Backlog" },
        group_id: "",
        sort_order: 0,
        is_backlog: true,
        is_done: false,
        task_count: 1,
      },
      {
        node: {
          node_id: "node-1",
          key: "implement",
          kind: "agent",
          display_name: "Implement",
          assignee_role: "coder",
        },
        group_id: "group-1",
        sort_order: 1,
        is_backlog: false,
        is_done: false,
        task_count: 0,
      },
      {
        node: { node_id: "done", key: "done", kind: "terminal", display_name: "Done" },
        group_id: "",
        sort_order: 99,
        is_backlog: false,
        is_done: true,
        task_count: 0,
      },
    ],
    cards: boardCards,
    done_preview: [],
    next_page_token: "",
    generated_at_unix_ms: 1,
  },
};

const workflowDefinitionResponse = {
  definition: {
    workflow: {
      id: "workflow-1",
      name: "Delivery",
      description: "",
      version: 1,
    },
    node_groups: [],
    nodes: [
      {
        id: "backlog",
        workflow_id: "workflow-1",
        key: "backlog",
        kind: "start",
        display_name: "Backlog",
      },
      {
        id: "node-1",
        workflow_id: "workflow-1",
        key: "implement",
        kind: "agent",
        display_name: "Implement",
        subagent_role: "coder",
      },
      {
        id: "done",
        workflow_id: "workflow-1",
        key: "done",
        kind: "terminal",
        display_name: "Done",
      },
    ],
    transition_groups: [],
    edges: [],
  },
};

function firstBoardCard(): (typeof boardResponse.board.cards)[number] {
  const card = boardResponse.board.cards[0];
  if (card === undefined) {
    throw new Error("board response test fixture has no cards");
  }
  return card;
}

function firstBoardGroup(): (typeof boardResponse.board.groups)[number] {
  const group = boardResponse.board.groups[0];
  if (group === undefined) {
    throw new Error("board response test fixture has no groups");
  }
  return group;
}

function boardColumnAt(index: number): (typeof boardResponse.board.columns)[number] {
  const column = boardResponse.board.columns[index];
  if (column === undefined) {
    throw new Error(`board response test fixture has no column at ${index.toString()}`);
  }
  return column;
}

function doneBoardCard(): (typeof boardResponse.board.cards)[number] {
  const card = firstBoardCard();
  return {
    ...card,
    active_node_ids: ["done"],
    actions: {
      ...card.actions,
      can_start: false,
      manual_move_target_node_ids: [],
    },
    status: {
      ...card.status,
      kind: "done",
      label: "Done",
      native_state: "terminal",
      node_ids: ["done"],
    },
  };
}

function nativeDialogBridge(opened: NativeDialogWindowOptions[]): NativeBridge {
  const base = createTauriNativeBridge("macos");
  return {
    ...base,
    dialogs: {
      async openWindow(options): Promise<void> {
        opened.push(options);
      },
    },
  };
}

function rejectingNativeDialogBridge(opened: NativeDialogWindowOptions[]): NativeBridge {
  const base = createTauriNativeBridge("macos");
  return {
    ...base,
    dialogs: {
      async openWindow(options): Promise<void> {
        opened.push(options);
        throw new Error("Native dialog windows are unavailable in this shell.");
      },
    },
  };
}

function nativeWindowBridge(onClose: () => void): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    window: {
      ...base.window,
      async closeCurrent(): Promise<void> {
        onClose();
      },
    },
  };
}

function methodCallCount(calls: readonly { method: string }[], method: string): number {
  return calls.filter((call) => call.method === method).length;
}

function sidebarWidthStyle(sidebar: HTMLElement): string {
  return sidebar.style.getPropertyValue("--app-sidebar-width");
}

function mockWindowWidth(width: number): () => void {
  const descriptor = Object.getOwnPropertyDescriptor(window, "innerWidth");
  Object.defineProperty(window, "innerWidth", { configurable: true, value: width });
  return () => {
    if (descriptor === undefined) {
      Reflect.deleteProperty(window, "innerWidth");
      return;
    }
    Object.defineProperty(window, "innerWidth", descriptor);
  };
}

function taskDetailWindowBridge(opened: NativeTaskDetailTarget[]): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      taskDetailWindow: true,
    },
    taskDetail: {
      ...base.taskDetail,
      async openWindow(target): Promise<void> {
        opened.push(target);
      },
    },
  };
}
