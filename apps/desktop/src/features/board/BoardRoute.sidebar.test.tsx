import { type NativeDialogWindowOptions } from "@app/native-bridge";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import { App } from "../../App";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";
import {
  boardRoutes,
  emptyActivityResponse,
  installBoardRouteLifecycle,
  methodCallCount,
  mockWindowWidth,
  nativeDialogBridge,
  nativeWindowBridge,
  rejectingNativeDialogBridge,
  sidebarWidthStyle,
  taskDetailResponseForCancel,
  workspace,
} from "./boardRouteTestHarness";

describe("BoardRoute sidebar", () => {
  installBoardRouteLifecycle();

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
    await waitFor(() => {
      expect(within(sidebar).getByRole("button", { name: "Source workspace" })).toHaveTextContent("Main");
    });
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
    await waitFor(() => {
      expect(within(sidebar).getByRole("button", { name: "Source workspace" })).toHaveTextContent("Main");
    });
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

  it("opens board task detail in the global sidebar", async () => {
    const restoreWindowWidth = mockWindowWidth(1600);
    window.history.pushState(null, "", "/projects/project-1?workflowId=workflow-1");
    const services = createTestServices([
      ...startupRoutes,
      ...boardRoutes(),
      { method: "workflow.task.get", result: taskDetailResponseForCancel() },
      { method: "workflow.task.activity.list", result: emptyActivityResponse },
    ]);

    const { unmount } = render(<App services={services} />);

    try {
      const card = await screen.findByRole("article", { name: "Write focused tests" });
      fireEvent.click(card);

      const sidebar = await screen.findByTestId("app-sidebar-host");
      expect(screen.getByRole("complementary", { name: "Task" })).toHaveAttribute("data-mode", "overlay");
      expect(sidebarWidthStyle(sidebar)).toBe("650px");
      expect(await within(sidebar).findByDisplayValue("Task detail title")).toBeInTheDocument();
      expect(methodCallCount(services.transport.calls, "workflow.task.get")).toBe(1);
      expect(methodCallCount(services.transport.calls, "workflow.task.activity.list")).toBe(1);
      expect(new URLSearchParams(window.location.search).get("taskId")).toBe("task-1");

      fireEvent.click(within(sidebar).getByRole("button", { name: "Close" }));

      await waitFor(() => {
        expect(screen.queryByTestId("app-sidebar-host")).not.toBeInTheDocument();
      });
      expect(new URLSearchParams(window.location.search).get("taskId")).toBe("");

      fireEvent.click(await screen.findByRole("article", { name: "Write focused tests" }));

      const reopenedSidebar = await screen.findByTestId("app-sidebar-host");
      expect(screen.getByRole("complementary", { name: "Task" })).toHaveAttribute("data-mode", "overlay");
      expect(sidebarWidthStyle(reopenedSidebar)).toBe("650px");
      expect(await within(reopenedSidebar).findByDisplayValue("Task detail title")).toBeInTheDocument();
      expect(new URLSearchParams(window.location.search).get("taskId")).toBe("task-1");
    } finally {
      unmount();
      restoreWindowWidth();
    }
  });

  it("keeps each board sidebar destination width independent and persisted", async () => {
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

    // Task detail uses its own typed default width, independent of the create-task resize.
    fireEvent.click(await screen.findByRole("article", { name: "Write focused tests" }));
    const taskDetailSidebar = await screen.findByRole("complementary", { name: "Task" });
    expect(await within(taskDetailSidebar).findByDisplayValue("Task detail title")).toBeInTheDocument();
    expect(sidebarWidthStyle(taskDetailSidebar)).toBe("650px");

    fireEvent.click(within(taskDetailSidebar).getByRole("button", { name: "Close" }));
    await waitFor(() => {
      expect(screen.queryByRole("complementary", { name: "Task" })).not.toBeInTheDocument();
    });

    // Reopening the create-task destination restores its own resized width.
    fireEvent.click(await screen.findByRole("button", { name: "New Task" }));
    const reopenedCreateTask = await screen.findByRole("complementary", { name: "Create Backlog task" });
    expect(sidebarWidthStyle(reopenedCreateTask)).toBe(resizedWidth);
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
    await waitFor(() => {
      expect(screen.getByRole("button", { name: "Source workspace" })).toHaveTextContent("Main");
    });
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
});
