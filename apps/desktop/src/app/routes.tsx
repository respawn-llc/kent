/* eslint-disable react-refresh/only-export-components -- TanStack Router route config intentionally colocates route components with route definitions. */
import { createRoute, createRouter, createRootRoute, Outlet } from "@tanstack/react-router";
import { z } from "zod";

import { BoardRoute } from "../features/board/BoardRoute";
import { HomeRoute } from "../features/home/HomeRoute";
import { SettingsRoute } from "../features/settings/SettingsRoute";
import { StandaloneTaskRoute } from "../features/task-detail/StandaloneTaskRoute";
import { StartupGate } from "../features/startup/StartupGate";
import { AppChrome } from "./AppChrome";

const optionalSearchString = z.preprocess(
  (value: unknown) => (typeof value === "string" ? value : ""),
  z.string(),
);

const projectSearchSchema = z.object({
  workflowId: optionalSearchString,
  taskId: optionalSearchString,
  resumeRunId: optionalSearchString,
});

const rootRoute = createRootRoute({
  component: RootRoute,
});

const homeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: HomeRoute,
});

const settingsRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/settings",
  component: SettingsRoute,
});

const projectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/projects/$projectId",
  validateSearch: (search: Record<string, unknown>) => projectSearchSchema.parse(search),
  component: ProjectRoute,
});

const taskRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/tasks/$taskId",
  component: TaskRoute,
});

const routeTree = rootRoute.addChildren([homeRoute, settingsRoute, projectRoute, taskRoute]);

export function createAppRouter() {
  return createRouter({ routeTree });
}

export type AppRouter = ReturnType<typeof createAppRouter>;

function RootRoute() {
  return (
    <AppChrome>
      <StartupGate>
        <Outlet />
      </StartupGate>
    </AppChrome>
  );
}

function ProjectRoute() {
  const params = projectRoute.useParams();
  const search = projectRoute.useSearch();
  return (
    <BoardRoute
      projectId={params.projectId}
      resumeRunId={search.resumeRunId}
      selectedTaskId={search.taskId}
      workflowId={search.workflowId}
    />
  );
}

function TaskRoute() {
  const params = taskRoute.useParams();
  return <StandaloneTaskRoute taskId={params.taskId} />;
}

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}
