import type { SidebarDestinationPolicy } from "@/app-facade";
import type { ProcessScope } from "@/api";
import { decodeNewTaskRetainedState } from "@/features/tasks";
import { insertPreparedTaskDependency } from "@/shared/task-dependencies";

export const sidebarDestinationPolicy: SidebarDestinationPolicy = {
  applyBackResult: (destination, state, result) => {
    if (destination.kind !== "newTask") {
      return state;
    }
    const retainedState = decodeNewTaskRetainedState(state);
    if (retainedState === undefined) {
      return state;
    }
    return {
      ...retainedState,
      preparedDependencies: insertPreparedTaskDependency(retainedState.preparedDependencies, {
        direction: result.direction,
        taskID: result.task.id,
        shortID: result.task.shortID,
        title: result.task.title,
        workflowID: result.task.workflowID,
        status: result.task.status,
      }),
    };
  },
  equals: (left, right) => {
    if (left.kind === "taskDetail" && right.kind === "taskDetail") return left.taskID === right.taskID;
    if (left.kind === "worktree" && right.kind === "worktree")
      return left.chat === right.chat && left.page === right.page;
    if (left.kind === "processes" && right.kind === "processes")
      return sameProcessScope(left.scope, right.scope);
    if (left.kind === "custom" && right.kind === "custom")
      return left.identity !== undefined && left.identity === right.identity;
    return false;
  },
  retainedState: (_destination, state) => state,
};

function sameProcessScope(left: ProcessScope, right: ProcessScope): boolean {
  if (left.projectID !== right.projectID) return false;
  switch (left.kind) {
    case "project":
      return right.kind === "project";
    case "session":
      return right.kind === "session" && left.sessionID === right.sessionID;
  }
}
