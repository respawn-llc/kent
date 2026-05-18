/* eslint-disable max-lines -- Board route integration tests keep representative board fixtures local. */
import { createBrowserNativeBridge, type NativeBridge, type NativeDialogWindowOptions } from "@builder/desktop-native-bridge";
import { act, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, vi } from "vitest";

import type { JsonValue } from "../../api/json";
import { App } from "../../App";
import { appChromeTitleClassNames } from "../../app/appChromeStyles";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";

const boardHoverMenuCollapsedClassNames = [
  "board-hover-menu-collapsed",
  "fixed",
  "bottom-[var(--space-4)]",
  "left-[var(--space-4)]",
  "grid-rows-[0fr]",
  "min-h-[var(--board-menu-collapsed-height)]",
  "max-h-[min(700px,70vh)]",
  "p-[var(--board-menu-padding)]",
  "w-[var(--board-menu-collapsed-width)]",
  "rounded-[var(--radius-l)]",
] as const;

const boardHoverMenuExpandedClassNames = [
  "board-hover-menu-expanded",
  "grid-rows-[1fr]",
  "max-h-[min(700px,70vh)]",
  "p-[var(--board-menu-padding)]",
  "w-[min(360px,calc(100vw-32px))]",
] as const;

const boardHoverMenuActionDockClassNames = [
  "gap-[var(--board-menu-icon-gap)]",
  "absolute",
  "bottom-[var(--board-menu-padding)]",
  "h-10",
  "left-[var(--board-menu-padding)]",
] as const;

const boardHoverMenuWorkflowContentClassNames = [
  "gap-[var(--board-menu-content-gap)]",
  "min-h-0",
  "min-w-0",
  "overflow-y-auto",
] as const;

