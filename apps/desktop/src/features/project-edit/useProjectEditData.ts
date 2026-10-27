import type { QueryClient } from "@tanstack/react-query";

import { invalidateProjectBoardQueries } from "@/app-facade";
import { queryKeys } from "@/app-facade";

export async function invalidateProjectEditQueries(
  queryClient: QueryClient,
  projectID: string,
): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: queryKeys.projects }),
    queryClient.invalidateQueries({ queryKey: queryKeys.projectEdit(projectID) }),
  ]);
}

export async function invalidateProjectWorkspaceOwners(
  queryClient: QueryClient,
  projectID: string,
): Promise<void> {
  await Promise.all([
    invalidateProjectWorkspaceMetadata(queryClient, projectID),
    queryClient.invalidateQueries({ queryKey: queryKeys.projectWorkspaceCatalog(projectID) }),
  ]);
}

export async function invalidateProjectWorkspaceMetadata(
  queryClient: QueryClient,
  projectID: string,
): Promise<void> {
  await Promise.all([
    invalidateProjectEditQueries(queryClient, projectID),
    invalidateProjectBoardQueries(queryClient, projectID),
  ]);
}
