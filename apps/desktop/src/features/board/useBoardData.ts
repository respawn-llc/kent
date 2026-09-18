import { useQueryClient, type QueryClient } from "@tanstack/react-query";
import { useCallback, useLayoutEffect, useMemo, useState } from "react";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Effect from "effect/Effect";

import { type ProjectObservation, type WorkflowProjectEvent } from "@/api";
import {
  invalidateProjectBoardQueries,
  invalidateProjectTaskSearches,
  queryKeys,
  reportNonCancelledError,
  useProjectObservation,
} from "@/app-facade";
import { useAppServices } from "@/app-facade";
import { workflowProjectEventCanChangeTaskSearch } from "@/app-facade";
import { workflowProjectQuestionTaskID } from "@/app-facade";
import { useTaskInterruptAction } from "@/shared/execution-target";
import { useProjectLabelEffects } from "@/shared/labels";
import { workflowProjectEventAffectsDependencyBoard } from "@/shared/task-dependencies";
import { useBoardQuery, useBoardQueryModel } from "./BoardQueryRuntime";
import { createBoardRead } from "./BoardQueryModel";
import { createBoardColumnQueryModel } from "./BoardColumnQueryModel";

export function useBoard(projectID: string, workflowID: string | undefined) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const scope = useBoardQueryModel();
  const model = useMemo(
    () => createBoardRead(api, client, scope, { projectID, workflowID }),
    [api, client, scope, projectID, workflowID],
  );
  return { ...useAtomValue(model.state), refetch: useAtomSet(model.retry, { mode: "value" }) };
}

export function useBoardNodeCards(projectID: string, workflowID: string, nodeID: string, enabled: boolean) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const { filter, queriesEnabled, sort } = useBoardQuery();
  const [model] = useState(() =>
    createBoardColumnQueryModel(api, client, {
      projectID,
      workflowID,
      nodeID,
      filter,
      queriesEnabled,
      sort,
      enabled,
    }),
  );
  const update = useAtomSet(model.inputs);
  useLayoutEffect(() => {
    update({ projectID, workflowID, nodeID, filter, queriesEnabled, sort, enabled });
  }, [update, projectID, workflowID, nodeID, filter, queriesEnabled, sort, enabled]);
  const state = useAtomValue(model.state);
  const page = useAtomSet(model.page, { mode: "value" });
  const refetch = useAtomSet(model.retry, { mode: "value" });
  return {
    ...state,
    refetch,
    fetchNextPage: useCallback(() => {
      page("next");
    }, [page]),
    fetchPreviousPage: useCallback(() => {
      page("previous");
    }, [page]),
  };
}

export function useProjectBoardSubscription(
  projectID: string,
  boardQueryWorkflowID: string | undefined,
  input: Readonly<{
    selectedWorkflowID: string | undefined;
    selectedTaskID?: string;
    onBackgroundError?: (error: unknown) => void;
    onSelectedTaskDeleted?: () => void;
  }>,
) {
  const queryClient = useQueryClient();
  const labelEffects = useProjectLabelEffects();
  const { onBackgroundError, onSelectedTaskDeleted, selectedTaskID, selectedWorkflowID } = input;
  const report = (error: unknown): void => {
    reportNonCancelledError(error, (failure) => onBackgroundError?.(failure));
  };
  const consume = Effect.fn("Board.consumeProjectObservation")(function* (observation: ProjectObservation) {
    const refresh = (run: () => Promise<unknown>) =>
      Effect.tryPromise({ try: run, catch: (cause) => ({ _tag: "BoardRefreshError" as const, cause }) }).pipe(
        Effect.catch((error) =>
          Effect.sync(() => {
            report(error.cause);
          }),
        ),
      );
    const refreshBoundary = Effect.all(
      [
        refresh(async () => labelEffects.refreshAfterSubscriptionBoundary()),
        refresh(async () => invalidateProjectTaskSearches(queryClient, projectID)),
      ],
      { concurrency: "unbounded" },
    );
    switch (observation.kind) {
      case "open":
        yield* refreshBoundary;
        break;
      case "event": {
        const { event } = observation;
        yield* refresh(async () => labelEffects.consumeProjectEvent(event));
        if (isDeletedTaskEvent(event, selectedTaskID)) {
          onSelectedTaskDeleted?.();
        }
        const taskID = workflowProjectQuestionTaskID(event);
        if (taskID !== null) {
          yield* refresh(async () =>
            Promise.all([
              queryClient.invalidateQueries({ queryKey: queryKeys.task(taskID), refetchType: "active" }),
              queryClient.invalidateQueries({ queryKey: queryKeys.activity(taskID), refetchType: "active" }),
              queryClient.invalidateQueries({ queryKey: queryKeys.allPendingAsks, refetchType: "active" }),
            ]),
          );
        }
        if (workflowProjectEventCanChangeTaskSearch(event)) {
          yield* refresh(async () => invalidateProjectTaskSearches(queryClient, projectID));
        }
        if (shouldRefreshBoardFromProjectEvent(event, boardQueryWorkflowID, selectedWorkflowID)) {
          yield* refresh(async () => invalidateProjectBoardQueries(queryClient, projectID));
        }
        break;
      }
      case "complete":
        if (observation.code === 0) yield* refreshBoundary;
        break;
      case "error":
        report(observation.error);
        break;
    }
  });
  return useProjectObservation(projectID.length > 0 ? projectID : null, projectID, consume);
}

function isDeletedTaskEvent(event: WorkflowProjectEvent, taskID: string | undefined): boolean {
  if (taskID === undefined) {
    return false;
  }
  const trimmedTaskID = taskID.trim();
  if (trimmedTaskID.length === 0) {
    return false;
  }
  return event.resource === "task" && event.action === "deleted" && event.primaryEntityID === trimmedTaskID;
}

export function shouldRefreshBoardFromProjectEvent(
  event: WorkflowProjectEvent,
  boardQueryWorkflowID: string | undefined,
  selectedWorkflowID: string | undefined,
): boolean {
  if (event.resource === "task" && event.action === "labels_changed" && event.workflowID !== null) {
    return false;
  }
  return (
    workflowProjectEventAffectsDependencyBoard(event, boardQueryWorkflowID) ||
    workflowProjectEventAffectsDependencyBoard(event, selectedWorkflowID)
  );
}

export function useBoardTaskActions(projectID: string) {
  const queryClient = useQueryClient();
  const refresh = useCallback(async (): Promise<void> => {
    await refreshBoardTasks(queryClient, projectID);
  }, [projectID, queryClient]);
  const interrupt = useTaskInterruptAction(refresh);
  return {
    refresh,
    interrupt,
  };
}

async function refreshBoardTasks(client: QueryClient, projectID: string) {
  await Promise.all([
    invalidateProjectBoardQueries(client, projectID),
    invalidateProjectTaskSearches(client, projectID),
  ]);
}

export async function refreshBoardAfterTaskDelete(client: QueryClient, projectID: string, taskID: string) {
  await Promise.all([
    refreshBoardTasks(client, projectID),
    client.invalidateQueries({ queryKey: queryKeys.task(taskID) }),
    client.invalidateQueries({ queryKey: queryKeys.taskAttention(taskID) }),
    client.invalidateQueries({ queryKey: queryKeys.activity(taskID) }),
    client.invalidateQueries({ queryKey: queryKeys.allTasks }),
    client.invalidateQueries({ queryKey: queryKeys.allActivity }),
    client.invalidateQueries({ queryKey: queryKeys.allAttention }),
  ]);
}