describe("BoardRoute", () => {
  const originalUserAgent = window.navigator.userAgent;

  beforeEach(() => {
    installStorage("localStorage");
    installStorage("sessionStorage");
    setNavigatorUserAgent(originalUserAgent);
  });

  afterEach(() => {
    vi.useRealTimers();
    setNavigatorUserAgent(originalUserAgent);
  });

  it("restores the last valid project workflow route on relaunch", async () => {
    window.history.pushState(null, "", "/");
    localStorage.setItem(
      "builder.desktop.lastProjectRoute",
      JSON.stringify({ projectId: "project-1", workflowId: "workflow-1" }),
    );
    const services = createTestServices([...startupRoutes, { method: "workflow.board.get", result: boardResponse }]);

    render(<App services={services} />);

    expect(await screen.findByTestId("app-chrome-title")).toHaveTextContent("Delivery");
    expect(window.location.pathname).toBe("/projects/project-1");
    expect(window.location.search).toContain("workflowId=workflow-1");
  });

  it("renders workflow groups and drag-starts a Backlog task without confirmation", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      { method: "workflow.board.get", result: boardResponse },
      { method: "workflow.task.start", result: {} },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("heading", { name: "Core" })).toBeInTheDocument();
    expect(screen.getByTestId("app-chrome-title")).toHaveTextContent("Delivery");
    expect(screen.getByTestId("app-chrome-title")).toHaveClass(...appChromeTitleClassNames, "left-[var(--space-2)]");
    expect(screen.queryByRole("heading", { name: "Project" })).not.toBeInTheDocument();
    expect(screen.queryByText("proj")).not.toBeInTheDocument();
    expect(screen.queryByText("Drag Backlog task to first active node to start automation.")).not.toBeInTheDocument();
    expect(screen.getByTestId("route-transition-frame")).not.toHaveClass("p-[var(--space-2)]");
    expect(screen.getByRole("list")).toHaveClass("overflow-x-auto");
    expect(screen.getByRole("list")).not.toHaveClass(
      "hide-scrollbar",
      "overflow-y-hidden",
      "pb-[var(--shadow-bleed-island)]",
    );
    expect(screen.getByText("coder")).toBeInTheDocument();
    expect(screen.getByRole("listitem", { name: "Backlog" })).toHaveClass("island-glass");
    expect(screen.getByRole("listitem", { name: "Backlog" })).toHaveClass("w-[min(560px,80vw)]", "shrink-0");
    expect(screen.queryByRole("button", { name: "Expand Done" })).not.toBeInTheDocument();
    expect(screen.queryByTestId("board-transition-source")).not.toBeInTheDocument();
    expect(screen.getByTestId("board-column-rail")).toHaveClass(
      "w-max",
      "min-w-full",
      "px-[var(--space-2)]",
      "pb-[var(--space-2)]",
    );
    expect(screen.getByTestId("board-column-rail")).not.toHaveClass("pt-[var(--space-2)]", "p-[var(--space-2)]");
    expect(screen.getByTestId("kanban-column-scroll-backlog")).toHaveClass(
      "overflow-y-auto",
      "pr-[var(--space-1)]",
    );
    const card = screen.getByRole("article", { name: "Write focused tests" });
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
    expect(screen.queryByText("Confirm")).not.toBeInTheDocument();
  });

  it("renders the shared full-page empty state when project has no workflows", async () => {
    window.history.pushState(null, "", "/projects/project-1");
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.board.get",
        result: {
          board: {
            ...boardResponse.board,
            workflows: [],
          },
        },
      },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("heading", { name: "No workflows yet" })).toBeInTheDocument();
    expect(screen.getByText("Set up a valid project workflow from CLI, agent, or API before creating tasks.")).toBeInTheDocument();
    expect(screen.getByTestId("empty-state")).toHaveClass("h-full", "min-h-0", "place-items-center");
    expect(screen.getByTestId("empty-state-content")).toHaveClass("justify-items-center", "text-center");
    expect(screen.getByTestId("empty-state-icon")).not.toBeEmptyDOMElement();
  });

  it("falls back to the project name for the chrome title when workflow name is blank", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.board.get",
        result: {
          board: {
            ...boardResponse.board,
            selected_workflow: { ...workflow, display_name: "" },
            workflows: [{ ...workflow, display_name: "" }],
          },
        },
      },
    ]);

    render(<App services={services} />);

    expect(await screen.findByTestId("app-chrome-title")).toHaveTextContent("Project");
  });

  it("places the chrome title on the right side on macOS", async () => {
    setNavigatorUserAgent("Mozilla/5.0 (Macintosh; Intel Mac OS X 15_0)");
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([...startupRoutes, { method: "workflow.board.get", result: boardResponse }]);

    render(<App services={services} />);

    expect(await screen.findByTestId("app-chrome-title")).toHaveTextContent("Delivery");
    expect(screen.getByTestId("app-chrome-title")).toHaveClass("right-[var(--space-2)]", "text-right");
  });

  it("only shows the Done expansion control when hidden Done cards exist", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const baseCard = firstBoardCard();
    const doneCard = {
      ...baseCard,
      task_id: "task-done",
      short_id: "T-9",
      title: "Finished task",
      status: {
        ...baseCard.status,
        kind: "done",
        label: "Done",
        native_state: "done",
      },
    };
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.board.get",
        result: {
          board: {
            ...boardResponse.board,
            columns: boardResponse.board.columns.map((column) =>
              column.is_done ? { ...column, task_count: 2 } : column,
            ),
            done_preview: [doneCard],
            has_hidden_done_cards: true,
          },
        },
      },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("button", { name: "Expand Done" })).toBeInTheDocument();
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
          code: "workflow.validation.no_terminal_path",
          message: "non-terminal node cannot reach a terminal node",
          node_id: "node-1",
          edge_id: "",
          blocks_context: true,
        },
        {
          code: "workflow.validation.unreachable_node",
          message: "node is not reachable from start",
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
        {
          method: "workflow.board.get",
          result: {
            board: {
              ...boardResponse.board,
              selected_workflow: invalidWorkflow,
              workflows: [invalidWorkflow],
              groups: [],
              cards: boardResponse.board.cards,
              columns: [
                {
                  node: { node_id: "backlog", key: "backlog", display_name: "Backlog" },
                  group_id: "",
                  sort_order: 0,
                  is_backlog: true,
                  is_done: false,
                  task_count: 0,
                },
                {
                  node: { node_id: "done", key: "done", display_name: "Done" },
                  group_id: "",
                  sort_order: 1,
                  is_backlog: false,
                  is_done: true,
                  task_count: 0,
                },
              ],
            },
          },
        },
        { method: "workflow.task.start", result: {} },
      ],
      nativeDialogBridge(opened),
    );

    render(<App services={services} />);

    expect(await screen.findByRole("heading", { name: "Backlog" })).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Done" })).toBeInTheDocument();
    expect(screen.queryByText("Workflow validation blocks automation. Backlog tasks and comments remain available.")).not.toBeInTheDocument();
    const issues = screen.getByRole("complementary", { name: "Workflow issues" });
    expect(issues).toHaveClass("fixed", "right-[var(--space-4)]", "bottom-[var(--space-4)]", "gap-[6px]");
    expect(within(issues).getByTestId("floating-notice-header")).toHaveClass("items-center", "leading-none");
    expect(within(issues).getByRole("heading", { name: "Workflow issues" })).toHaveClass("text-lg", "font-bold", "leading-none", "text-[var(--color-error)]");
    expect(within(issues).getByRole("button", { name: "Collapse" })).toHaveClass("h-[18px]", "w-[18px]");
    expect(within(issues).getByRole("list")).toHaveClass("workflow-issues-list", "list-none", "leading-snug", "max-w-[72ch]");
    expect(within(issues).getAllByRole("listitem").map((item) => item.textContent)).toEqual([
      "task start requires exactly one outgoing transition group",
      "non-terminal node cannot reach a terminal node",
      "node is not reachable from start",
    ]);
    expect(within(issues).getByRole("list")).toHaveClass("text-[var(--color-on-island)]");
    fireEvent.click(within(issues).getByRole("button", { name: "Collapse" }));
    const expandButton = screen.getByRole("button", { name: "Expand" });
    expect(screen.getByRole("complementary", { name: "Workflow issues" })).toHaveClass(
      "floating-notice-collapsed",
      "h-12",
      "rounded-[var(--radius-m)]",
      "w-12",
    );
    expect(expandButton).toHaveClass("h-full", "w-full");
    expect(screen.getByRole("article", { name: "Write focused tests" })).toHaveAttribute("draggable", "false");
    expect(screen.queryByText("No valid workflow")).not.toBeInTheDocument();

    const card = screen.getByRole("article", { name: "Write focused tests" });
    const doneColumn = screen.getByRole("listitem", { name: "Done" });
    const dataTransfer = new TestDataTransfer();
    fireEvent.dragStart(card, { dataTransfer });
    fireEvent.drop(doneColumn, { dataTransfer });

    expect(services.transport.calls.some((call) => call.method === "workflow.task.start")).toBe(false);
    expect(services.transport.calls.some((call) => call.method === "workflow.task.move")).toBe(false);

    fireEvent.click(screen.getByRole("button", { name: "New Task" }));
    await waitFor(() => {
      expect(opened).toHaveLength(1);
    });
    expect(screen.queryByRole("dialog", { name: "Create Backlog task" })).not.toBeInTheDocument();
  });

  it("opens Create Task in a native dialog window when native dialogs are available", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const opened: NativeDialogWindowOptions[] = [];
    const services = createTestServices(
      [...startupRoutes, { method: "workflow.board.get", result: boardResponse }],
      nativeDialogBridge(opened),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "New Task" }));

    await waitFor(() => {
      expect(opened).toHaveLength(1);
    });
    const openedDialog = onlyOpenedDialog(opened);
    expect(openedDialog.initialHeight).toBe(560);
    expect(openedDialog.initialWidth).toBe(608);
    expect(openedDialog.resizable).toBe(true);
    expect(openedDialog.label).toMatch(/^new-task-project-1-/u);
    expect(openedDialog.params).toEqual({
      projectID: "project-1",
      workflowID: "workflow-1",
    });
    expect(openedDialog.route).toBe("/native-dialog/new-task");
    expect(openedDialog.title).toBe("Create Backlog task");
    expect(screen.queryByRole("dialog", { name: "Create Backlog task" })).not.toBeInTheDocument();
  });

  it("falls back to an inline Create Task dialog when native dialogs are unavailable", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const opened: NativeDialogWindowOptions[] = [];
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "workflow.board.get", result: boardResponse },
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

    await waitFor(() => {
      expect(opened).toHaveLength(1);
    });
    expect(screen.getByText("Create task window failed")).toBeInTheDocument();
    expect(screen.getByText("Native dialog windows are unavailable in this shell.")).toBeInTheDocument();
    const dialog = await screen.findByRole("dialog", { name: "Create Backlog task" });
    expect(within(dialog).getByText("Main")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create task" })).toHaveClass("mx-auto", "max-w-[400px]");
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
      expect(screen.queryByRole("dialog", { name: "Create Backlog task" })).not.toBeInTheDocument();
    });
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
    expect(await screen.findByText("Main")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Create task" })).toHaveClass("mx-auto", "max-w-[400px]");
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
    const services = createTestServices([...startupRoutes, { method: "workflow.board.get", result: boardResponse }]);

    render(<App services={services} />);

    const menu = await screen.findByRole("navigation");
    vi.useFakeTimers();
    expect(menu).toHaveClass(...boardHoverMenuCollapsedClassNames);
    expect(screen.getByRole("button", { name: "New Task" })).toBeInTheDocument();
    expect(screen.getByTestId("board-hover-menu-actions")).toHaveClass(...boardHoverMenuActionDockClassNames);
    expect(screen.getByTestId("board-hover-menu-workflows")).toHaveClass("opacity-0");

    fireEvent.mouseEnter(menu);

    expect(menu).toHaveClass(...boardHoverMenuExpandedClassNames);
    expect(screen.getByRole("heading", { name: "Workflows" })).toHaveClass(
      "text-lg",
      "font-bold",
      "leading-none",
      "text-[var(--color-on-island)]",
    );
    expect(screen.getByTestId("board-hover-menu-header")).toHaveClass(
      "grid",
      "grid-cols-[minmax(0,1fr)_auto]",
      "items-center",
      "px-[var(--space-2)]",
      "pt-[var(--space-2)]",
      "leading-none",
    );
    expect(screen.queryByText("Default")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Pin menu" })).toHaveClass("size-[24px]", "text-[var(--color-muted)]");
    expect(screen.getByTestId("board-hover-menu-workflows")).toHaveClass(
      ...boardHoverMenuWorkflowContentClassNames,
    );
    expect(screen.getByTestId("board-hover-menu-actions")).toHaveClass(...boardHoverMenuActionDockClassNames);
    expect(screen.getAllByRole("button", { name: /Delivery/u })).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Delivery" })).toHaveAttribute("data-slot", "item");

    fireEvent.click(screen.getByRole("button", { name: "Pin menu" }));
    expect(screen.getByRole("button", { name: "Unpin menu" })).toHaveAttribute("aria-pressed", "true");
    expect(screen.getByRole("button", { name: "Unpin menu" })).toHaveClass("text-[var(--color-primary)]");
    expect(screen.getByTestId("board-hover-menu-pin-off-icon")).toBeInTheDocument();

    fireEvent.mouseLeave(menu);
    act(() => {
      vi.advanceTimersByTime(500);
    });

    expect(menu).toHaveClass(...boardHoverMenuExpandedClassNames);

    const unpinButton = screen.getByRole("button", { name: "Unpin menu" });
    fireEvent.click(unpinButton);
    fireEvent.blur(unpinButton, { relatedTarget: null });
    fireEvent.mouseLeave(menu);

    act(() => {
      vi.advanceTimersByTime(500);
    });

    expect(screen.getByTestId("board-hover-menu-workflows")).toHaveClass("opacity-0");

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
      {
        method: "workflow.board.get",
        result: {
          board: {
            ...boardResponse.board,
            cards: [activeCard],
          },
        },
      },
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
    fireEvent.drop(doneColumn, { dataTransfer });

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "workflow.task.move",
        params: { task_id: "task-1", target_node_id: "done", output_values: {} },
      });
    });
  });

  it("fetches the next board task page when a column scroll reaches the end", async () => {
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const secondPageCard = {
      ...boardResponse.board.cards[0],
      task_id: "task-2",
      short_id: "T-2",
      title: "Second page task",
    };
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.board.get",
        handler: (params: JsonValue) => {
          if (isObject(params) && params.page_token === "cursor-2") {
            return {
              board: {
                ...boardResponse.board,
                cards: [secondPageCard],
                next_page_token: "",
              },
            };
          }
          return {
            board: {
              ...boardResponse.board,
              next_page_token: "cursor-2",
            },
          };
        },
      },
    ]);

    render(<App services={services} />);

    const scroller = await screen.findByTestId("kanban-column-scroll-backlog");
    setScrollMetrics(scroller, { clientHeight: 100, scrollHeight: 140, scrollTop: 40 });
    fireEvent.scroll(scroller);

    expect(await screen.findByRole("article", { name: "Second page task" })).toBeInTheDocument();
    expect(services.transport.calls).toContainEqual({
      method: "workflow.board.get",
      params: {
        project_id: "project-1",
        workflow_id: "workflow-1",
        done_preview_limit: 5,
        page_size: 100,
        page_token: "cursor-2",
      },
    });
  });
});

