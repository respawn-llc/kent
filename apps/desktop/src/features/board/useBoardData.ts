import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";

import type { WorkflowBoard } from "../../api";
import { queryKeys } from "../../app/queryKeys";
import { useAppServices } from "../../app/useAppServices";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";

export function useBoard(projectID: string, workflowID: string) {
  const { api } = useAppServices();
  return useInfiniteQuery({
    queryKey: queryKeys.board(projectID, workflowID),
    queryFn: async ({ pageParam }) => api.getBoard(projectID, workflowID, pageParam),
    initialPageParam: "",
    enabled: projectID.length > 0,
    getNextPageParam: (lastPage) => (lastPage.nextPageToken.length > 0 ? lastPage.nextPageToken : undefined),
    select: (data) => combineBoardPages(data.pages),
  });
}

function combineBoardPages(pages: readonly WorkflowBoard[]): WorkflowBoard {
  const first = pages[0];
  if (first === undefined) {
    throw new Error("board pages are empty");
  }
  const cardsByID = new Map(first.cards.map((card) => [card.id, card]));
  for (const page of pages.slice(1)) {
    for (const card of page.cards) {
      cardsByID.set(card.id, card);
    }
  }
  const last = pages.at(-1) ?? first;
  return {
    ...first,
    cards: [...cardsByID.values()],
    generatedAt: last.generatedAt,
    latestEventSequence: last.latestEventSequence,
    nextPageToken: last.nextPageToken,
  };
}

export function useProjectBoardSubscription(
  projectID: string,
  boardQueryWorkflowID: string,
  selectedWorkflowID: string,
  latestSequence: number,
) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  const connection = useConnectionSnapshot();

  useEffect(() => {
    if (projectID.length === 0 || connection.phase !== "connected") {
      return;
    }
    async function refresh(): Promise<void> {
      await queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, boardQueryWorkflowID) });
      if (selectedWorkflowID !== boardQueryWorkflowID) {
        await queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, selectedWorkflowID) });
      }
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
    return () => {
      subscription.close();
    };
  }, [
    api,
    boardQueryWorkflowID,
    connection.generation,
    connection.phase,
    latestSequence,
    projectID,
    queryClient,
    selectedWorkflowID,
  ]);

  useEffect(() => {
    if (projectID.length === 0 || connection.phase !== "connected") {
      return;
    }
    void queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, boardQueryWorkflowID) });
    if (selectedWorkflowID !== boardQueryWorkflowID) {
      void queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, selectedWorkflowID) });
    }
  }, [boardQueryWorkflowID, connection.generation, connection.phase, projectID, queryClient, selectedWorkflowID]);
}

export function useBoardTaskActions(projectID: string, boardQueryWorkflowID: string, selectedWorkflowID: string) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  async function refresh(): Promise<void> {
    await queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, boardQueryWorkflowID) });
    if (selectedWorkflowID !== boardQueryWorkflowID) {
      await queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, selectedWorkflowID) });
    }
  }
  return {
    start: useMutation({
      mutationFn: async (taskID: string) => api.startTask(taskID),
      onSuccess: refresh,
    }),
    move: useMutation({
      mutationFn: async (input: Readonly<{ taskID: string; targetNodeID: string }>) =>
        api.moveTask(input.taskID, input.targetNodeID),
      onSuccess: refresh,
    }),
    interrupt: useMutation({
      mutationFn: async (input: Readonly<{ taskID: string; runID: string }>) =>
        api.interruptTask(input.taskID, input.runID),
      onSuccess: refresh,
    }),
    resume: useMutation({
      mutationFn: async (input: Readonly<{ taskID: string; runID: string }>) =>
        api.resumeTask(input.taskID, input.runID),
      onSuccess: refresh,
    }),
  };
}
