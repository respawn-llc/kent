import { createContext, useCallback, useContext, useState, type ReactNode } from "react";
import { useTranslation } from "react-i18next";

import { errorMessage, type TaskSetupRecovery, type WorkflowExecutionTargetSelection } from "@/api";
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
  resume(recovery?: TaskSetupRecovery): void;
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
  const [recovery, setRecovery] = useState<TaskSetupRecovery | null>(null);
  const reportError = useCallback(
    (kind: "resume" | "start", error: unknown) => {
      push({
        id: `task-${kind}-error`,
        title: t(kind === "resume" ? "board.resumeFailed" : "board.startFailed"),
        body: errorMessage(error),
        durationMs: Infinity,
        tone: "danger",
      });
    },
    [push, t],
  );
  const continuation = useTaskInitiatingActionController({
    execute: async (action, selection) => executeTaskInitiatingAction(api, action, selection),
    onApplied: async () => {
      setRecovery(null);
      await onApplied();
    },
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
  function resume(setupRecovery?: TaskSetupRecovery): void {
    if (setupRecovery !== undefined) {
      setRecovery(setupRecovery);
      return;
    }
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
        setupRecovery={
          recovery === null
            ? undefined
            : {
                onClose: () => {
                  setRecovery(null);
                },
                onSubmit: (selection) => {
                  run(resumeTaskInitiatingAction(taskID), selection);
                },
                recovery,
                running: continuation.pendingResumeTaskIDs.has(taskID),
              }
        }
      />
    </TaskInitiatingActionContext.Provider>
  );
}

export function TaskResumeButton({ recovery }: Readonly<{ recovery?: TaskSetupRecovery | undefined }>) {
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
        controller.resume(recovery);
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
