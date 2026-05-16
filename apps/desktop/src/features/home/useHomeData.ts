import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";

import type { ProjectBinding } from "../../api";
import { queryKeys } from "../../app/queryKeys";
import { useAppServices } from "../../app/useAppServices";

export function useProjectPages() {
  const { api } = useAppServices();
  return useInfiniteQuery({
    queryKey: queryKeys.projects,
    queryFn: async ({ pageParam }) => api.listProjects(pageParam),
    initialPageParam: "",
    getNextPageParam: (lastPage) => (lastPage.nextPageToken.length > 0 ? lastPage.nextPageToken : undefined),
  });
}

export function useGlobalAttentionPages() {
  const { api } = useAppServices();
  return useInfiniteQuery({
    queryKey: queryKeys.attention(""),
    queryFn: async ({ pageParam }) => api.listAttention("", pageParam),
    initialPageParam: "",
    getNextPageParam: (lastPage) => (lastPage.nextPageToken.length > 0 ? lastPage.nextPageToken : undefined),
  });
}

export function useProjectCreation() {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: ProjectCreateInput) => api.createProject(input.name, input.key, input.workspaceRoot),
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.projects });
    },
  });
}

export function useWorkspaceAttach() {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: async (input: WorkspaceAttachInput): Promise<ProjectBinding> => api.attachWorkspace(input.projectID, input.workspaceRoot),
    onSuccess: async (_binding, input) => {
      await queryClient.invalidateQueries({ queryKey: queryKeys.workspaces(input.projectID) });
      await queryClient.invalidateQueries({ queryKey: queryKeys.projects });
    },
  });
}

export type ProjectCreateInput = Readonly<{
  name: string;
  key: string;
  workspaceRoot: string;
}>;

export type WorkspaceAttachInput = Readonly<{
  projectID: string;
  workspaceRoot: string;
}>;
