import {
  createMemoryHistory,
  createRootRoute,
  createRoute,
  createRouter,
  Outlet,
  RouterProvider,
} from "@tanstack/react-router";
import { render, screen } from "@testing-library/react";
import { vi } from "vitest";

import { AppProviders } from "../../app/AppProviders";
import { createTestServices, startupRoutes } from "../../testSupport/appServices";
import { homeListCardButtonClassName, homeListCardShellClassName } from "../../ui";
import { WorkflowCard } from "../workflows/WorkflowCard";
import { ProjectRow } from "./ProjectRow";

it("keeps project and workflow cards on the same Home list card shell", async () => {
  render(
    <AppProviders services={createTestServices(startupRoutes)}>
      <RouterProvider router={cardTestRouter()} />
    </AppProviders>,
  );

  await screen.findAllByTestId("home-card-regression");
  const shells = screen.getAllByTestId("home-list-card");
  const buttons = screen.getAllByTestId("home-list-card-button");

  expect(shells).toHaveLength(2);
  expect(buttons).toHaveLength(2);
  expect(shells[0]).toHaveClass(...homeListCardShellClassName.split(" "));
  expect(shells[1]).toHaveClass(...homeListCardShellClassName.split(" "));
  expect(buttons[0]).toHaveClass(...homeListCardButtonClassName.split(" "));
  expect(buttons[1]).toHaveClass(...homeListCardButtonClassName.split(" "));
});

function cardTestRouter() {
  const rootRoute = createRootRoute({ component: Outlet });
  const indexRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/",
    component: CardRegressionFixture,
  });
  return createRouter({
    history: createMemoryHistory({ initialEntries: ["/"] }),
    routeTree: rootRoute.addChildren([indexRoute]),
  });
}

function CardRegressionFixture() {
  return (
    <div>
      <div data-testid="home-card-regression">
        <ProjectRow project={projectSummary} />
      </div>
      <div data-testid="home-card-regression">
        <WorkflowCard
          onOpen={vi.fn()}
          workflow={{
            description: "",
            version: 1,
            id: "workflow-1",
            name: "Alpha",
          }}
        />
      </div>
    </div>
  );
}

const projectSummary = {
  attentionCount: 0,
  defaultWorkflowID: "workflow-1",
  defaultWorkflowName: "Default",
  defaultWorkflowValid: true,
  id: "project-alpha",
  key: "ALP",
  name: "Alpha",
  primaryWorkspace: {
    availability: "available",
    id: "workspace-project-alpha",
    isPrimary: true,
    name: "workspace-project-alpha",
    rootPath: "/tmp/project-alpha",
    updatedAt: 1,
  },
  taskCount: 0,
  updatedAt: 1,
  workflowCount: 1,
} satisfies Parameters<typeof ProjectRow>[0]["project"];
