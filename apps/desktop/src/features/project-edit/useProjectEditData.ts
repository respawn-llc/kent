import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { ApiService } from "@/api";

import { invalidateProjectBoardQueries, invalidateProjectDeleteQueries } from "@/app-facade";
import { queryKeys } from "@/app-facade";
import { useAppServices } from "@/app-facade";

export function projectDeleteMutationOptions(api: ApiService, projectID: string) {
  return { mutationFn: async () => api.deleteProject(projectID) };
}
export function useProjectDelete(
  projectID: string,
  options: Readonly<{ invalidateOnDeleted?: boolean | undefined }> = {},
) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  const invalidateOnDeleted = options.invalidateOnDeleted ?? true;
  return useMutation({
    ...projectDeleteMutationOptions(api, projectID),
    onSuccess: async (response) => {
      if (!response.deleted) {
        await invalidateProjectEditQueries(queryClient, projectID);
        return;
      }
      if (invalidateOnDeleted) {
        await invalidateProjectDeleteQueries(queryClient, projectID);
      }
    },
  });
}

export async function invalidateProjectEditQueries(
  queryClient: ReturnType<typeof useQueryClient>,
  projectID: string,
): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: queryKeys.projects }),
    queryClient.invalidateQueries({ queryKey: queryKeys.projectEdit(projectID) }),
  ]);
}

export async function invalidateProjectWorkspaceOwners(
  queryClient: ReturnType<typeof useQueryClient>,
  projectID: string,
): Promise<void> {
  await Promise.all([
    invalidateProjectWorkspaceMetadata(queryClient, projectID),
    queryClient.invalidateQueries({ queryKey: queryKeys.projectWorkspaceCatalog(projectID) }),
  ]);
}

export async function invalidateProjectWorkspaceMetadata(
  queryClient: ReturnType<typeof useQueryClient>,
  projectID: string,
): Promise<void> {
  await Promise.all([
    invalidateProjectEditQueries(queryClient, projectID),
    invalidateProjectBoardQueries(queryClient, projectID),
  ]);
}
