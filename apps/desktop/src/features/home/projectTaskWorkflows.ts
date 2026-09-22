import {
  InfiniteQueryObserver,
  useQueryClient,
  type InfiniteData,
  type QueryClient,
} from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { useAtomSet, useAtomValue } from "@effect/atom-react";
import * as Atom from "effect/unstable/reactivity/Atom";
import * as Effect from "effect/Effect";

import { workflowPageSize, type WorkflowPage, type WorkflowRecord, type ApiService } from "@/api";
import { queryAtom, queryKeys, useAppServices, retainQueryData, type RetainedQueryData } from "@/app-facade";

export type ProjectTaskWorkflowItem = Readonly<{
  description: string;
  id: string;
  isProjectDefault: boolean;
  name: string;
}>;

export type ProjectTaskWorkflowPage = Readonly<{
  workflows: readonly ProjectTaskWorkflowItem[];
  nextOffset: bigint | null;
}>;

const retainedProjectTaskWorkflowPages = 3;

export function useProjectTaskWorkflowPages(projectID: string) {
  const services = useAppServices();
  const [api] = useState(() => services.api);
  const client = useQueryClient();
  const model = useMemo(
    () => createProjectTaskWorkflowModel(api, client, projectID),
    [api, client, projectID],
  );
  return {
    ...useAtomValue(model.request),
    newTaskAvailable: useAtomValue(model.available),
    fetchNextPage: useAtomSet(model.nextPage),
    fetchPreviousPage: useAtomSet(model.previousPage),
    refetch: useAtomSet(model.retry),
    model,
  };
}

export function createProjectTaskWorkflowModel(api: ApiService, client: QueryClient, projectID: string) {
  const observer = new InfiniteQueryObserver<
    ProjectTaskWorkflowPage,
    Error,
    InfiniteData<ProjectTaskWorkflowPage, bigint>,
    readonly unknown[],
    bigint
  >(client, {
    queryKey: queryKeys.projectTaskWorkflows(projectID),
    queryFn: async ({ pageParam }) =>
      projectTaskWorkflowPage(
        await api.listWorkflows({
          limit: workflowPageSize,
          offset: pageParam,
          projectID,
        }),
      ),
    initialPageParam: 0n,
    getPreviousPageParam: (_firstPage, _allPages, firstPageParam) =>
      firstPageParam === 0n
        ? undefined
        : firstPageParam > BigInt(workflowPageSize)
          ? firstPageParam - BigInt(workflowPageSize)
          : 0n,
    getNextPageParam: (lastPage) => lastPage.nextOffset ?? undefined,
    maxPages: retainedProjectTaskWorkflowPages,
    gcTime: 0,
  });
  const request = queryAtom(observer);
  const retained = Atom.make<RetainedQueryData<boolean, string> | null>(null);
  const available = Atom.make((get) => {
    get.mount(retained);
    const data = get(request).data;
    const previous = get.once(retained);
    const next = retainQueryData(
      previous,
      {
        scope: projectID,
        data: data === undefined ? false : firstPageNewTaskAvailability(data),
        retain: true,
      },
      (left, right) => left === right,
    );
    if (previous !== next.retained) get.set(retained, next.retained);
    return next.data ?? false;
  });
  return {
    request,
    available,
    nextPage: Atom.fn(
      () =>
        Effect.promise(async () => {
          const current = observer.getCurrentResult();
          if (current.isEnabled && !current.isFetching && current.hasNextPage) await observer.fetchNextPage();
        }),
      { concurrent: true },
    ),
    previousPage: Atom.fn(
      () =>
        Effect.promise(async () => {
          const current = observer.getCurrentResult();
          if (current.isEnabled && !current.isFetching && current.hasPreviousPage)
            await observer.fetchPreviousPage();
        }),
      {
        concurrent: true,
      },
    ),
    retry: Atom.fn(
      () =>
        Effect.promise(async () => {
          const current = observer.getCurrentResult();
          if (current.isEnabled && !current.isFetching) await observer.refetch();
        }),
      { concurrent: true },
    ),
  };
}

function projectTaskWorkflowPage(page: WorkflowPage): ProjectTaskWorkflowPage {
  return {
    nextOffset: page.nextOffset,
    workflows: page.workflows.map(projectTaskWorkflowItem),
  };
}

function projectTaskWorkflowItem(workflow: WorkflowRecord): ProjectTaskWorkflowItem {
  if (workflow.projectLink === undefined) {
    throw new Error(`Project-scoped Workflow ${workflow.id} is missing its Project link.`);
  }
  return {
    description: workflow.description,
    id: workflow.id,
    isProjectDefault: workflow.projectLink.isDefault,
    name: workflow.name,
  };
}

export function projectTaskWorkflowItems(
  data: InfiniteData<ProjectTaskWorkflowPage, bigint> | undefined,
): readonly ProjectTaskWorkflowItem[] {
  return data?.pages.flatMap((page) => page.workflows) ?? [];
}

function firstPageNewTaskAvailability(
  data: InfiniteData<ProjectTaskWorkflowPage, bigint>,
): boolean | undefined {
  const firstPage = data.pages.find((_page, index) => data.pageParams[index] === 0n);
  if (firstPage === undefined) {
    return undefined;
  }
  return (
    (firstPage.workflows.length === 1 && firstPage.nextOffset === null) ||
    firstPage.workflows.some((workflow) => workflow.isProjectDefault)
  );
}
