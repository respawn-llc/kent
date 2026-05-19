import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import type { QuestionAnswerInput } from "../../api";
import { queryKeys } from "../../app/queryKeys";
import { useAppServices } from "../../app/useAppServices";

export function useTaskDetail(taskID: string, enabled: boolean) {
  const { api } = useAppServices();
  return useQuery({
    queryKey: queryKeys.task(taskID),
    queryFn: async () => api.getTask(taskID),
    enabled: enabled && taskID.length > 0,
  });
}

export function useTaskActivity(taskID: string, enabled: boolean) {
  const { api } = useAppServices();
  return useInfiniteQuery({
    queryKey: queryKeys.activity(taskID),
    queryFn: async ({ pageParam }) => api.listTaskActivity(taskID, pageParam),
    enabled: enabled && taskID.length > 0,
    initialPageParam: "",
    getNextPageParam: (lastPage) => (lastPage.nextPageToken.length > 0 ? lastPage.nextPageToken : undefined),
  });
}

export function usePendingAsks(sessionID: string) {
  const { api } = useAppServices();
  return useQuery({
    queryKey: queryKeys.pendingAsks(sessionID),
    queryFn: async () => api.listPendingAsks(sessionID),
    enabled: sessionID.length > 0,
  });
}

export function useTaskMutations(taskID: string, onChanged?: () => void) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  async function refresh(): Promise<void> {
    await queryClient.invalidateQueries({ queryKey: queryKeys.task(taskID) });
    await queryClient.invalidateQueries({ queryKey: queryKeys.activity(taskID) });
    await queryClient.invalidateQueries({ queryKey: queryKeys.projects });
    await queryClient.invalidateQueries({ queryKey: queryKeys.allAttention });
    await queryClient.invalidateQueries({ queryKey: queryKeys.allBoards });
    await queryClient.invalidateQueries({ queryKey: queryKeys.allTasks });
    await queryClient.invalidateQueries({ queryKey: queryKeys.allActivity });
    await queryClient.invalidateQueries({ queryKey: queryKeys.allPendingAsks });
    onChanged?.();
  }
  return {
    addComment: useMutation({
      mutationFn: async (body: string) => api.addComment(taskID, body),
      onSuccess: refresh,
    }),
    replaceComment: useMutation({
      mutationFn: async (input: Readonly<{ commentID: string; body: string }>) =>
        api.replaceComment(input.commentID, input.body),
      onSuccess: refresh,
    }),
    deleteComment: useMutation({
      mutationFn: async (commentID: string) => api.deleteComment(commentID),
      onSuccess: refresh,
    }),
    approve: useMutation({
      mutationFn: async (transitionID: string) => api.approveTransition(transitionID),
      onSuccess: refresh,
    }),
    cancel: useMutation({
      mutationFn: async () => api.cancelTask(taskID),
      onSuccess: refresh,
    }),
    interrupt: useMutation({
      mutationFn: async (runID: string) => api.interruptTask(taskID, runID),
      onSuccess: refresh,
    }),
    resume: useMutation({
      mutationFn: async (runID: string) => api.resumeTask(taskID, runID),
      onSuccess: refresh,
    }),
    answerQuestion: useMutation({
      mutationFn: async (input: QuestionAnswerInput) => api.answerQuestion(input),
      onSuccess: refresh,
    }),
  };
}
