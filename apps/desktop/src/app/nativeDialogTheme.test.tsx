import { render, screen } from "@testing-library/react";
import { afterEach, beforeEach } from "vitest";

import { App } from "../App";
import { createTestServices, startupRoutes } from "../testSupport/appServices";

describe("native dialog theme inheritance", () => {
  beforeEach(() => {
    document.documentElement.removeAttribute("data-builder-theme");
  });

  afterEach(() => {
    document.documentElement.removeAttribute("data-builder-theme");
    window.history.pushState(null, "", "/");
  });

  it("applies inherited native-dialog theme before rendering dialog routes", async () => {
    window.history.pushState(
      null,
      "",
      "/native-dialog/new-task?projectID=project-1&workflowID=workflow-1&__builderTheme=light",
    );

    render(
      <App
        services={createTestServices([
          ...startupRoutes,
          {
            method: "project.workspace.list",
            result: {
              project_id: "project-1",
              workspaces: [],
              default_workspace_id: "",
              next_page_token: "",
            },
          },
        ])}
      />,
    );

    expect(document.documentElement).toHaveAttribute("data-builder-theme", "light");
    expect(await screen.findByRole("dialog", { name: "Create Backlog task" })).toBeInTheDocument();
    expect(document.documentElement).toHaveAttribute("data-builder-theme", "light");
  });

});
