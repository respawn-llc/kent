import { MutationObserver, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useMemo } from "react";

import type { CreatedTaskSummary, TaskEditInput, TaskMutationInput } from "@/api";
import {
  invalidateProjectTaskSearches,
  queryAction,
  queryKeys,
  useAppServices,
  useQueryAction,
  type AppServices,
} from "@/app-facade";

export type CreateTaskSubmission = Readonly<{
  input: TaskMutationInput;
  onSuccess(created: CreatedTaskSummary): void;
  onError(error: unknown): void;
}>;

export type UpdateTaskSubmission = Readonly<{
  input: TaskEditInput;
  projectID: string;
  onSuccess(): void;
  onError(error: unknown): void;
}>;

export function useCreateTask(
  projectID: string,
  boardQueryWorkflowID: string | undefined,
  selectedWorkflowID: string | undefined,
) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  const model = useMemo(
    () =>
      queryAction(
        new MutationObserver(queryClient, {
          mutationFn: async ({ input }: CreateTaskSubmission) => api.createTask(input),
          onError: (error, submission) => {
            submission.onError(error);
          },
          onSuccess: async (created, submission) => {
            const workflowIDs = new Set<string | undefined>([boardQueryWorkflowID, selectedWorkflowID]);
            const invalidations: Promise<void>[] = [];
            for (const workflowID of workflowIDs) {
              invalidations.push(
                queryClient.invalidateQueries({
                  queryKey: queryKeys.boardWorkflowRoot(projectID, workflowID),
                }),
              );
              if (workflowID !== undefined) {
                invalidations.push(
                  queryClient.invalidateQueries({
                    queryKey: queryKeys.boardNodeCardsWorkflowRoot(projectID, workflowID),
                  }),
                );
              }
            }
            invalidations.push(invalidateProjectTaskSearches(queryClient, projectID));
            await Promise.all(invalidations);
            submission.onSuccess(created);
          },
        }),
      ),
    [api, queryClient, projectID, boardQueryWorkflowID, selectedWorkflowID],
  );
  return useQueryAction(model);
}

export function createUpdateTaskAction(api: AppServices["api"], queryClient: QueryClient) {
  return queryAction(
    new MutationObserver(queryClient, {
      mutationFn: async ({ input }: UpdateTaskSubmission) => api.updateTask(input),
      onError: (error, submission) => {
        submission.onError(error);
      },
      onSuccess: async (_result, submission) => {
        await queryClient.invalidateQueries({ queryKey: queryKeys.task(submission.input.taskID) });
        await queryClient.invalidateQueries({ queryKey: queryKeys.allBoards });
        await queryClient.invalidateQueries({ queryKey: queryKeys.allAttention });
        await invalidateProjectTaskSearches(queryClient, submission.projectID);
        submission.onSuccess();
      },
    }),
  );
}
