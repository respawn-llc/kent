import { createRoute, type Register, type RootRoute } from "@tanstack/react-router";
import { z } from "zod";

import { TaskDeleteWindowRoute, taskDeleteNativeDialogPath } from "@/features/board";
import { ProjectCreateWindowRoute } from "@/features/home";
import { ProjectDeleteWindowRoute } from "@/features/project-edit";
import { TaskDetailWindowRoute } from "@/features/task-detail";
import { InvalidNativeDialogRoute } from "./InvalidNativeDialogRoute";
import { taskDetailNativeDialogPath } from "./sidebarPopOut";
import { useWindowChromeTitle, nativeChatRoutePath } from "@/app-facade";
import { desktopChatEnabled } from "@/shared/feature-flags";
import { NativeChatRoute } from "./NativeChatRoute";

export const projectDeleteNativeDialogPath = "/native-dialog/project-delete";
export { taskDeleteNativeDialogPath };

const optionalSearchString = z.string().catch("");

const projectCreateSearchSchema = z.object({
  key: optionalSearchString,
  name: optionalSearchString,
  workspaceRoot: optionalSearchString,
});

const projectDeleteSearchSchema = z.object({
  projectID: optionalSearchString,
});

const taskDeleteSearchSchema = z.object({
  taskID: optionalSearchString,
});

const taskDetailSearchSchema = z.object({
  taskID: optionalSearchString,
});

export function createNativeDialogRoutes(rootRoute: RootRoute<Register>) {
  const chatSearchSchema = z.object({
    projectID: z.string().trim().min(1),
    sessionID: z.string().trim().min(1),
  });
  const chatRoute = desktopChatEnabled
    ? createRoute({
        getParentRoute: () => rootRoute,
        path: nativeChatRoutePath,
        validateSearch: (search: Record<string, unknown>) => {
          const parsed = chatSearchSchema.safeParse(search);
          return {
            chat: parsed.success ? { kind: "valid" as const, ...parsed.data } : { kind: "invalid" as const },
          };
        },
        component: ChatNativeRoute,
      })
    : undefined;

  function ChatNativeRoute() {
    if (chatRoute === undefined) return <InvalidNativeDialogRoute />;
    const { chat } = chatRoute.useSearch();
    if (chat.kind === "invalid") return <InvalidNativeDialogRoute />;
    return <NativeChatRoute key={chat.sessionID} projectID={chat.projectID} sessionID={chat.sessionID} />;
  }
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

  const taskDeleteWindowRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: taskDeleteNativeDialogPath,
    validateSearch: (search: Record<string, unknown>) => taskDeleteSearchSchema.parse(search),
    component: TaskDeleteNativeRoute,
  });

  function TaskDeleteNativeRoute() {
    const search = taskDeleteSearchSchema.parse(taskDeleteWindowRoute.useSearch());
    const taskID = search.taskID.trim();
    useWindowChromeTitle(null);
    if (taskID.length === 0) {
      return <InvalidNativeDialogRoute />;
    }
    return <TaskDeleteWindowRoute taskID={taskID} />;
  }

  const taskDetailWindowRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: taskDetailNativeDialogPath,
    validateSearch: (search: Record<string, unknown>) => taskDetailSearchSchema.parse(search),
    component: TaskDetailNativeRoute,
  });

  function TaskDetailNativeRoute() {
    const search = taskDetailSearchSchema.parse(taskDetailWindowRoute.useSearch());
    const taskID = search.taskID.trim();
    if (taskID.length === 0) {
      return <InvalidNativeDialogRoute />;
    }
    return <TaskDetailWindowRoute taskID={taskID} />;
  }

  return [
    projectCreateRoute,
    projectDeleteRoute,
    taskDeleteWindowRoute,
    taskDetailWindowRoute,
    ...(chatRoute === undefined ? [] : [chatRoute]),
  ] as const;
}
