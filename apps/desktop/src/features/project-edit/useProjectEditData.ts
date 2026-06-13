import { useInfiniteQuery, useMutation, useQueryClient } from "@tanstack/react-query";
import { useEffect } from "react";
import type {
  NativeBridge,
  NativeProjectWorkspaceChanged,
  NativeWorkspaceUnlinkTarget,
} from "@app/native-bridge";

import type { ProjectBinding } from "../../api";
import { errorMessage } from "../../api/errors";
import { invalidateProjectDeleteQueries } from "../../app/projectDeletionEvents";
import { queryKeys } from "../../app/queryKeys";
import { useAppServices } from "../../app/useAppServices";

export function useProjectEdit(projectID: string) {
  const { api } = useAppServices();
  return useInfiniteQuery({
    queryKey: queryKeys.projectEdit(projectID),
    queryFn: async ({ pageParam }) => api.getProjectEdit(projectID, pageParam),
    initialPageParam: "",
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

export function useProjectDelete(
  projectID: string,
  options: Readonly<{ invalidateOnDeleted?: boolean | undefined }> = {},
) {
  const { api } = useAppServices();
  const queryClient = useQueryClient();
  const invalidateOnDeleted = options.invalidateOnDeleted ?? true;
  return useMutation({
    mutationFn: async () => api.deleteProject(projectID),
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

export function useProjectWorkspaceUnlinkRequests(
  nativeBridge: NativeBridge,
  handler: (target: NativeWorkspaceUnlinkTarget) => void,
) {
  const { logger } = useAppServices();
  useEffect(() => {
    let active = true;
    let unlisten: (() => void) | null = null;
    void nativeBridge.projectWorkspace
      .onUnlinkRequested(handler)
      .then((nextUnlisten) => {
        if (active) {
          unlisten = nextUnlisten;
          return;
        }
        nextUnlisten();
      })
      .catch((error: unknown) => {
        void logger.append("warn", "Workspace unlink event listener failed.", { error: errorMessage(error) });
      });
    return () => {
      active = false;
      unlisten?.();
    };
  }, [handler, logger, nativeBridge.projectWorkspace]);
}

export function useProjectWorkspaceChangedEvents(nativeBridge: NativeBridge, projectID: string) {
  const { logger } = useAppServices();
  const queryClient = useQueryClient();
  useEffect(() => {
    let active = true;
    let unlisten: (() => void) | null = null;
    const handler = (event: NativeProjectWorkspaceChanged) => {
      if (active && event.projectID === projectID) {
        void invalidateProjectEditQueries(queryClient, projectID);
      }
    };
    void nativeBridge.projectWorkspace
      .onChanged(handler)
      .then((nextUnlisten) => {
        if (active) {
          unlisten = nextUnlisten;
          return;
        }
        nextUnlisten();
      })
      .catch((error: unknown) => {
        void logger.append("warn", "Project workspace change listener failed.", {
          error: errorMessage(error),
        });
      });
    return () => {
      active = false;
      unlisten?.();
    };
  }, [logger, nativeBridge.projectWorkspace, projectID, queryClient]);
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
