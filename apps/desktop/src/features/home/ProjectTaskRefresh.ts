import type { QueryClient } from "@tanstack/react-query";
import { invalidateProjectBoardQueries, invalidateProjectTaskSearches, queryKeys } from "@/app-facade";

export function createProjectTaskRefresh(client: QueryClient, projectID: string) {
  const rows = async () => {
    await client.invalidateQueries({
      queryKey: queryKeys.projectTaskListsRoot(projectID),
      refetchType: "active",
    });
  };
  const workflows = async () => {
    await Promise.all([
      client.invalidateQueries({
        queryKey: queryKeys.projectWorkflowLinks(projectID),
        exact: true,
        refetchType: "active",
      }),
      client.resetQueries({ queryKey: queryKeys.projectTaskWorkflows(projectID), exact: true }),
      client.invalidateQueries({ queryKey: queryKeys.projectBoardsRoot(projectID), refetchType: "active" }),
    ]);
  };
  const linked = async () => {
    await Promise.all([workflows(), rows()]);
  };
  const resumed = async () => {
    await Promise.all([
      invalidateProjectBoardQueries(client, projectID),
      invalidateProjectTaskSearches(client, projectID),
      rows(),
    ]);
  };
  return { rows, linked, resumed } as const;
}
