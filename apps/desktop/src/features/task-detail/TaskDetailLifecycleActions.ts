import { MutationObserver, type QueryClient } from "@tanstack/react-query";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { TFunction } from "i18next";
import { errorMessage, isTaskMissingError, type TaskDetail } from "@/api";
import { queryAction, queryAtom, type AppServices, type StatusController } from "@/app-facade";
import { taskActionErrorMessage } from "@/shared/task-mutations";
import type { TaskDetailCompletion } from "./TaskDetailCommentActions";
import type { TaskDetailDeleteDismissal } from "./taskDetailDismissal";
import { refreshTaskDetail } from "./taskDetailQueries";

export type TaskDetailLifecycle = Readonly<{
  interruptPending: boolean;
  approvalPending: boolean;
  interrupt(): void;
  approve(approvalID: string): void;
}>;

export function createTaskDetailLifecycleActions({
  services,
  client,
  taskID,
  detail,
  t,
  push,
}: Readonly<{
  services: AppServices;
  client: QueryClient;
  taskID: string;
  detail(): TaskDetail | undefined;
  t: TFunction;
  push: StatusController["push"];
}>) {
  type Completion = TaskDetailCompletion & Readonly<{ projectID: string }>;
  const refresh = async (input: Completion) => {
    await refreshTaskDetail(client, taskID, input.projectID);
    input.onChanged?.();
  };
  const interruptObserver = new MutationObserver(client, {
    mutationFn: async () => services.api.interruptTask(taskID),
    onSuccess: async (_result, input: Completion) => refresh(input),
    onError: (error) => {
      push({
        id: "task-interrupt-error",
        title: t("board.interruptFailed"),
        body: errorMessage(error),
        durationMs: Infinity,
        tone: "danger",
      });
    },
  });
  const approvalObserver = new MutationObserver(client, {
    mutationFn: async (input: Completion & Readonly<{ approvalID: string }>) =>
      services.api.approveApproval(input.approvalID),
    onSuccess: async (_result, input) => refresh(input),
    onError: (error) => {
      push({
        id: "task-approval-failed",
        title: t("task.approvalFailed"),
        body: taskActionErrorMessage(error, t),
        tone: "danger",
      });
    },
  });
  const interrupt = Atom.fn<TaskDetailCompletion>()(
    (input) =>
      Effect.gen(function* () {
        const task = detail();
        if (
          task === undefined ||
          !task.actions.canInterrupt ||
          interruptObserver.getCurrentResult().isPending
        )
          return;
        yield* Effect.tryPromise(async () =>
          interruptObserver.mutate({ ...input, projectID: task.projectID }),
        ).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const approve = Atom.fn<TaskDetailCompletion & Readonly<{ approvalID: string }>>()(
    (input) =>
      Effect.gen(function* () {
        const task = detail();
        if (task === undefined || approvalObserver.getCurrentResult().isPending) return;
        yield* Effect.tryPromise(async () =>
          approvalObserver.mutate({ ...input, projectID: task.projectID }),
        ).pipe(Effect.ignore);
      }),
    { concurrent: true },
  );
  const deletion = queryAction(
    new MutationObserver(client, {
      mutationFn: async () => {
        try {
          await services.api.deleteTask(taskID);
        } catch (error) {
          if (!isTaskMissingError(error)) throw error;
        }
      },
      onError: (error) => {
        push({
          body: errorMessage(error),
          id: "task-detail-delete-error",
          title: t("board.deleteTaskWindowError"),
          tone: "danger",
        });
      },
      onSuccess: async (_result, dismiss: TaskDetailDeleteDismissal) => {
        try {
          const outcome = await dismiss();
          if (outcome.kind === "failed") throw outcome.error;
        } catch (error) {
          push({
            body: errorMessage(error),
            durationMs: Infinity,
            id: "task-detail-delete-dismiss-error",
            title: t("board.deleteTaskWindowError"),
            tone: "danger",
          });
        }
      },
    }),
  );
  const remove = Atom.fn<Readonly<{ dismiss: TaskDetailDeleteDismissal }>>()(
    (input, get) =>
      Effect.gen(function* () {
        if (detail()?.actions.canDelete !== true) return;
        yield* get.setResult(deletion.submit, input.dismiss);
      }),
    { concurrent: true },
  );
  return {
    interrupt,
    approve,
    remove,
    deletion,
    interruptRequest: queryAtom(interruptObserver),
    approvalRequest: queryAtom(approvalObserver),
    refresh,
  } as const;
}