class TestDataTransfer {
  readonly #values = new Map<string, string>();

  setData(type: string, value: string): void {
    this.#values.set(type, value);
  }

  getData(type: string): string {
    return this.#values.get(type) ?? "";
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

const workflow = {
  workflow_id: "workflow-1",
  display_name: "Delivery",
  description: "",
  graph_revision: 1,
  is_project_default: true,
  valid_for_task_creation: true,
  validation_errors: [],
};

const workspace = {
  workspace_id: "workspace-1",
  display_name: "Main",
  root_path: "/tmp/project",
  availability: "available",
  is_primary: true,
  updated_at_unix_ms: 1,
};

const taskActions = {
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

const boardResponse = {
  board: {
    project_id: "project-1",
    project: { project_key: "proj", display_name: "Project" },
    selected_workflow: workflow,
    workflows: [workflow],
    groups: [{ group_id: "group-1", key: "core", display_name: "Core", sort_order: 1, node_ids: ["node-1"] }],
    columns: [
      {
        node: { node_id: "backlog", key: "backlog", display_name: "Backlog" },
        group_id: "",
        sort_order: 0,
        is_backlog: true,
        is_done: false,
        task_count: 1,
      },
      {
        node: { node_id: "node-1", key: "implement", display_name: "Implement", assignee_role: "coder" },
        group_id: "group-1",
        sort_order: 1,
        is_backlog: false,
        is_done: false,
        task_count: 0,
      },
      {
        node: { node_id: "done", key: "done", display_name: "Done" },
        group_id: "",
        sort_order: 99,
        is_backlog: false,
        is_done: true,
        task_count: 0,
      },
    ],
    cards: [
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
    ],
    done_preview: [],
    next_page_token: "",
    generated_at_unix_ms: 1,
    latest_event_sequence: 1,
  },
};

function firstBoardCard(): (typeof boardResponse.board.cards)[number] {
  const card = boardResponse.board.cards[0];
  if (card === undefined) {
    throw new Error("board response test fixture has no cards");
  }
  return card;
}

function nativeDialogBridge(opened: NativeDialogWindowOptions[]): NativeBridge {
  const base = createBrowserNativeBridge();
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
  const base = createBrowserNativeBridge();
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

function onlyOpenedDialog(opened: readonly NativeDialogWindowOptions[]): NativeDialogWindowOptions {
  const dialog = opened[0];
  if (dialog === undefined) {
    throw new Error("expected a native dialog to open");
  }
  return dialog;
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
