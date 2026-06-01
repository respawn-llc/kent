/* eslint-disable max-lines -- Project edit integration tests keep representative workspace fixtures local. */
import {
  createBrowserNativeBridge,
  type NativeBridge,
  type NativeDirectorySelection,
  type NativeDialogWindowOptions,
} from "@builder/desktop-native-bridge";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, vi } from "vitest";

import type { JsonValue } from "../../api/json";
import { App } from "../../App";
import { AppProviders } from "../../app/AppProviders";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";
import { ProjectEditRoute } from "./ProjectEditRoute";

const originalHistoryLengthDescriptor = Object.getOwnPropertyDescriptor(window.history, "length");
const originalLocalStorageDescriptor = Object.getOwnPropertyDescriptor(globalThis, "localStorage");

describe("ProjectEditRoute", () => {
  beforeEach(() => {
    window.history.replaceState(null, "", "/projects/project-1/edit");
  });

  afterEach(() => {
    vi.restoreAllMocks();
    if (originalHistoryLengthDescriptor === undefined) {
      Reflect.deleteProperty(window.history, "length");
    } else {
      Object.defineProperty(window.history, "length", originalHistoryLengthDescriptor);
    }
    restoreGlobalProperty("localStorage", originalLocalStorageDescriptor);
  });

  it("renders project identity, validates/saves name, and saves default workspace from row star", async () => {
    const services = createTestServices([
      ...startupRoutes,
      { method: "project.edit.get", result: projectEditResponse },
      { method: "project.update", result: { project: projectSummary } },
      { method: "project.defaultWorkspace.set", result: { project: projectSummary } },
    ]);

    renderProjectEdit(services);

    await screen.findByRole("heading", { name: "Workspaces" });
    expect(screen.getByDisplayValue("PROJ")).toBeDisabled();
    expect(screen.queryByLabelText("Default workspace")).not.toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Project name"), { target: { value: " Project " } });
    expect(screen.getByRole("button", { name: "Save name" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save name" })).toHaveClass(
      "aspect-square",
      "self-stretch",
      "rounded-full",
    );

    fireEvent.change(screen.getByLabelText("Project name"), { target: { value: "Renamed Project" } });
    fireEvent.click(screen.getByRole("button", { name: "Save name" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "project.update",
        params: { project_id: "project-1", display_name: "Renamed Project" },
      });
    });

    fireEvent.click(screen.getByRole("button", { name: "Make /tmp/project-alt the default workspace" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "project.defaultWorkspace.set",
        params: { project_id: "project-1", workspace_id: "workspace-2" },
      });
    });
    expect(
      screen.getByRole("button", { name: "Make /tmp/project the default workspace" }).className,
    ).not.toContain("hover:");
    fireEvent.click(screen.getByRole("button", { name: "Make /tmp/project the default workspace" }));
    expect(
      services.transport.calls.filter((call) => call.method === "project.defaultWorkspace.set"),
    ).toHaveLength(1);
    expect(screen.getByRole("button", { name: "Unlink /tmp/project" }).className).not.toContain("hover:");
  });

  it("opens Project Edit from the Home pencil and shows duplicate attach info without mutation", async () => {
    window.history.replaceState(null, "", "/");
    const services = createTestServices(
      [
        {
          method: "server.readiness.get",
          result: startupRoutes[0]?.result,
        },
        {
          method: "project.home.list",
          result: {
            projects: [projectSummary],
            next_page_token: "",
            generated_at_unix_ms: 1,
          },
        },
        globalAttentionRoute,
        { method: "project.edit.get", result: projectEditResponse },
      ],
      directoryBridge("/tmp/project"),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Edit Project" }));
    await screen.findByRole("complementary", { name: "Project" });
    await screen.findByRole("heading", { name: "Workspaces" });

    fireEvent.click(screen.getByRole("button", { name: "Attach workspace" }));

    await waitFor(() => {
      expect(services.transport.calls.some((call) => call.method === "project.attachWorkspace")).toBe(false);
    });
  });

  it("attaches new workspace through native picker", async () => {
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "project.edit.get", result: projectEditResponse },
        {
          method: "project.attachWorkspace",
          result: {
            binding: {
              project_id: "project-1",
              project_key: "PROJ",
              display_name: "Project",
              workspace_id: "workspace-3",
              canonical_root: "/tmp/project-extra",
              workspace_name: "project-extra",
              workspace_status: "available",
            },
          },
        },
      ],
      directoryBridge("/tmp/project-extra"),
    );

    renderProjectEdit(services);

    fireEvent.click(await screen.findByRole("button", { name: "Attach workspace" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "project.attachWorkspace",
        params: { project_id: "project-1", workspace_root: "/tmp/project-extra" },
      });
    });
  });

  it("confirms unlink and renders structured blockers from server", async () => {
    const services = createTestServices([
      ...startupRoutes,
      { method: "project.edit.get", result: projectEditResponse },
      {
        method: "project.unlinkWorkspace",
        result: {
          project_id: "project-1",
          workspace_id: "workspace-2",
          unlinked: false,
          blockers: [
            {
              code: "active_tasks",
              message: "1 active task still uses this workspace.",
              count: 1,
            },
          ],
        },
      },
    ]);

    renderProjectEdit(services);

    fireEvent.click(await screen.findByRole("button", { name: "Unlink /tmp/project-alt" }));
    await screen.findByRole("dialog");
    fireEvent.click(screen.getByRole("button", { name: "Unlink workspace" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "project.unlinkWorkspace",
        params: { project_id: "project-1", workspace_id: "workspace-2" },
      });
    });
  });

  it("opens workspace unlink in a native dialog when native dialogs are available", async () => {
    const opened: NativeDialogWindowOptions[] = [];
    const services = createTestServices(
      [...startupRoutes, { method: "project.edit.get", result: projectEditResponse }],
      nativeDialogBridge(opened),
    );

    renderProjectEdit(services);

    fireEvent.click(await screen.findByRole("button", { name: "Unlink /tmp/project-alt" }));

    await waitFor(() => {
      expect(opened).toHaveLength(1);
    });
    expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
    expect(opened[0]).toMatchObject({
      initialWidth: 400,
      route: "/native-dialog/workspace-unlink",
      title: "Unlink workspace?",
      params: {
        projectID: "project-1",
        workspaceID: "workspace-2",
        rootPath: "/tmp/project-alt",
      },
    });
  });

  it("keeps rendered native workspace unlink dialog at 400px for long paths", async () => {
    const fittedSizes: { width: number; height: number }[] = [];
    const rootPath = "/tmp/project-alt/with/a/very/long/path/that/needs/readable/wrapping";
    vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(() => dialogRect(400, 300));
    window.history.pushState(
      null,
      "",
      `/native-dialog/workspace-unlink?projectID=project-1&workspaceID=workspace-2&rootPath=${encodeURIComponent(rootPath)}`,
    );
    const services = createTestServices([], nativeDialogFitBridge(fittedSizes));

    render(<App services={services} />);

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(services.transport.calls.map((call) => call.method)).not.toContain("server.readiness.get");
    await waitFor(() => {
      expect(fittedSizes).toContainEqual({ height: 300, width: 400 });
    });
  });

  it("keeps the native workspace unlink dialog open when the server returns blockers", async () => {
    window.history.pushState(
      null,
      "",
      "/native-dialog/workspace-unlink?projectID=project-1&workspaceID=workspace-2&rootPath=%2Ftmp%2Fproject-alt",
    );
    let closeCount = 0;
    const services = createTestServices(
      [
        {
          method: "project.unlinkWorkspace",
          result: {
            project_id: "project-1",
            workspace_id: "workspace-2",
            unlinked: false,
            blockers: [
              {
                code: "active_tasks",
                message: "1 active task still uses this workspace.",
                count: 1,
              },
            ],
          },
        },
      ],
      nativeWindowCloseBridge(() => {
        closeCount += 1;
      }),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Unlink workspace" }));

    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    expect(closeCount).toBe(0);
    expect(services.transport.calls).toContainEqual({
      method: "project.unlinkWorkspace",
      params: { project_id: "project-1", workspace_id: "workspace-2" },
    });
  });

  it("closes the native workspace unlink dialog only after unlink succeeds", async () => {
    window.history.pushState(
      null,
      "",
      "/native-dialog/workspace-unlink?projectID=project-1&workspaceID=workspace-2&rootPath=%2Ftmp%2Fproject-alt",
    );
    let closeCount = 0;
    const changedProjects: string[] = [];
    const services = createTestServices(
      [
        {
          method: "project.unlinkWorkspace",
          result: {
            project_id: "project-1",
            workspace_id: "workspace-2",
            unlinked: true,
            blockers: [],
          },
        },
      ],
      nativeWindowCloseBridge(
        () => {
          closeCount += 1;
        },
        (projectID) => {
          changedProjects.push(projectID);
        },
      ),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Unlink workspace" }));

    await waitFor(() => {
      expect(closeCount).toBe(1);
    });
    expect(changedProjects).toEqual(["project-1"]);
    expect(services.transport.calls).toContainEqual({
      method: "project.unlinkWorkspace",
      params: { project_id: "project-1", workspace_id: "workspace-2" },
    });
  });

  it("shows a toast when native workspace unlink confirmation fails", async () => {
    window.history.pushState(
      null,
      "",
      "/native-dialog/workspace-unlink?projectID=project-1&workspaceID=workspace-2&rootPath=%2Ftmp%2Fproject-alt",
    );
    const services = createTestServices([
      {
        method: "project.unlinkWorkspace",
        error: new Error("server refused unlink"),
      },
    ]);

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Unlink workspace" }));

    expect(
      await within(screen.getByTestId("sonner-test-surface")).findByText("Workspace unlink window failed"),
    ).toBeInTheDocument();
    expect(services.transport.calls.map((call) => call.method)).not.toContain("server.readiness.get");
  });

  it("falls back to inline workspace unlink when native dialog open fails", async () => {
    const opened: NativeDialogWindowOptions[] = [];
    const services = createTestServices(
      [
        ...startupRoutes,
        { method: "project.edit.get", result: projectEditResponse },
        {
          method: "project.unlinkWorkspace",
          result: {
            project_id: "project-1",
            workspace_id: "workspace-2",
            unlinked: true,
            blockers: [],
          },
        },
      ],
      rejectingNativeDialogBridge(opened),
    );

    renderProjectEdit(services);

    fireEvent.click(await screen.findByRole("button", { name: "Unlink /tmp/project-alt" }));

    await waitFor(() => {
      expect(opened).toHaveLength(1);
    });
    expect(await screen.findByRole("dialog")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Unlink workspace" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "project.unlinkWorkspace",
        params: { project_id: "project-1", workspace_id: "workspace-2" },
      });
    });
  });

  it("requests next project edit workspace page through infinite scroll", async () => {
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "project.edit.get",
        handler: (params: JsonValue) => {
          if (isObject(params) && params.page_token === "cursor-2") {
            return {
              ...projectEditResponse,
              workspaces: [workspace3],
              next_page_token: "",
            };
          }
          return {
            ...projectEditResponse,
            workspaces: [workspace1, workspace2],
            next_page_token: "cursor-2",
          };
        },
      },
    ]);

    renderProjectEdit(services);

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "project.edit.get",
        params: { project_id: "project-1", page_size: 100, page_token: "cursor-2" },
      });
    });
  });

  it("does not render a local Back control", async () => {
    const services = createTestServices([
      ...startupRoutes,
      { method: "project.edit.get", result: projectEditResponse },
    ]);

    renderProjectEdit(services);

    await screen.findByRole("heading", { name: "Workspaces" });
    expect(screen.queryByRole("button", { name: "Back" })).not.toBeInTheDocument();
  });

  it("confirms project deletion from the sidebar trash button and returns Home", async () => {
    window.history.replaceState(null, "", "/");
    installStorage("localStorage");
    localStorage.setItem(
      "builder.desktop.lastProjectRoute",
      JSON.stringify({ projectId: "project-1", workflowId: "workflow-1" }),
    );
    const services = createTestServices([
      {
        method: "server.readiness.get",
        result: startupRoutes[0]?.result,
      },
      {
        method: "project.home.list",
        result: {
          projects: [projectSummary],
          next_page_token: "",
          generated_at_unix_ms: 1,
        },
      },
      globalAttentionRoute,
      { method: "project.edit.get", result: projectEditResponse },
      {
        method: "project.delete",
        result: {
          project_id: "project-1",
          deleted: true,
          blockers: [],
        },
      },
    ]);

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Edit Project" }));
    await screen.findByRole("complementary", { name: "Project" });
    fireEvent.click(screen.getByRole("button", { name: "Delete project" }));
    const dialog = await screen.findByRole("dialog", { name: "Delete project?" });
    fireEvent.click(within(dialog).getByRole("button", { name: "Delete project" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "project.delete",
        params: { project_id: "project-1" },
      });
    });
    expect(
      await within(screen.getByTestId("sonner-test-surface")).findByText("Project deleted."),
    ).toBeInTheDocument();
    expect(localStorage.getItem("builder.desktop.lastProjectRoute")).toBeNull();
    await waitFor(() => {
      expect(screen.queryByRole("complementary", { name: "Project" })).not.toBeInTheDocument();
    });
  });
});

