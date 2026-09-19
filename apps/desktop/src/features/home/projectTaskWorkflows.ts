import { useInfiniteQuery, type InfiniteData, type UseInfiniteQueryResult } from "@tanstack/react-query";

import { workflowPageSize, type WorkflowPage, type WorkflowRecord } from "@/api";
import { queryKeys, useAppServices, useRetainedQueryData } from "@/app-facade";

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

export function useProjectTaskWorkflowPages(
  projectID: string,
): UseInfiniteQueryResult<InfiniteData<ProjectTaskWorkflowPage, bigint>> {
  const { api } = useAppServices();
  return useInfiniteQuery<
    ProjectTaskWorkflowPage,
    Error,
    InfiniteData<ProjectTaskWorkflowPage, bigint>,
    readonly unknown[],
    bigint
  >({
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

export function useProjectTaskNewTaskAvailable(
  projectID: string,
  data: InfiniteData<ProjectTaskWorkflowPage, bigint> | undefined,
): boolean {
  const currentAvailability = data === undefined ? false : firstPageNewTaskAvailability(data);
  return useRetainedQueryData(projectID, currentAvailability, (left, right) => left === right) ?? false;
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
