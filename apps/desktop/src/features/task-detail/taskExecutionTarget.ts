import type { TaskDetail } from "@/api";
import type { WorkflowOutputRoot } from "@/shared/workflow-parameter-output";

export function taskExecutionRoot(detail: TaskDetail): string {
  return detail.worktreePath ?? detail.sourceWorkspace.rootPath;
}

export function taskOutputRoot(
  mode: NonNullable<TaskDetail["executionTarget"]>["mode"] | undefined,
  worktreePath: string | null,
  sourceWorkspaceRoot: string,
): WorkflowOutputRoot {
  if (mode === "none") {
    return { kind: "source", path: sourceWorkspaceRoot };
  }
  return worktreePath === null ? { kind: "unavailable" } : { kind: "managed", path: worktreePath };
}