function renderProjectEdit(services: ReturnType<typeof createTestServices>) {
  return render(
    <AppProviders services={services}>
      <ProjectEditRoute projectId="project-1" />
    </AppProviders>,
  );
}

function directoryBridge(path: string): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      directories: { select: true },
    },
    directories: {
      async selectDirectory(): Promise<NativeDirectorySelection> {
        return { path };
      },
    },
  };
}

function nativeDialogBridge(opened: NativeDialogWindowOptions[]): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      dialogWindows: true,
    },
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
    capabilities: {
      ...base.capabilities,
      dialogWindows: true,
    },
    dialogs: {
      async openWindow(options): Promise<void> {
        opened.push(options);
        throw new Error("Native dialog windows are unavailable in this shell.");
      },
    },
  };
}

function nativeDialogFitBridge(fittedSizes: { width: number; height: number }[]): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    window: {
      ...base.window,
      async fitCurrentToContent(size: { width: number; height: number }): Promise<void> {
        fittedSizes.push(size);
      },
    },
  };
}

function nativeWindowCloseBridge(
  onClose: () => void,
  onChanged: (projectID: string) => void = () => undefined,
): NativeBridge {
  const base = createBrowserNativeBridge();
  return {
    ...base,
    window: {
      ...base.window,
      async closeCurrent(): Promise<void> {
        onClose();
      },
    },
    projectWorkspace: {
      ...base.projectWorkspace,
      async notifyChanged(event): Promise<void> {
        onChanged(event.projectID);
      },
    },
  };
}

