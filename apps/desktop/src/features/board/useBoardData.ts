import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useCallback, useEffect } from "react";

import { queryKeys } from "../../app/queryKeys";
import { useAppServices } from "../../app/useAppServices";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";

export function useBoard(projectID: string, workflowID: string) {
  const { api } = useAppServices();
  return useQuery({
    queryKey: queryKeys.board(projectID, workflowID),
    queryFn: async () => api.getBoard(projectID, workflowID),
    enabled: projectID.trim().length > 0,
  });
}

export function useBoardNodeCards(projectID: string, workflowID: string, nodeID: string, enabled: boolean) {
  const { api } = useAppServices();
  return useInfiniteQuery({
    queryKey: queryKeys.boardNodeCards(projectID, workflowID, nodeID),
    queryFn: async ({ pageParam }) => api.listBoardNodeCards(projectID, workflowID, nodeID, pageParam),
    initialPageParam: "",
    enabled: enabled && projectID.length > 0 && workflowID.length > 0 && nodeID.length > 0,
    getNextPageParam: (lastPage) => (lastPage.nextPageToken.length > 0 ? lastPage.nextPageToken : undefined),
  });
}

export function useProjectBoardSubscription(
  projectID: string,
  boardQueryWorkflowID: string,
  input: Readonly<{
    selectedWorkflowID: string;
    selectedTaskID?: string;
    onBackgroundError?: (error: unknown) => void;
    onSelectedTaskDeleted?: () => void;
  }>,
) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  const connection = useConnectionSnapshot();
  const { onBackgroundError, onSelectedTaskDeleted, selectedTaskID = "", selectedWorkflowID } = input;
  const consumeBackgroundError = useCallback(
    (error: unknown): void => {
      onBackgroundError?.(error);
    },
    [onBackgroundError],
  );

  useEffect(() => {
    if (projectID.length === 0 || connection.phase !== "connected") {
      return;
    }
    async function refresh(): Promise<void> {
      await queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, boardQueryWorkflowID) });
      if (selectedWorkflowID !== boardQueryWorkflowID) {
        await queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, selectedWorkflowID) });
      }
      await queryClient.invalidateQueries({
        queryKey: queryKeys.boardNodeCardsRoot(projectID, selectedWorkflowID),
      });
      await queryClient.invalidateQueries({ queryKey: queryKeys.attention("") });
      await queryClient.invalidateQueries({ queryKey: queryKeys.attention(projectID) });
    }
    const subscription = api.subscribeProject(projectID, {
      onOpen() {
        void refresh().catch(consumeBackgroundError);
      },
      onEvent(_method, params) {
        if (isDeletedTaskEvent(params, selectedTaskID)) {
          onSelectedTaskDeleted?.();
        }
        void refresh().catch(consumeBackgroundError);
      },
      onComplete() {
        return;
      },
      onError() {
        void refresh().catch(consumeBackgroundError);
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
    consumeBackgroundError,
    onBackgroundError,
    onSelectedTaskDeleted,
    projectID,
    queryClient,
    selectedTaskID,
    selectedWorkflowID,
  ]);
}

function isDeletedTaskEvent(params: unknown, taskID: string): boolean {
  const trimmedTaskID = taskID.trim();
  if (trimmedTaskID.length === 0 || !isRecord(params) || !("event" in params)) {
    return false;
  }
  const rawEvent = params.event;
  if (!isRecord(rawEvent)) {
    return false;
  }
  return (
    stringField(rawEvent, "resource") === "task" &&
    stringField(rawEvent, "action") === "deleted" &&
    stringArrayField(rawEvent, "changed_ids").includes(trimmedTaskID)
  );
}

function isRecord(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function stringField(value: Readonly<Record<string, unknown>>, key: string): string {
  const raw = value[key];
  return typeof raw === "string" ? raw : "";
}

function stringArrayField(value: Readonly<Record<string, unknown>>, key: string): readonly string[] {
  const raw = value[key];
  if (!Array.isArray(raw)) {
    return [];
  }
  return raw.filter((item): item is string => typeof item === "string");
}

export function useBoardTaskActions(
  projectID: string,
  boardQueryWorkflowID: string,
  selectedWorkflowID: string,
) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  async function refresh(): Promise<void> {
    await queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, boardQueryWorkflowID) });
    if (selectedWorkflowID !== boardQueryWorkflowID) {
      await queryClient.invalidateQueries({ queryKey: queryKeys.board(projectID, selectedWorkflowID) });
    }
    await queryClient.invalidateQueries({
      queryKey: queryKeys.boardNodeCardsRoot(projectID, selectedWorkflowID),
    });
  }
  async function refreshAfterTaskDelete(taskID: string): Promise<void> {
    await Promise.all([
      refresh(),
      queryClient.invalidateQueries({ queryKey: queryKeys.task(taskID) }),
      queryClient.invalidateQueries({ queryKey: queryKeys.activity(taskID) }),
      queryClient.invalidateQueries({ queryKey: queryKeys.allTasks }),
      queryClient.invalidateQueries({ queryKey: queryKeys.allActivity }),
      queryClient.invalidateQueries({ queryKey: queryKeys.allAttention }),
      queryClient.invalidateQueries({ queryKey: queryKeys.attention("") }),
      queryClient.invalidateQueries({ queryKey: queryKeys.attention(projectID) }),
    ]);
  }
  return {
    start: useMutation({
      mutationFn: async (taskID: string) => api.startTask(taskID),
      onSuccess: refresh,
    }),
    move: useMutation({
      mutationFn: async (
        input: Readonly<{
          taskID: string;
          targetNodeID: string;
          outputValues?: Readonly<Record<string, string>>;
          allowMissingEdge?: boolean;
          autoApprove?: boolean;
        }>,
      ) => api.moveTask(input),
      onSettled: async () => {
        await refresh();
      },
    }),
    interrupt: useMutation({
      mutationFn: async (input: Readonly<{ taskID: string; runID: string }>) =>
        api.interruptTask(input.taskID, input.runID),
      onSuccess: refresh,
    }),
    delete: useMutation({
      mutationFn: async (taskID: string) => api.deleteTask(taskID),
      onSuccess: async (_result, taskID) => {
        await refreshAfterTaskDelete(taskID);
      },
    }),
    resume: useMutation({
      mutationFn: async (input: Readonly<{ taskID: string; runID: string }>) =>
        api.resumeTask(input.taskID, input.runID),
      onSuccess: refresh,
    }),
  };
}
