import { createRoute, type Register, type RootRoute } from "@tanstack/react-router";
import { z } from "zod";

import { TaskDetailWindowRoute } from "@/features/task-detail";
import { InvalidNativeDialogRoute } from "./InvalidNativeDialogRoute";
import { taskDetailNativeDialogPath } from "./sidebarPopOut";
import { nativeChatRoutePath } from "@/app-facade";
import { NativeChatRoute } from "./NativeChatRoute";

const optionalSearchString = z.string().catch("");

const taskDetailSearchSchema = z.object({
  taskID: optionalSearchString,
});

export function createNativeDialogRoutes(rootRoute: RootRoute<Register>) {
  const chatSearchSchema = z.object({
    projectID: z.string().trim().min(1),
    sessionID: z.string().trim().min(1),
  });
  const chatRoute = createRoute({
    getParentRoute: () => rootRoute,
    path: nativeChatRoutePath,
    validateSearch: (search: Record<string, unknown>) => {
      const parsed = chatSearchSchema.safeParse(search);
      return {
        chat: parsed.success ? { kind: "valid" as const, ...parsed.data } : { kind: "invalid" as const },
      };
    },
    component: ChatNativeRoute,
  });

  function ChatNativeRoute() {
    const { chat } = chatRoute.useSearch();
    if (chat.kind === "invalid") return <InvalidNativeDialogRoute />;
    return <NativeChatRoute key={chat.sessionID} projectID={chat.projectID} sessionID={chat.sessionID} />;
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

  return [taskDetailWindowRoute, chatRoute] as const;
}
