import {
  InfiniteQueryObserver,
  QueryObserver,
  useQueryClient,
  type InfiniteData,
  type InfiniteQueryObserverResult,
} from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useAtomMount, useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";

import {
  errorMessage,
  noTaskLabelFilter,
  type ProjectTaskGroupCounts,
  type ProjectTaskGroup,
  type TaskListItem,
  type TaskListPage,
  type WorkflowProjectEvent,
  type ProjectObservation,
} from "@/api";
import {
  queryAtom,
  queryKeys,
  useAppServices,
  useProjectObservation,
  type QuerySnapshot,
  type AppServices,
} from "@/app-facade";
import { createProjectTaskRefresh } from "./ProjectTaskRefresh";
import type { QueryClient } from "@tanstack/react-query";
import { defaultProjectTaskSort, projectTaskSortsEqual, type ProjectTaskSort } from "./projectTaskSorting";

export const projectTaskGroups = ["active", "backlog", "done"] as const satisfies readonly ProjectTaskGroup[];
export type { ProjectTaskGroup } from "@/api";

export const projectTaskGroupPageSize = 50;
export const projectTaskGroupRetainedPages = 10;
export const projectTaskGroupPrefetchPages = 1;

export type ProjectTaskGroupDisclosure = Readonly<Record<ProjectTaskGroup, boolean>>;
export type ProjectTaskGroupData = Readonly<{
  error: Error | null;
  fetchNextPage(): void;
  fetchPreviousPage(): void;
  hasNextPage: boolean;
  hasPreviousPage: boolean;
  isError: boolean;
  isFetchNextPageError: boolean;
  isFetchPreviousPageError: boolean;
  isFetching: boolean;
  isFetchingNextPage: boolean;
  isFetchingPreviousPage: boolean;
  isPending: boolean;
  isSortReplacement: boolean;
  nextRequestGeneration: string;
  pages: readonly TaskListPage[];
  previousRequestGeneration: string;
  refetch(): void;
  tasks: readonly TaskListItem[];
}>;

export type ProjectTaskListData = Readonly<{
  active: ProjectTaskGroupData;
  backlog: ProjectTaskGroupData;
  counts: ReturnType<typeof useProjectTaskGroupCounts>;
  done: ProjectTaskGroupData;
}>;

export function projectTaskListWorkflowCardinality(
  data: ProjectTaskListData,
): TaskListPage["matchingWorkflowCardinality"] | undefined {
  for (const group of projectTaskGroups) {
    const page = data[group].pages.at(0);
    if (page !== undefined) return page.matchingWorkflowCardinality;
  }
  return undefined;
}

export function useProjectTaskListData({
  expanded,
  projectID,
  sort = defaultProjectTaskSort,
}: Readonly<{
  expanded: ProjectTaskGroupDisclosure;
  projectID: string;
  sort?: ProjectTaskSort;
}>): ProjectTaskListData {
  const counts = useProjectTaskGroupCounts(projectID);
  const active = useProjectTaskGroupData(projectID, "active", expanded.active, sort);
  const backlog = useProjectTaskGroupData(projectID, "backlog", expanded.backlog, sort);
  const done = useProjectTaskGroupData(projectID, "done", expanded.done, sort);
  return { active, backlog, counts, done };
}

function useProjectTaskGroupCounts(projectID: string) {
  const { api } = useAppServices();
  const client = useQueryClient();
  const model = useMemo(() => {
    const observer = new QueryObserver(client, {
      queryKey: queryKeys.projectTaskGroupCounts(projectID),
      queryFn: async (): Promise<ProjectTaskGroupCounts> =>
        api.getProjectTaskGroupCounts({
          projectID,
        }),
      enabled: projectID.length > 0,
      placeholderData: (previous) => previous,
    });
    return {
      request: queryAtom(observer),
      retry: Atom.fn(() => Effect.promise(async () => observer.refetch()), { concurrent: true }),
    };
  }, [api, client, projectID]);
  return { ...useAtomValue(model.request), refetch: useAtomSet(model.retry) };
}

