import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

import { queryKeys } from "../../app/queryKeys";
import { useAppServices } from "../../app/useAppServices";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";

export function useBoard(projectID: string, workflowID: string) {
  const { api } = useAppServices();
  return useQuery({
    queryKey: queryKeys.board(projectID, workflowID),
    queryFn: async () => api.getBoard(projectID, workflowID),
    enabled: projectID.length > 0,
  });
}

export function useProjectBoardSubscription(projectID: string, workflowID: string, latestSequence: number) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  const connection = useConnectionSnapshot();

  useEffect(() => {
    if (projectID.length === 0 || connection.phase !== "connected") {
      return;
    }
    async function refresh(): Promise<void> {
      await queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, workflowID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.attention("") });
      await queryClient.invalidateQueries({ queryKey: queryKeys.attention(projectID) });
    }
    const subscription = api.subscribeProject(projectID, latestSequence, {
      onEvent() {
        void refresh();
      },
      onComplete() {
        return;
      },
      onError() {
        void refresh();
      },
    });
    return () => { subscription.close(); };
  }, [api, connection.generation, connection.phase, latestSequence, projectID, queryClient, workflowID]);

  useEffect(() => {
    if (connection.phase === "connected") {
      void queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, workflowID) });
    }
  }, [connection.generation, connection.phase, projectID, queryClient, workflowID]);
}

export function useBoardTaskActions(projectID: string, workflowID: string) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  async function refresh(): Promise<void> {
    await queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, workflowID) });
  }
  return {
    start: useMutation({
      mutationFn: async (taskID: string) => api.startTask(taskID),
      onSuccess: refresh,
    }),
    move: useMutation({
      mutationFn: async (input: Readonly<{ taskID: string; targetNodeID: string }>) => api.moveTask(input.taskID, input.targetNodeID),
      onSuccess: refresh,
    }),
    interrupt: useMutation({
      mutationFn: async (input: Readonly<{ taskID: string; runID: string }>) => api.interruptTask(input.taskID, input.runID),
      onSuccess: refresh,
    }),
    resume: useMutation({
      mutationFn: async (input: Readonly<{ taskID: string; runID: string }>) => api.resumeTask(input.taskID, input.runID),
      onSuccess: refresh,
    }),
  };
}
