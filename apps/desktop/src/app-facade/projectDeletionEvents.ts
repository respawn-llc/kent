import type { QueryClient } from "@tanstack/react-query";
import { clearLastProjectRoute } from "./projectRoutePersistence";
import { queryKeys } from "./queryKeys";
import { removeProjectTaskSearches } from "./taskSearchQueries";
import type { AppStorageNamespace } from "./services";

export async function completeProjectDeletion({
  navigateHome,
  projectID,
  pushDeletedToast,
  queryClient,
  storageNamespace,
}: Readonly<{
  navigateHome?: (() => Promise<void>) | undefined;
  projectID: string;
  pushDeletedToast: () => void;
  queryClient: QueryClient;
  storageNamespace: AppStorageNamespace | null;
}>): Promise<void> {
  await invalidateProjectDeleteQueries(queryClient, projectID);
  clearLastProjectRoute(storageNamespace, projectID);
  if (navigateHome !== undefined) await navigateHome();
  pushDeletedToast();
}

export async function invalidateProjectDeleteQueries(
  queryClient: QueryClient,
  projectID: string,
): Promise<void> {
  await removeProjectTaskSearches(queryClient, projectID);
  await Promise.all([
    queryClient.invalidateQueries({ queryKey: queryKeys.projects }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allProjectEdits }),
    queryClient.invalidateQueries({ queryKey: queryKeys.projectCatalog(projectID) }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allBoards }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allAttention }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allTasks }),
    queryClient.invalidateQueries({ queryKey: queryKeys.allProjectWorkflowLinks, refetchType: "active" }),
  ]);
}