function useProjectTaskGroupData(
  projectID: string,
  group: ProjectTaskGroup,
  enabled: boolean,
  sort: ProjectTaskSort,
): ProjectTaskGroupData {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  const [retained] = useState(() => Atom.make(emptyGenerationState));
  useAtomMount(retained);
  const model = useMemo(() => {
    const observer = new InfiniteQueryObserver<
      TaskListPage,
      Error,
      InfiniteData<TaskListPage, number>,
      readonly unknown[],
      number
    >(queryClient, {
      queryKey: queryKeys.projectTaskGroup(projectID, group, sort),
      queryFn: async ({ pageParam }) =>
        api.listTasks({
          projectID,
          group,
          labelFilter: noTaskLabelFilter,
          sort: [sort],
          offset: pageParam,
          limit: projectTaskGroupPageSize,
        }),
      initialPageParam: 0,
      enabled: enabled && projectID.length > 0,
      getPreviousPageParam: (_firstPage, _allPages, firstPageParam) =>
        firstPageParam === 0 ? undefined : Math.max(0, firstPageParam - projectTaskGroupPageSize),
      getNextPageParam: (lastPage) => lastPage.nextOffset ?? undefined,
      maxPages: projectTaskGroupRetainedPages,
    });
    const observed = queryAtom(observer);
    const state = Atom.make((get) => {
      if (!enabled) {
        get.set(retained, emptyGenerationState);
        queryClient.removeQueries({ queryKey: queryKeys.projectTaskGroupRoot(projectID, group) });
        return emptyProjectTaskGroupData;
      }
      const query = get(observed);
      const generation = get.once(retained);
      const sortChanged =
        generation.currentSort !== null && !projectTaskSortsEqual(generation.currentSort, sort);
      const queryEstablished = !query.isError && query.data !== undefined;
      const targetEstablished = queryEstablished && query.data.pageParams[0] === 0;
      const isSortReplacement = projectTaskSortReplacement({
        hasSource: generation.establishedData !== null,
        replacementSort: generation.replacementSort,
        sort,
        sortChanged,
        targetEstablished,
      });
      const displayedData = query.data ?? (isSortReplacement ? generation.establishedData : undefined);
      const next = queryEstablished
        ? projectTaskGenerationReducer(generation, { data: query.data, kind: "established", sort })
        : sortChanged
          ? projectTaskGenerationReducer(generation, { kind: "selected", sort })
          : generation;
      if (next !== generation) get.set(retained, next);
      return projectTaskGroupData({
        displayedData: displayedData ?? undefined,
        enabled,
        isSortReplacement,
        projectID,
        query,
      });
    });
    return {
      state,
      nextPage: Atom.fn(() => Effect.promise(async () => observer.fetchNextPage()), { concurrent: true }),
      previousPage: Atom.fn(() => Effect.promise(async () => observer.fetchPreviousPage()), {
        concurrent: true,
      }),
      retry: Atom.fn(() => Effect.promise(async () => observer.refetch()), { concurrent: true }),
    };
  }, [api, queryClient, projectID, group, enabled, sort, retained]);
  return {
    ...useAtomValue(model.state),
    fetchNextPage: useAtomSet(model.nextPage),
    fetchPreviousPage: useAtomSet(model.previousPage),
    refetch: useAtomSet(model.retry),
  };
}

type ProjectTaskGenerationState = Readonly<{
  currentSort: ProjectTaskSort | null;
  establishedData: InfiniteData<TaskListPage, number> | null;
  replacementSort: ProjectTaskSort | null;
}>;

type ProjectTaskGenerationAction =
  | Readonly<{ data: InfiniteData<TaskListPage, number>; kind: "established"; sort: ProjectTaskSort }>
  | Readonly<{ kind: "disabled" }>
  | Readonly<{ kind: "selected"; sort: ProjectTaskSort }>;

const emptyGenerationState: ProjectTaskGenerationState = {
  currentSort: null,
  establishedData: null,
  replacementSort: null,
};

function projectTaskGenerationReducer(
  state: ProjectTaskGenerationState,
  action: ProjectTaskGenerationAction,
): ProjectTaskGenerationState {
  if (action.kind === "disabled") {
    return emptyGenerationState;
  }
  if (action.kind === "selected") {
    return projectTaskSortsEqual(state.currentSort, action.sort)
      ? state
      : { ...state, currentSort: action.sort, replacementSort: action.sort };
  }
  return projectTaskSortsEqual(state.currentSort, action.sort) &&
    state.establishedData === action.data &&
    state.replacementSort === null
    ? state
    : { currentSort: action.sort, establishedData: action.data, replacementSort: null };
}

function projectTaskSortReplacement({
  hasSource,
  replacementSort,
  sort,
  sortChanged,
  targetEstablished,
}: Readonly<{
  hasSource: boolean;
  replacementSort: ProjectTaskSort | null;
  sort: ProjectTaskSort;
  sortChanged: boolean;
  targetEstablished: boolean;
}>): boolean {
  return (
    hasSource &&
    (sortChanged || (replacementSort !== null && projectTaskSortsEqual(replacementSort, sort))) &&
    !targetEstablished
  );
}