function dialogRect(width: number, height: number): DOMRect {
  return {
    bottom: height,
    height,
    left: 0,
    right: width,
    top: 0,
    width,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  };
}

function isObject(value: JsonValue): value is Readonly<Record<string, JsonValue>> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function installStorage(name: "localStorage"): void {
  const values = new Map<string, string>();
  Object.defineProperty(globalThis, name, {
    configurable: true,
    value: {
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

function restoreGlobalProperty(name: "localStorage", descriptor: PropertyDescriptor | undefined): void {
  if (descriptor === undefined) {
    Reflect.deleteProperty(globalThis, name);
    return;
  }
  Object.defineProperty(globalThis, name, descriptor);
}

const workspace1 = {
  workspace_id: "workspace-1",
  display_name: "Project",
  root_path: "/tmp/project",
  availability: "available",
  is_primary: true,
  updated_at_unix_ms: 1,
};

const workspace2 = {
  workspace_id: "workspace-2",
  display_name: "Project Alt",
  root_path: "/tmp/project-alt",
  availability: "available",
  is_primary: false,
  updated_at_unix_ms: 2,
};

const workspace3 = {
  workspace_id: "workspace-3",
  display_name: "Project Extra",
  root_path: "/tmp/project-extra",
  availability: "available",
  is_primary: false,
  updated_at_unix_ms: 3,
};

const projectEditResponse = {
  project_id: "project-1",
  project_key: "PROJ",
  display_name: "Project",
  default_workspace_id: "workspace-1",
  workspaces: [workspace1, workspace2],
  next_page_token: "",
};

const projectSummary = {
  project_id: "project-1",
  project_key: "PROJ",
  display_name: "Project",
  primary_workspace: workspace1,
  default_workflow_id: "workflow-1",
  default_workflow_name: "Delivery",
  default_workflow_valid: true,
  updated_at_unix_ms: 1,
  task_count: 0,
  attention_count: 0,
  workflow_count: 1,
};

const globalAttentionRoute = {
  method: "workflow.attention.list",
  result: {
    items: [],
    next_page_token: "",
    generated_at_unix_ms: 1,
  },
};
