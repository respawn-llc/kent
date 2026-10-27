import { describe, expect, it } from "bun:test";
import { QueryClient, QueryObserver } from "@tanstack/react-query";

import { isProjectMissingError, RpcError } from "@/api";
import { invalidateProjectDeleteQueries } from "./projectDeletionEvents";
import { queryKeys } from "./queryKeys";

describe("Project deletion owner refresh", () => {
  it.each([
    ["Project Edit", queryKeys.projectEdit("project-1")],
    ["Link Workflow", queryKeys.projectWorkflowLinks("project-1")],
    ["Project Workflow Editor", queryKeys.projectWorkflowLinks("project-1")],
    ["Project Workspace catalog", queryKeys.projectWorkspaceCatalog("project-1")],
    ["exact Project Workspace", queryKeys.projectWorkspace("project-1", "workspace-1")],
  ])("refreshes a second %s owner after deletion", async (_name, queryKey) => {
    const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    let requests = 0;
    const observer = new QueryObserver(queryClient, {
      queryFn: async () => {
        requests += 1;
        if (requests === 1) return { request: requests };
        throw new RpcError({
          code: "project_not_found",
          message: "missing",
          method: "owner.read",
        });
      },
      queryKey,
    });
    const unsubscribe = observer.subscribe(() => undefined);
    await observer.refetch();
    await invalidateProjectDeleteQueries(queryClient, "project-1");
    expect(requests).toBe(2);
    expect(isProjectMissingError(observer.getCurrentResult().error)).toBe(true);
    unsubscribe();
    queryClient.clear();
  });
});
