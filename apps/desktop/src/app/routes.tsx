/* eslint-disable react-refresh/only-export-components -- TanStack Router route config intentionally colocates route components with route definitions. */
import { createRoute, createRouter, createRootRoute, Outlet, useLocation } from "@tanstack/react-router";
import { lazy, Suspense, useEffect } from "react";
import { useTranslation } from "react-i18next";
import { z } from "zod";

import { BoardRoute } from "../features/board/BoardRoute";
import { HomeRoute } from "../features/home/HomeRoute";
import { ProjectCreateWindowRoute } from "../features/home/ProjectCreateForm";
import { ProjectEditRoute } from "../features/project-edit/ProjectEditRoute";
import { WorkspaceUnlinkWindowRoute } from "../features/project-edit/ProjectEditParts";
import { StandaloneTaskRoute, TaskDetailWindowRoute } from "../features/task-detail/StandaloneTaskRoute";
import { StartupGate } from "../features/startup/StartupGate";
import { NewTaskWindowRoute } from "../features/tasks/NewTaskDialog";
import { LoadingState } from "../ui";
import { AppChrome } from "./AppChrome";
import { RouteTransitionFrame } from "./RouteTransitionFrame";
import {
  createWorkflowDeleteConfirmWindowRoute,
  workflowDeleteConfirmNativeDialogPath,
} from "./workflowDeleteConfirmRoute";
import { useWindowChromeTitle } from "./windowChromeTitle";

const LazyWorkflowEditorRoute = lazy(async () => {
  const module = await import("../features/workflow-editor/WorkflowEditorRoute");
  return { default: module.WorkflowEditorRoute };
});

const LazyWorkflowLibraryRoute = lazy(async () => {
  const module = await import("../features/workflows/WorkflowLibraryRoute");
  return { default: module.WorkflowLibraryRoute };
});

const optionalSearchString = z.preprocess(
  (value: unknown) => (typeof value === "string" ? value : ""),
  z.string(),
);

const projectSearchSchema = z.object({
  workflowId: optionalSearchString,
  taskId: optionalSearchString,
  resumeRunId: optionalSearchString,
});

const projectCreateSearchSchema = z.object({
  name: optionalSearchString,
  key: optionalSearchString,
  workspaceRoot: optionalSearchString,
});

const taskDetailSearchSchema = z.object({
  taskId: optionalSearchString,
  resumeRunId: optionalSearchString,
});

const workflowEditorSearchSchema = z.object({
  projectId: optionalSearchString,
});

const newTaskSearchSchema = z.object({
  projectID: optionalSearchString,
  workflowID: optionalSearchString,
});

const workspaceUnlinkSearchSchema = z.object({
  projectID: optionalSearchString,
  workspaceID: optionalSearchString,
  rootPath: optionalSearchString,
});

const storedProjectRouteSchema = z.object({
  projectId: z.string(),
  workflowId: z.string(),
});

const lastProjectRouteStorageKey = "builder.desktop.lastProjectRoute";
const routeRestoreSessionKey = "builder.desktop.routeRestoreChecked";
let routeRestoreCheckedFallback = false;

const rootRoute = createRootRoute({ component: RootRoute });

const homeRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: HomeShellRoute,
});

const projectRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/projects/$projectId",
  validateSearch: (search: Record<string, unknown>) => projectSearchSchema.parse(search),
  component: ProjectRoute,
});

const projectEditRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/projects/$projectId/edit",
  component: ProjectEditShellRoute,
});

const workflowLibraryRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/workflows",
  component: WorkflowLibraryShellRoute,
});

const workflowEditorRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/workflows/$workflowId/editor",
  validateSearch: (search: Record<string, unknown>) => workflowEditorSearchSchema.parse(search),
  component: WorkflowEditorShellRoute,
});

const legacyWorkflowEditorRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/projects/$projectId/workflows/$workflowId/editor",
  validateSearch: (search: Record<string, unknown>) => projectSearchSchema.pick({ workflowId: true }).parse(search),
  component: LegacyWorkflowEditorRedirectRoute,
});

const taskRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/tasks/$taskId",
  component: TaskRoute,
});

const projectCreateRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/native-dialog/project-create",
  validateSearch: (search: Record<string, unknown>) => projectCreateSearchSchema.parse(search),
  component: ProjectCreateRoute,
});

const taskDetailWindowRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/native-dialog/task-detail",
  validateSearch: (search: Record<string, unknown>) => taskDetailSearchSchema.parse(search),
  component: TaskDetailNativeRoute,
});

const newTaskWindowRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/native-dialog/new-task",
  validateSearch: (search: Record<string, unknown>) => newTaskSearchSchema.parse(search),
  component: NewTaskNativeRoute,
});

const workspaceUnlinkWindowRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/native-dialog/workspace-unlink",
  validateSearch: (search: Record<string, unknown>) => workspaceUnlinkSearchSchema.parse(search),
  component: WorkspaceUnlinkNativeRoute,
});

const workflowDeleteConfirmWindowRoute = createWorkflowDeleteConfirmWindowRoute(rootRoute);

const routeTree = rootRoute.addChildren([
  homeRoute,
  projectRoute,
  workflowLibraryRoute,
  workflowEditorRoute,
  legacyWorkflowEditorRoute,
  projectEditRoute,
  taskRoute,
  projectCreateRoute,
  taskDetailWindowRoute,
  newTaskWindowRoute,
  workspaceUnlinkWindowRoute,
  workflowDeleteConfirmWindowRoute,
]);

export function createAppRouter() {
  return createRouter({ routeTree });
}

export type AppRouter = ReturnType<typeof createAppRouter>;

function RootRoute() {
  const isNativeDialogWindow =
    typeof window !== "undefined" && window.location.pathname.startsWith("/native-dialog/");
  if (isNativeDialogWindow) {
    if (shouldSkipNativeDialogStartupGate(window.location.pathname)) {
      return <Outlet />;
    }
    return (
      <StartupGate>
        <Outlet />
      </StartupGate>
    );
  }

  return (
    <AppChrome>
      <RoutePersistence />
      <StartupGate>
        <RouteTransitionFrame />
      </StartupGate>
    </AppChrome>
  );
}

export function shouldSkipNativeDialogStartupGate(pathname: string): boolean {
  return pathname === "/native-dialog/workspace-unlink" || pathname === workflowDeleteConfirmNativeDialogPath;
}

function RoutePersistence() {
  const navigate = rootRoute.useNavigate();
  const location = useLocation();

  useEffect(() => {
    if (claimRouteRestoreCheck()) {
      const restored = readLastProjectRoute();
      if (location.pathname === "/" && restored !== null) {
        // Session restore is startup state hydration, not a user-initiated destination change, so it
        // intentionally bypasses the animated app navigation API.
        void navigate({
          to: "/projects/$projectId",
          params: { projectId: restored.projectId },
          search: { workflowId: restored.workflowId, taskId: "", resumeRunId: "" },
          replace: true,
        });
      }
    }
    const current = projectRouteState(location.pathname, location.searchStr);
    if (current !== null) {
      writeLastProjectRoute(current);
    }
  }, [location.pathname, location.searchStr, navigate]);

  return null;
}

function projectRouteState(
  pathname: string,
  searchStr: string,
): Readonly<{ projectId: string; workflowId: string }> | null {
  const segments = pathname.split("/").filter((segment) => segment.length > 0);
  if (segments.length !== 2 || segments[0] !== "projects") {
    return null;
  }
  const params = new URLSearchParams(searchStr);
  return {
    projectId: decodeURIComponent(segments[1] ?? ""),
    workflowId: params.get("workflowId") ?? "",
  };
}

function readLastProjectRoute(): Readonly<{ projectId: string; workflowId: string }> | null {
  const storage = safeStorage("local");
  const raw = storage?.getItem(lastProjectRouteStorageKey) ?? null;
  if (raw === null) {
    return null;
  }
  try {
    const parsed: unknown = JSON.parse(raw);
    const result = storedProjectRouteSchema.safeParse(parsed);
    if (!result.success) {
      return null;
    }
    return result.data;
  } catch {
    return null;
  }
}

