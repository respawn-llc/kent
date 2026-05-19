import {
  createBrowserNativeBridge,
  type NativeBridge,
  type NativeDirectorySelection,
} from "@builder/desktop-native-bridge";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach } from "vitest";

import { App } from "./App";
import { createTestServices, startupRoutes } from "./testSupport/appServices";

describe("App", () => {
  const originalGetBoundingClientRect = HTMLElement.prototype.getBoundingClientRect.bind(
    HTMLElement.prototype,
  );
  const originalResizeObserver = globalThis.ResizeObserver;

  beforeEach(() => {
    window.history.pushState(null, "", "/");
    clearStorage("localStorage");
    clearStorage("sessionStorage");
    document.documentElement.removeAttribute("data-builder-theme");
    HTMLElement.prototype.getBoundingClientRect = originalGetBoundingClientRect;
    globalThis.ResizeObserver = originalResizeObserver;
  });

  afterEach(() => {
    HTMLElement.prototype.getBoundingClientRect = originalGetBoundingClientRect;
    globalThis.ResizeObserver = originalResizeObserver;
    document.documentElement.removeAttribute("data-builder-theme");
  });

  it("renders the startup-gated home shell", async () => {
    render(<App services={createTestServices(startupRoutes)} />);

    expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "New Project" })).toBeInTheDocument();
  });

  it("keeps route content on shell chrome without an extra surface or route padding", async () => {
    render(<App services={createTestServices(startupRoutes)} />);

    expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument();
    expect(screen.getByTestId("app-shell-content")).toHaveClass(
      "app-region-no-drag",
      "min-h-0",
      "min-w-0",
      "w-full",
      "overflow-visible",
    );
    expect(screen.getByTestId("app-shell-content")).not.toHaveClass("island-glass");
    expect(screen.getByTestId("route-transition-frame")).toHaveClass(
      "route-transition-frame",
      "h-full",
      "min-h-0",
      "min-w-0",
      "w-full",
      "p-[var(--space-2)]",
    );
    expect(screen.getByTestId("home-route-root")).toHaveClass("h-full", "min-h-0");
    expect(screen.getByTestId("home-route-root").className).not.toContain("p-[var(--space-4)]");
    expect(screen.getByTestId("home-pane-grid")).toHaveClass("gap-[var(--space-2)]");
    expect(screen.getByTestId("home-pane-grid").className).not.toContain("gap-[var(--space-4)]");
    expect(screen.queryByTestId("app-chrome-history-buttons")).not.toBeInTheDocument();
  });

  it("keeps the macOS home chrome link offset tied to the native titlebar tokens", async () => {
    render(
      <App services={createTestServices(startupRoutes, createBrowserNativeBridge({ platform: "macos" }))} />,
    );

    expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument();
    expect(screen.getByTestId("app-chrome-navigation")).toHaveClass(
      "left-[var(--native-home-link-left-macos)]",
    );
  });

  it("shows browser-backed history controls as a contiguous macOS chrome row", async () => {
    window.history.replaceState({ __TSR_index: 1, __TSR_key: "current", key: "current" }, "", "/");

    render(
      <App services={createTestServices(startupRoutes, createBrowserNativeBridge({ platform: "macos" }))} />,
    );

    expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument();
    const chromeNavigation = screen.getByTestId("app-chrome-navigation");
    const historyButtons = within(chromeNavigation).getByTestId("app-chrome-history-buttons");
    expect(chromeNavigation).toHaveClass("flex", "h-6", "left-[var(--native-home-link-left-macos)]");
    expect(within(chromeNavigation).getByLabelText("Home")).toHaveClass("h-6", "w-6");
    expect(historyButtons).toHaveClass("grid", "grid-cols-2");
    expect(historyButtons).toHaveAttribute("data-placement", "after-home");
    expect(within(historyButtons).getByLabelText("Back")).toBeEnabled();
    expect(within(historyButtons).getByLabelText("Forward")).toBeDisabled();
  });

  it("places history controls before Home on non-macOS chrome", async () => {
    window.history.replaceState({ __TSR_index: 1, __TSR_key: "current", key: "current" }, "", "/");

    render(<App services={createTestServices(startupRoutes)} />);

    expect(await screen.findByRole("heading", { name: "Projects" })).toBeInTheDocument();
    const chromeNavigation = screen.getByTestId("app-chrome-navigation");
    const historyButtons = within(chromeNavigation).getByTestId("app-chrome-history-buttons");
    expect(chromeNavigation).toHaveClass("right-[var(--space-4)]");
    expect(historyButtons).toHaveAttribute("data-placement", "before-home");
    expect(within(historyButtons).getByLabelText("Back")).toBeEnabled();
  });

  it("renders workflow validation blockers with wrapping inbox metadata", async () => {
    render(
      <App
        services={createTestServices([
          ...startupRoutes,
          {
            method: "workflow.attention.list",
            result: {
              items: [
                {
                  id: "validation_blocker:project-1:workflow-1",
                  kind: "validation_blocker",
                  project_id: "project-1",
                  workflow_id: "workflow-1",
                  task_id: "",
                  task_short_id: "",
                  task_title: "",
                  run_id: "",
                  session_id: "",
                  ask_id: "",
                  task_transition_id: "",
                  message: 'Workflow "Default nodes" is invalid for task start',
                  occurred_at_unix_ms: 1,
                  latest_event_sequence: 1,
                },
              ],
              next_page_token: "",
              generated_at_unix_ms: 1,
              latest_event_sequence: 1,
            },
          },
        ])}
      />,
    );

    expect(await screen.findByText("validation_blocker")).toBeInTheDocument();
    expect(screen.getByText('Workflow "Default nodes" is invalid for task start')).toBeInTheDocument();
    expect(screen.getByTestId("attention-row")).toHaveClass("min-w-0");
    expect(screen.getByTestId("attention-row-meta")).toHaveClass("flex", "flex-wrap", "min-w-0");
  });

  it("creates projects from a validated dialog destination", async () => {
    const services = createTestServices(
      [
        ...startupRoutes,
        {
          method: "project.planWorkspaceBinding",
          result: {
            kind: "local_unbound",
            canonical_root: "/tmp/example-project",
            binding: null,
          },
        },
        {
          method: "project.create",
          result: {
            binding: {
              project_id: "project-1",
              project_key: "EXP",
              display_name: "Example Project",
              workspace_id: "workspace-1",
              canonical_root: "/tmp/example-project",
              workspace_name: "example-project",
              workspace_status: "available",
            },
          },
        },
      ],
      directoryBridge("/tmp/example-project"),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "New Project" }));
    expect(await screen.findByRole("heading", { name: "Create project" })).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Project name"), { target: { value: " Example Project " } });
    fireEvent.change(screen.getByLabelText("Project key"), { target: { value: "x ?" } });

    expect(await screen.findByText("Remove whitespace at start or end.")).toBeInTheDocument();
    expect(screen.getByText("Remove whitespace.")).toBeInTheDocument();
    expect(screen.getByText("Use A-Z and 0-9 only.")).toBeInTheDocument();

    fireEvent.change(screen.getByLabelText("Project name"), { target: { value: "Example Project" } });
    fireEvent.change(screen.getByLabelText("Project key"), { target: { value: "exp" } });
    fireEvent.click(screen.getByRole("button", { name: "Create project" }));

    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "project.create",
        params: {
          display_name: "Example Project",
          project_key: "EXP",
          workspace_root: "/tmp/example-project",
        },
      });
    });
  });

  it("falls back to the dialog when the project creation window is denied", async () => {
    const services = createTestServices(
      [
        ...startupRoutes,
        {
          method: "project.planWorkspaceBinding",
          result: {
            kind: "local_unbound",
            canonical_root: "/tmp/example-project",
            binding: null,
          },
        },
      ],
      projectCreationWindowBridge("/tmp/example-project", { message: "window.get_all_windows not allowed" }),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "New Project" }));

    expect(await screen.findByText("Project creation window failed")).toBeInTheDocument();
    expect(screen.getByText("window.get_all_windows not allowed")).toBeInTheDocument();
    expect(screen.queryByText("Server rejected request")).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Create project" })).toBeInTheDocument();
  });

  it("does not create projects for server workspace selection plans", async () => {
    const services = createTestServices(
      [
        ...startupRoutes,
        {
          method: "project.planWorkspaceBinding",
          result: {
            kind: "server_workspace_selection",
            canonical_root: "/tmp/example-project",
            binding: null,
          },
        },
        {
          method: "project.create",
          result: {
            binding: {
              project_id: "project-created",
              project_key: "EXP",
              display_name: "Example Project",
              workspace_id: "workspace-created",
              canonical_root: "/tmp/example-project",
              workspace_name: "example-project",
              workspace_status: "available",
            },
          },
        },
      ],
      directoryBridge("/tmp/example-project"),
    );

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "New Project" }));

    expect(await screen.findByText("Choose an existing project")).toBeInTheDocument();
    expect(screen.queryByRole("heading", { name: "Create project" })).not.toBeInTheDocument();
    expect(services.transport.calls).not.toContainEqual({
      method: "project.create",
      params: {
        display_name: "Example Project",
        project_key: "EXP",
        workspace_root: "/tmp/example-project",
      },
    });
  });
});

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

function projectCreationWindowBridge(path: string, error: unknown): NativeBridge {
  const base = directoryBridge(path);
  return {
    ...base,
    capabilities: {
      ...base.capabilities,
      projectCreationWindow: true,
    },
    projectCreation: {
      ...base.projectCreation,
      async openWindow(): Promise<void> {
        throw error;
      },
    },
  };
}

function clearStorage(name: "localStorage" | "sessionStorage"): void {
  try {
    if (name === "localStorage") {
      globalThis.localStorage.clear();
      return;
    }
    globalThis.sessionStorage.clear();
  } catch {
    return;
  }
}
