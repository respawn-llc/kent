import { MutationObserver, useIsMutating, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useMemo } from "react";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/reactivity/Atom";
import * as Effect from "effect/Effect";
import type { ApiService } from "@/api";
import { queryAction, useAppServices } from "@/app-facade";
import { refreshBoardAfterTaskDelete } from "./useBoardData";

export type BoardDeleteCompletion = Readonly<{
  onDeleted(): void | Promise<void>;
  onError(error: unknown): void;
}>;

const taskDeletionMutationKey = ["board-task-delete"] as const;

function createBoardTaskDeletions(api: ApiService, client: QueryClient) {
  const task = Atom.family((taskID: string) =>
    queryAction(
      new MutationObserver<void, Error, BoardDeleteCompletion>(client, {
        mutationKey: taskDeletionMutationKey,
        mutationFn: async () => api.deleteTask(taskID),
        onSuccess: async (_result, input) => input.onDeleted(),
        onError: (error, input) => {
          input.onError(error);
        },
      }),
    ),
  );
  const submit = Atom.fn<BoardDeleteCompletion & Readonly<{ taskID: string }>>()(
    (input, get) =>
      Effect.gen(function* () {
        const action = task(input.taskID);
        // Keep Query observation attached through confirmation replacement, even
        // when no dialog is reading this Task's pending state.
        yield* Atom.mount(action.request);
        yield* get.setResult(action.submit, input);
      }).pipe(Effect.scoped),
    { concurrent: true },
  );
  return { task, submit } as const;
}

export function useBoardTaskDeletions() {
  const { api } = useAppServices();
  const client = useQueryClient();
  const model = useMemo(() => createBoardTaskDeletions(api, client), [api, client]);
  return {
    task: model.task,
    submit: useAtomSet(model.submit, { mode: "value" }),
    pending: useIsMutating({ mutationKey: taskDeletionMutationKey }) > 0,
  };
}

export function useBoardTaskDeletion(owner: ReturnType<typeof useBoardTaskDeletions>, taskID: string) {
  return {
    ...useAtomValue(owner.task(taskID).request),
    submit: (input: BoardDeleteCompletion) => {
      owner.submit({ ...input, taskID });
    },
  };
}

export function useBoardDeleteConfirmation(
  input: Readonly<{
    projectID: string;
    onError(error: unknown): void;
    deletions: ReturnType<typeof useBoardTaskDeletions>;
  }>,
) {
  const client = useQueryClient();
  return {
    confirm: (taskID: string) => {
      input.deletions.submit({
        taskID,
        onError: input.onError,
        onDeleted: async () => {
          await refreshBoardAfterTaskDelete(client, input.projectID, taskID);
        },
      });
    },
  };
}
