import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";

import type { JsonValue } from "../../api/json";
import { App } from "../../App";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";

describe("HomeRoute", () => {
  beforeEach(() => {
    installStorage("localStorage");
    installStorage("sessionStorage");
    window.history.pushState(null, "", "/");
  });

  it("reloads project pages from the first page after leaving and revisiting Home", async () => {
    const services = createHomeRevisitServices();

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Alpha /tmp/project-alpha" }));
    await waitFor(() => {
      expect(services.transport.calls).toContainEqual({
        method: "project.home.list",
        params: { page_size: 40, page_token: "next" },
      });
    });
    await waitFor(() => {
      expect(window.location.pathname).toBe("/projects/project-alpha");
    });

    fireEvent.click(screen.getByLabelText("Home"));

    await screen.findByRole("button", { name: "Beta /tmp/project-beta" });
    expect(screen.getByTestId("app-chrome-title")).toHaveTextContent("Projects");
    await waitFor(() => {
      const projectCalls = services.transport.calls.filter((call) => call.method === "project.home.list");
      expect(projectCalls.at(-1)).toEqual({
        method: "project.home.list",
        params: { page_size: 40, page_token: "" },
      });
    });
  });

  it("reloads project pages from the first page after browser back returns Home", async () => {
    const services = createHomeRevisitServices();

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Alpha /tmp/project-alpha" }));
    await waitFor(() => {
      expect(window.location.pathname).toBe("/projects/project-alpha");
    });

    fireEvent.click(screen.getByLabelText("Back"));

    await waitFor(() => {
      expect(window.location.pathname).toBe("/");
      const projectCalls = services.transport.calls.filter((call) => call.method === "project.home.list");
      expect(projectCalls.at(-1)).toEqual({
        method: "project.home.list",
        params: { page_size: 40, page_token: "" },
      });
    });
  });

  it("shows project card workspace paths relative to the user's home directory", async () => {
    const services = createTestServices(
      [
        ...startupRoutes,
        {
          method: "project.home.list",
          result: projectPage(
            [projectSummary("project-builder", "Builder", 10, "/Users/nek/Developer/builder-cli")],
            "",
          ),
        },
      ],
      undefined,
      { homePath: "/Users/nek" },
    );

    render(<App services={services} />);

    const projectCard = await screen.findByRole("button", { name: "Builder ~/Developer/builder-cli" });
    expect(projectCard).toBeInTheDocument();
    expect(projectCard).toHaveAttribute("title", "/Users/nek/Developer/builder-cli");
    expect(screen.queryByText("/Users/nek/Developer/builder-cli")).not.toBeInTheDocument();
  });

  it("keeps Inbox on the right while Workflows replaces Projects in the left tabbed pane", async () => {
    const services = createTestServices([
      ...startupRoutes,
      {
        method: "workflow.list",
        result: {
          workflows: [
            {
              id: "workflow-delivery",
              name: "Delivery",
              description: "Ship changes",
              version: 1,
            },
          ],
          next_page_token: "",
        },
      },
    ]);

    render(<App services={services} />);

    expect(await screen.findByRole("tab", { name: "Projects" })).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByRole("heading", { name: "Inbox" })).toBeInTheDocument();

    fireEvent.click(screen.getByRole("tab", { name: "Workflows" }));

    expect(screen.getByRole("tab", { name: "Workflows" })).toHaveAttribute("aria-selected", "true");
    expect(await screen.findByText("Delivery")).toBeInTheDocument();
    expect(screen.getByText("Ship changes")).toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "Inbox" })).toBeInTheDocument();
    expect(window.location.pathname).toBe("/");
  });

  it("opens workflow creation from the Workflows tab plus action", async () => {
    const services = createTestServices(startupRoutes);

    render(<App services={services} />);

    fireEvent.click(await screen.findByRole("button", { name: "Create workflow" }));

    const sidebar = await screen.findByRole("complementary", { name: "Create workflow" });
    expect(within(sidebar).getByLabelText("Workflow name")).toBeInTheDocument();
  });

  it("disables workflow creation from the Workflows tab while disconnected", async () => {
    const services = createTestServices(startupRoutes);
    services.transport.connection.set("disconnected", "offline");

    render(<App services={services} />);

    expect(await screen.findByRole("button", { name: "Create workflow" })).toBeDisabled();
  });
});

function createHomeRevisitServices() {
  return createTestServices([
    ...startupRoutes,
    {
      method: "project.home.list",
      handler: (params: JsonValue, callIndex: number) => {
        if (isPageToken(params, "next")) {
          return projectPage([projectSummary("project-beta", "Beta", 20)], "");
        }
        if (callIndex >= 2) {
          return projectPage(
            [projectSummary("project-beta", "Beta", 30), projectSummary("project-alpha", "Alpha", 10)],
            "",
          );
        }
        return projectPage([projectSummary("project-alpha", "Alpha", 10)], "next");
      },
    },
    { method: "workflow.board.get", result: boardResponse },
  ]);
}

function isPageToken(params: JsonValue, token: string): boolean {
  return isJsonRecord(params) && params.page_token === token;
}

function isJsonRecord(value: JsonValue): value is Readonly<Record<string, JsonValue>> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function projectPage(projects: readonly ReturnType<typeof projectSummary>[], nextPageToken: string) {
  return {
    projects,
    next_page_token: nextPageToken,
    generated_at_unix_ms: 1,
  };
}

function projectSummary(
  projectID: string,
  name: string,
  updatedAtUnixMs: number,
  rootPath = `/tmp/${projectID}`,
) {
  return {
    project_id: projectID,
    project_key: name.slice(0, 3).toUpperCase(),
    display_name: name,
    primary_workspace: workspaceSummary(`workspace-${projectID}`, rootPath, updatedAtUnixMs),
    default_workflow_id: "workflow-1",
    default_workflow_name: "Default",
    default_workflow_valid: true,
    updated_at_unix_ms: updatedAtUnixMs,
    task_count: 0,
    attention_count: 0,
    workflow_count: 1,
  };
}

function workspaceSummary(workspaceID: string, rootPath: string, updatedAtUnixMs: number) {
  return {
    workspace_id: workspaceID,
    display_name: workspaceID,
    root_path: rootPath,
    availability: "available",
    is_primary: true,
    updated_at_unix_ms: updatedAtUnixMs,
  };
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

const workflow = {
  workflow_id: "workflow-1",
  display_name: "Default",
  description: "",
  version: 1,
  is_project_default: true,
  valid_for_task_creation: true,
  validation_errors: [],
};

const boardResponse = {
  board: {
    project_id: "project-alpha",
    project: { project_key: "ALP", display_name: "Alpha" },
    selected_workflow: workflow,
    workflows: [workflow],
    groups: [],
    columns: [],
    cards: [],
    done_preview: [],
    next_page_token: "",
    generated_at_unix_ms: 1,
  },
};
