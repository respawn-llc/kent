import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import type { ProjectBinding } from "../../api";
import { queryKeys } from "../../app/queryKeys";
import { useAppServices } from "../../app/useAppServices";

const slidingWindowPageLimit = 5;

export function useProjectEdit(projectID: string) {
  const { api } = useAppServices();
  return useInfiniteQuery({
    queryKey: queryKeys.projectEdit(projectID),
    queryFn: async ({ pageParam }) => api.getProjectEdit(projectID, pageParam),
    initialPageParam: "",
    maxPages: slidingWindowPageLimit,
    enabled: projectID.length > 0,
    getNextPageParam: (lastPage) => (lastPage.nextPageToken.length > 0 ? lastPage.nextPageToken : undefined),
  });
}

export function useProjectNameSave(projectID: string) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (displayName: string) => api.updateProject(projectID, displayName),
    onSuccess: async () => {
      await invalidateProjectEditQueries(queryClient, projectID);
    },
  });
}

export function useProjectDefaultWorkspaceSave(projectID: string) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (workspaceID: string) => api.setDefaultWorkspace(projectID, workspaceID),
    onSuccess: async () => {
      await invalidateProjectEditQueries(queryClient, projectID);
    },
  });
}

export function useProjectWorkspaceAttach(projectID: string) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (workspaceRoot: string): Promise<ProjectBinding> =>
      api.attachWorkspace(projectID, workspaceRoot),
    onSuccess: async () => {
      await invalidateProjectEditQueries(queryClient, projectID);
    },
  });
}

export function useProjectWorkspaceUnlink(projectID: string) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (workspaceID: string) => api.unlinkWorkspace(projectID, workspaceID),
    onSuccess: async () => {
      await invalidateProjectEditQueries(queryClient, projectID);
    },
  });
}

async function invalidateProjectEditQueries(
  queryClient: ReturnType<typeof useQueryClient>,
  projectID: string,
): Promise<void> {
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: queryKeys.projects }),
    queryClient.invalidateQueries({ queryKey: queryKeys.projectEdit(projectID) }),
    queryClient.invalidateQueries({ queryKey: queryKeys.workspaces(projectID) }),
  ]);
}
