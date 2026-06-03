import { createRoute, type AnyRootRoute } from "@tanstack/react-router";
import { z } from "zod";

import { ProjectCreateWindowRoute } from "../features/home/ProjectCreateForm";
import { ProjectDeleteWindowRoute } from "../features/project-edit/ProjectDeleteButton";
import { WorkspaceUnlinkWindowRoute } from "../features/project-edit/ProjectEditParts";
import { TaskDetailWindowRoute } from "../features/task-detail/StandaloneTaskRoute";
import { NewTaskWindowRoute } from "../features/tasks/NewTaskDialog";
import { InvalidNativeDialogRoute } from "./InvalidNativeDialogRoute";
import { useWindowChromeTitle } from "./windowChromeTitle";

export const projectDeleteNativeDialogPath = "/native-dialog/project-delete";
export const workspaceUnlinkNativeDialogPath = "/native-dialog/workspace-unlink";

const optionalSearchString = z.preprocess(
  (value: unknown) => (typeof value === "string" ? value : ""),
  z.string(),
);

const projectCreateSearchSchema = z.object({
  key: optionalSearchString,
  name: optionalSearchString,
  workspaceRoot: optionalSearchString,
});

const projectDeleteSearchSchema = z.object({
  projectID: optionalSearchString,
});

const taskDetailSearchSchema = z.object({
  resumeRunId: optionalSearchString,
  taskId: optionalSearchString,
});

const newTaskSearchSchema = z.object({
  projectID: optionalSearchString,
  workflowID: optionalSearchString,
});

const workspaceUnlinkSearchSchema = z.object({
  projectID: optionalSearchString,
  rootPath: optionalSearchString,
  workspaceID: optionalSearchString,
});

export function createNativeDialogRoutes(rootRoute: AnyRootRoute) {
  const projectCreateRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/native-dialog/project-create",
    validateSearch: (search: Record<string, unknown>) => projectCreateSearchSchema.parse(search),
    component: ProjectCreateNativeRoute,
  });

  function ProjectCreateNativeRoute() {
    const search = projectCreateSearchSchema.parse(projectCreateRoute.useSearch());
    return <ProjectCreateWindowRoute draft={search} />;
  }

  const projectDeleteRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: projectDeleteNativeDialogPath,
    validateSearch: (search: Record<string, unknown>) => projectDeleteSearchSchema.parse(search),
    component: ProjectDeleteNativeRoute,
  });

  function ProjectDeleteNativeRoute() {
    const search = projectDeleteSearchSchema.parse(projectDeleteRoute.useSearch());
    const projectID = search.projectID.trim();
    useWindowChromeTitle(null);
    if (projectID.length === 0) {
      return <InvalidNativeDialogRoute />;
    }
    return <ProjectDeleteWindowRoute projectID={projectID} />;
  }

  const taskDetailWindowRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/native-dialog/task-detail",
    validateSearch: (search: Record<string, unknown>) => taskDetailSearchSchema.parse(search),
    component: TaskDetailNativeRoute,
  });

  function TaskDetailNativeRoute() {
    const search = taskDetailSearchSchema.parse(taskDetailWindowRoute.useSearch());
    return <TaskDetailWindowRoute resumeRunId={search.resumeRunId} taskId={search.taskId} />;
  }

  const newTaskWindowRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: "/native-dialog/new-task",
    validateSearch: (search: Record<string, unknown>) => newTaskSearchSchema.parse(search),
    component: NewTaskNativeRoute,
  });

  function NewTaskNativeRoute() {
    const search = newTaskSearchSchema.parse(newTaskWindowRoute.useSearch());
    return <NewTaskWindowRoute projectID={search.projectID} workflowID={search.workflowID} />;
  }

  const workspaceUnlinkWindowRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: workspaceUnlinkNativeDialogPath,
    validateSearch: (search: Record<string, unknown>) => workspaceUnlinkSearchSchema.parse(search),
    component: WorkspaceUnlinkNativeRoute,
  });

  function WorkspaceUnlinkNativeRoute() {
    const search = workspaceUnlinkSearchSchema.parse(workspaceUnlinkWindowRoute.useSearch());
    return (
      <WorkspaceUnlinkWindowRoute
        projectID={search.projectID}
        rootPath={search.rootPath}
        workspaceID={search.workspaceID}
      />
    );
  }

  return [
    projectCreateRoute,
    projectDeleteRoute,
    taskDetailWindowRoute,
    newTaskWindowRoute,
    workspaceUnlinkWindowRoute,
  ] as const;
}