function projectTaskGroupData({
  displayedData,
  enabled,
  isSortReplacement,
  projectID,
  query,
}: Readonly<{
  displayedData: InfiniteData<TaskListPage, number> | undefined;
  enabled: boolean;
  isSortReplacement: boolean;
  projectID: string;
  query: QuerySnapshot<InfiniteQueryObserverResult<InfiniteData<TaskListPage, number>>>;
}>): Omit<ProjectTaskGroupData, "fetchNextPage" | "fetchPreviousPage" | "refetch"> {
  if (!enabled) {
    return emptyProjectTaskGroupData;
  }
  const pages = displayedData?.pages ?? [];
  const pageParams = displayedData?.pageParams ?? [];
  const firstPageParam = pageParams[0] ?? 0;
  const nextPageParam = pages.at(-1)?.nextOffset;
  return {
    error: query.error,
    hasNextPage: query.hasNextPage,
    hasPreviousPage: query.hasPreviousPage,
    isError: query.isError,
    isFetchNextPageError: query.isFetchNextPageError,
    isFetchPreviousPageError: query.isFetchPreviousPageError,
    isFetching: query.isFetching,
    isFetchingNextPage: query.isFetchingNextPage,
    isFetchingPreviousPage: query.isFetchingPreviousPage,
    isPending: query.isPending,
    isSortReplacement,
    nextRequestGeneration: `${projectID}:${nextPageParam?.toString() ?? "end"}`,
    pages,
    previousRequestGeneration: `${projectID}:${firstPageParam.toString()}`,
    tasks: pages.flatMap((page) => page.tasks),
  };
}

const emptyProjectTaskGroupData: ProjectTaskGroupData = {
  error: null,
  fetchNextPage: () => undefined,
  fetchPreviousPage: () => undefined,
  hasNextPage: false,
  hasPreviousPage: false,
  isError: false,
  isFetchNextPageError: false,
  isFetchPreviousPageError: false,
  isFetching: false,
  isFetchingNextPage: false,
  isFetchingPreviousPage: false,
  isPending: false,
  isSortReplacement: false,
  nextRequestGeneration: "disabled",
  pages: [],
  previousRequestGeneration: "disabled",
  refetch: () => undefined,
  tasks: [],
};

export function useProjectTaskListEvents({
  enabled,
  projectID,
}: Readonly<{
  enabled: boolean;
  projectID: string;
}>) {
  const { logger } = useAppServices();
  const queryClient = useQueryClient();
  const consume = useMemo(
    () => createProjectTaskObservation(logger, queryClient, projectID),
    [logger, queryClient, projectID],
  );
  return useProjectObservation(enabled && projectID.length > 0 ? projectID : null, projectID, consume);
}

function createProjectTaskObservation(
  logger: AppServices["logger"],
  queryClient: QueryClient,
  projectID: string,
) {
  const refresh = createProjectTaskRefresh(queryClient, projectID);
  const reportBackgroundError = (error: unknown): void => {
    void logger.append("warn", "Project Task-list refresh failed.", {
      error: errorMessage(error),
      projectID,
    });
  };
  return Effect.fn("Home.consumeProjectTasks")(
    function* (observation: ProjectObservation) {
      switch (observation.kind) {
        case "open":
          yield* Effect.tryPromise(refresh.linked);
          break;
        case "event": {
          const { event } = observation;
          if (event.projectID !== null && event.projectID !== projectID) {
            return;
          }
          if (event.resource === "workflow" || event.resource === "workflow_link") {
            yield* Effect.tryPromise(refresh.linked);
            return;
          }
          if (event.resource === "label") {
            yield* Effect.tryPromise(refresh.rows);
            return;
          }
          if (projectTaskListEventCanChangeRows(event)) {
            yield* Effect.tryPromise(refresh.rows);
          }
          break;
        }
        case "complete":
          if (observation.code === 0) yield* Effect.tryPromise(refresh.linked);
          break;
        case "error":
          reportBackgroundError(observation.error);
          break;
      }
    },
    Effect.catch((error) =>
      Effect.sync(() => {
        reportBackgroundError(error.cause);
      }),
    ),
  );
}

export function projectTaskListEventCanChangeRows(event: WorkflowProjectEvent): boolean {
  if (event.resource === "label") {
    return true;
  }
  if (event.resource !== "task") {
    return false;
  }
  return taskListChangingActions.has(event.action);
}

const taskListChangingActions: ReadonlySet<WorkflowProjectEvent["action"]> = new Set([
  "created",
  "updated",
  "deleted",
  "started",
  "interrupted",
  "resumed",
  "approved",
  "moved",
  "completed",
  "question_waiting",
  "question_cleared",
  "labels_changed",
  "dependencies_changed",
]);