function writeLastProjectRoute(route: Readonly<{ projectId: string; workflowId: string }>): void {
  safeStorage("local")?.setItem(lastProjectRouteStorageKey, JSON.stringify(route));
}

function claimRouteRestoreCheck(): boolean {
  const storage = safeStorage("session");
  if (storage === null) {
    const shouldRestore = !routeRestoreCheckedFallback;
    routeRestoreCheckedFallback = true;
    return shouldRestore;
  }
  if (storage.getItem(routeRestoreSessionKey) !== null) {
    return false;
  }
  storage.setItem(routeRestoreSessionKey, "1");
  return true;
}

function safeStorage(kind: "local" | "session"): Storage | null {
  try {
    if (kind === "local") {
      return globalThis.localStorage;
    }
    return globalThis.sessionStorage;
  } catch {
    return null;
  }
}

function ProjectRoute() {
  const params = projectRoute.useParams();
  const search = projectRoute.useSearch();
  useWindowChromeTitle(null);
  return (
    <BoardRoute
      projectId={params.projectId}
      resumeRunId={search.resumeRunId}
      selectedTaskId={search.taskId}
      workflowId={search.workflowId}
    />
  );
}

function HomeShellRoute() {
  const { t } = useTranslation();
  useWindowChromeTitle(t("home.projectsPane"));
  return <HomeRoute />;
}

function ProjectEditShellRoute() {
  const params = projectEditRoute.useParams();
  useWindowChromeTitle(null);
  return <ProjectEditRoute projectId={params.projectId} />;
}

function WorkflowEditorShellRoute() {
  const { t } = useTranslation();
  const params = workflowEditorRoute.useParams();
  const search = workflowEditorRoute.useSearch();
  useWindowChromeTitle(null);
  return (
    <Suspense
      fallback={
        <LoadingState
          appearanceDelayMs={0}
          chromePadding
          contentWidth="full"
          title={t("workflowEditor.loadingTitle")}
        />
      }
    >
      <LazyWorkflowEditorRoute projectID={search.projectId} workflowID={params.workflowId} />
    </Suspense>
  );
}

function WorkflowLibraryShellRoute() {
  const { t } = useTranslation();
  useWindowChromeTitle(t("workflowLibrary.title"));
  return (
    <Suspense fallback={<LoadingState appearanceDelayMs={0} title={t("workflowLibrary.title")} />}>
      <LazyWorkflowLibraryRoute />
    </Suspense>
  );
}

function LegacyWorkflowEditorRedirectRoute() {
  const navigate = rootRoute.useNavigate();
  const params = legacyWorkflowEditorRoute.useParams();

  useEffect(() => {
    // Canonical route redirects are not user-initiated destination changes, so they intentionally
    // bypass the animated app navigation API.
    void navigate({
      to: "/workflows/$workflowId/editor",
      params: { workflowId: params.workflowId },
      search: { projectId: params.projectId },
      replace: true,
    });
  }, [navigate, params.projectId, params.workflowId]);

  return null;
}

function TaskRoute() {
  const { t } = useTranslation();
  const params = taskRoute.useParams();
  useWindowChromeTitle(t("task.title"));
  return <StandaloneTaskRoute taskId={params.taskId} />;
}

function ProjectCreateRoute() {
  const search = projectCreateRoute.useSearch();
  return <ProjectCreateWindowRoute draft={search} />;
}

function TaskDetailNativeRoute() {
  const search = taskDetailWindowRoute.useSearch();
  return <TaskDetailWindowRoute resumeRunId={search.resumeRunId} taskId={search.taskId} />;
}

function NewTaskNativeRoute() {
  const search = newTaskWindowRoute.useSearch();
  return <NewTaskWindowRoute projectID={search.projectID} workflowID={search.workflowID} />;
}

function WorkspaceUnlinkNativeRoute() {
  const search = workspaceUnlinkWindowRoute.useSearch();
  return (
    <WorkspaceUnlinkWindowRoute
      projectID={search.projectID}
      rootPath={search.rootPath}
      workspaceID={search.workspaceID}
    />
  );
}

declare module "@tanstack/react-router" {
  interface Register {
    router: AppRouter;
  }
}
