import { createContext, useCallback, useContext, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { type WorkflowExecutionTargetSelection } from "@/api";
import { taskActionErrorMessage } from "@/shared/task-mutations";
import { useAppServices, useStatusController } from "@/app-facade";
import {
  executeTaskInitiatingAction,
  resumeTaskInitiatingAction,
  startTaskInitiatingAction,
  TaskInitiatingActionDialogs,
  type TaskInitiatingAction,
  useTaskInitiatingActionController,
} from "@/shared/execution-target";
import { Button, Spinner } from "@/ui";

type TaskInitiatingActionController = Readonly<{
  resume(): void;
  start(): void;
  starting: boolean;
  resuming: boolean;
}>;

const TaskInitiatingActionContext = createContext<TaskInitiatingActionController | null>(null);

export function TaskInitiatingActionProvider({
  children,
  onApplied,
  onViewDependencies,
  taskID,
}: Readonly<{
  children: ReactNode;
  onApplied(): void | Promise<void>;
  onViewDependencies(taskID: string): void;
  taskID: string;
}>) {
  const { api } = useAppServices();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const reportError = useCallback(
    (kind: "resume" | "start", error: unknown) => {
      push({
        id: `task-${kind}-error`,
        title: t(kind === "resume" ? "board.resumeFailed" : "board.startFailed"),
        body: taskActionErrorMessage(error, t),
        durationMs: Infinity,
        tone: "danger",
      });
    },
    [push, t],
  );
  const continuation = useTaskInitiatingActionController({
    execute: async (action, selection) => executeTaskInitiatingAction(api, action, selection),
    onApplied,
    onAppliedError: (error, result) => {
      reportError(result.kind === "start" ? "start" : "resume", error);
    },
    onError: (action, error) => {
      reportError(action.kind === "start" ? "start" : "resume", error);
    },
  });
  function run(
    action: Extract<TaskInitiatingAction, { kind: "resume" | "start" }>,
    selection?: WorkflowExecutionTargetSelection,
  ): void {
    continuation.run(action, selection);
  }
  function resume(): void {
    run(resumeTaskInitiatingAction(taskID));
  }
  function start(): void {
    run(startTaskInitiatingAction(taskID));
  }
  return (
    <TaskInitiatingActionContext.Provider
      value={{
        resume,
        start,
        starting: continuation.pendingStartMoveTaskIDs.has(taskID),
        resuming: continuation.pendingResumeTaskIDs.has(taskID),
      }}
    >
      {children}
      <TaskInitiatingActionDialogs
        continuation={continuation}
        onResult={(result) => {
          if (result.kind === "view_dependencies") {
            onViewDependencies(result.taskID);
          } else if (result.action.kind === "resume" || result.action.kind === "start") {
            run(result.action, result.selection);
          }
        }}
      />
    </TaskInitiatingActionContext.Provider>
  );
}

export function TaskResumeButton() {
  const { t } = useTranslation();
  const controller = useContext(TaskInitiatingActionContext);
  if (controller === null) {
    throw new Error("Task Resume button requires a Task initiating-action provider");
  }
  return (
    <Button
      data-testid="task-detail-resume"
      aria-busy={controller.resuming}
      onClick={() => {
        controller.resume();
      }}
      variant="primary"
    >
      {controller.resuming ? <Spinner /> : t("board.resume")}
    </Button>
  );
}

export function TaskStartButton() {
  const { t } = useTranslation();
  const controller = useContext(TaskInitiatingActionContext);
  if (controller === null) {
    throw new Error("Task Start button requires a Task initiating-action provider");
  }
  return (
    <Button
      data-testid="task-detail-start"
      aria-busy={controller.starting}
      onClick={controller.start}
      variant="primary"
    >
      {controller.starting ? <Spinner /> : t("task.start")}
    </Button>
  );
}
